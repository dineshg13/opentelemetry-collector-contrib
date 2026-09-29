// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/sqlquery"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

const queryActivityEvent = "db.server.activity"

type queryMonitoringState struct {
	activityEnd time.Time
}

type queryConnectionClient interface {
	getMonitoringActivity(context.Context, int64, []string, *zap.Logger) ([]map[string]any, error)
	getQueryConnections(context.Context, int64, []string, *zap.Logger) ([]map[string]any, error)
}

func (p *postgreSQLScraper) scrapeQueryMonitoring(ctx context.Context, state *queryMonitoringState) (plog.Logs, error) {
	output := plog.NewLogs()
	c, err := p.clientFactory.getClient(ctx, defaultPostgreSQLDatabase)
	if err != nil {
		return output, err
	}
	defer c.Close()
	if p.dbVersion == "" {
		p.dbVersion, err = c.getVersion(ctx)
		if err != nil {
			return output, fmt.Errorf("query monitoring server version: %w", err)
		}
	}
	var collectionErrors errsMux
	activity, err := p.collectMonitoringActivity(ctx, c, p.dbVersion, monitoringIntervalStart(state.activityEnd, p.config.ControllerConfig.CollectionInterval), time.Time{})
	state.activityEnd = time.Now()
	if err != nil {
		collectionErrors.addPartial(err)
	} else {
		state.activityEnd = activity.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Timestamp().AsTime()
		activity.ResourceLogs().MoveAndAppendTo(output.ResourceLogs())
	}
	return output, collectionErrors.combine()
}

func (p *postgreSQLScraper) collectMonitoringActivity(ctx context.Context, c client, version string, start, end time.Time) (plog.Logs, error) {
	connectionClient, ok := c.(queryConnectionClient)
	if !ok {
		return plog.NewLogs(), errors.New("query monitoring connection summaries are unavailable")
	}
	limit := p.config.QueryMonitoring.MaxRows
	rows, err := connectionClient.getMonitoringActivity(ctx, limit+1, p.excludedDatabases, p.logger)
	if err != nil {
		return plog.NewLogs(), fmt.Errorf("query monitoring activity: %w", err)
	}
	if int64(len(rows)) > limit {
		return plog.NewLogs(), fmt.Errorf("query monitoring activity exceeds max_rows=%d; snapshot omitted", limit)
	}
	connections, err := connectionClient.getQueryConnections(ctx, limit+1, p.excludedDatabases, p.logger)
	if err != nil {
		return plog.NewLogs(), fmt.Errorf("query monitoring connections: %w", err)
	}
	if int64(len(connections)) > limit {
		return plog.NewLogs(), fmt.Errorf("query monitoring connection groups exceed max_rows=%d; snapshot omitted", limit)
	}
	if end.IsZero() {
		end = time.Now()
	}
	sessions := make([]any, 0, len(rows))
	for _, row := range rows {
		// The monitoring query already excludes idle and anonymous sessions.
		if attrInt64(row, "postgresql.pid") <= 0 || attrString(row, "db.query.text") == "" {
			return plog.NewLogs(), errors.New("query monitoring activity has invalid PID or obfuscated SQL")
		}
		clean := monitoringSelect(row, monitoringActivityAttributes)
		duration := attrFloat64(row, postgresqlTotalExecTimeAttributeName) / 1000
		if duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
			return plog.NewLogs(), errors.New("query monitoring activity duration must be finite and nonnegative")
		}
		clean["postgresql.total_exec_time"] = duration
		// The monitoring query formats query_start in UTC as RFC 3339.
		queryStart, err := time.Parse(time.RFC3339Nano, attrString(row, "postgresql.query_start"))
		if err != nil {
			return plog.NewLogs(), errors.New("query monitoring query_start must be an RFC 3339 timestamp")
		}
		if queryStart.After(end) {
			return plog.NewLogs(), errors.New("query monitoring query_start exceeds collection end; check database clock")
		}
		clean["postgresql.query_start"] = queryStart.UTC().Format(time.RFC3339Nano)
		pids, err := monitoringBlockingPIDs(attrString(row, dbAttributePrefix+querySampleColumnBlockingPIDs))
		if err != nil {
			return plog.NewLogs(), err
		}
		clean["postgresql.blocking.pids"] = pids
		if traceCtx, ok := row[querySampleTraceContextKey].(context.Context); ok {
			sc := trace.SpanContextFromContext(traceCtx)
			if sc.IsValid() {
				clean["trace_id"] = sc.TraceID().String()
				clean["span_id"] = sc.SpanID().String()
				clean["trace_flags"] = int64(sc.TraceFlags())
			}
		}
		sessions = append(sessions, clean)
	}
	connectionValues := make([]any, len(connections))
	for i, row := range connections {
		connectionValues[i] = row
	}
	return p.monitoringRecord(queryActivityEvent, map[string]any{"sessions": sessions, "connections": connectionValues}, len(rows), len(sessions), version, start, end)
}

func (p *postgreSQLScraper) monitoringRecord(event string, body map[string]any, observed, emitted int, version string, start, end time.Time) (plog.Logs, error) {
	if !start.Before(end) || start.UnixNano() <= 0 || end.UnixNano() <= 0 {
		return plog.NewLogs(), errors.New("query monitoring collection interval must be positive")
	}
	output := plog.NewLogs()
	rl := output.ResourceLogs().AppendEmpty()
	resource := p.setupLogsResourceBuilder(p.lb.NewResourceBuilder()).Emit()
	resource.CopyTo(rl.Resource())
	rl.Resource().Attributes().PutStr("db.system.name", "postgresql")
	if service, ok := rl.Resource().Attributes().Get("service.name"); !ok || service.Str() == "" {
		rl.Resource().Attributes().PutStr("service.name", defaultServiceName)
	}
	rl.Resource().Attributes().PutStr("db.system.version", version)
	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName(metadata.ScopeName)
	sl.Scope().SetVersion(p.buildVersion)
	record := sl.LogRecords().AppendEmpty()
	record.SetEventName(event)
	record.SetTimestamp(pcommon.NewTimestampFromTime(end))
	record.SetObservedTimestamp(pcommon.NewTimestampFromTime(end))
	attrs := record.Attributes()
	attrs.PutStr("postgresql.activity.schema.version", "1")
	attrs.PutStr("postgresql.activity.snapshot.id", uuid.NewString())
	attrs.PutInt("postgresql.activity.interval.start_time_unix_nano", start.UnixNano())
	attrs.PutInt("postgresql.activity.interval.end_time_unix_nano", end.UnixNano())
	attrs.PutBool("postgresql.activity.snapshot.complete", true)
	attrs.PutInt("postgresql.activity.rows_observed", int64(observed))
	attrs.PutInt("postgresql.activity.rows_emitted", int64(emitted))
	if err := record.Body().FromRaw(body); err != nil {
		return plog.NewLogs(), fmt.Errorf("query monitoring record: %w", err)
	}
	// OTLP/JSON is the larger encoding, so it bounds both transports.
	encoded, err := plogotlp.NewExportRequestFromLogs(output).MarshalJSON()
	if err != nil {
		return plog.NewLogs(), err
	}
	size := len(encoded)
	if size > p.config.QueryMonitoring.MaxPayloadBytes {
		return plog.NewLogs(), fmt.Errorf("query monitoring %s snapshot is %d bytes, exceeding max_payload_bytes=%d; snapshot omitted", event, size, p.config.QueryMonitoring.MaxPayloadBytes)
	}
	return output, nil
}

func monitoringBlockingPIDs(value string) ([]any, error) {
	if value == "" || value == "{}" {
		return []any{}, nil
	}
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return nil, errors.New("query monitoring blocking PIDs must be a PostgreSQL array")
	}
	parts := strings.Split(value[1:len(value)-1], ",")
	pids := make([]any, 0, len(parts))
	for _, part := range parts {
		pid, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || pid <= 0 {
			return nil, errors.New("query monitoring blocking PID must be a positive integer")
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func (c *postgreSQLClient) getQueryConnections(ctx context.Context, limit int64, excluded []string, logger *zap.Logger) ([]map[string]any, error) {
	query := `SELECT COALESCE(datname, '') AS datname, COALESCE(usename, '') AS usename,
COALESCE(application_name, '') AS application_name, COALESCE(state, '') AS state,
COUNT(*)::text AS connections FROM pg_stat_activity
WHERE pid != pg_backend_pid() AND backend_type = 'client backend' AND datname IS NOT NULL AND usename IS NOT NULL`
	if len(excluded) > 0 {
		query += " AND COALESCE(datname, '') NOT IN (" + quoteDatabaseList(excluded) + ")"
	}
	query += " GROUP BY datname, usename, application_name, state ORDER BY datname, usename, application_name, state LIMIT " + strconv.FormatInt(limit, 10)
	wrapped := sqlquery.NewDbClient(sqlquery.DbWrapper{Db: c.client}, query, logger, sqlquery.TelemetryConfig{})
	rows, err := wrapped.QueryRows(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		count, err := strconv.ParseInt(row["connections"], 10, 64)
		if err != nil || count < 0 {
			return nil, errors.New("query monitoring connection count is invalid")
		}
		result = append(result, map[string]any{"db.namespace": row["datname"], "user.name": row["usename"], "postgresql.application_name": row["application_name"], "postgresql.state": row["state"], "postgresql.connections": count})
	}
	return result, nil
}

var monitoringActivityAttributes = []string{
	"db.namespace", "user.name", "db.query.text",
	"postgresql.pid", "postgresql.state", "postgresql.application_name", "postgresql.query_start",
	"postgresql.query_id", "postgresql.wait_event", "postgresql.wait_event_type", "postgresql.blocking.pids",
	"network.peer.address", "network.peer.port", "postgresql.client_hostname", "postgresql.total_exec_time",
}

func monitoringSelect(row map[string]any, names []string) map[string]any {
	result := make(map[string]any, len(names))
	for _, name := range names {
		if value, ok := row[name]; ok {
			result[name] = value
		}
	}
	return result
}

func monitoringIntervalStart(previous time.Time, interval time.Duration) time.Time {
	if previous.IsZero() {
		return time.Now().Add(-interval)
	}
	return previous
}
