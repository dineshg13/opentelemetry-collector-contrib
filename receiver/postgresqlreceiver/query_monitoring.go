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

const (
	queryMetricsEvent  = "db.server.query_metrics"
	queryActivityEvent = "db.server.activity"
)

type queryMonitoringState struct {
	metricsEnd  time.Time
	activityEnd time.Time
	version     string
}

type queryConnectionClient interface {
	getMonitoringActivity(context.Context, int64, []string, *zap.Logger) ([]map[string]any, error)
	getQueryConnections(context.Context, int64, []string, *zap.Logger) ([]map[string]any, error)
}

func (p *postgreSQLScraper) scrapeQueryMonitoring(ctx context.Context, state *queryMonitoringState) (plog.Logs, error) {
	output := plog.NewLogs()
	c, err := p.clientFactory.getClient(ctx, defaultPostgreSQLDatabase)
	if err != nil {
		p.cache.Purge()
		return output, err
	}
	defer c.Close()
	if state.version == "" {
		state.version, err = c.getVersion(ctx)
		if err != nil {
			p.cache.Purge()
			return output, fmt.Errorf("query monitoring server version: %w", err)
		}
	}
	var collectionErrors errsMux
	metrics, err := p.collectMonitoringMetrics(ctx, c, state.version, monitoringIntervalStart(state.metricsEnd, p.config.ControllerConfig.CollectionInterval), time.Time{})
	state.metricsEnd = time.Now()
	if err != nil {
		collectionErrors.addPartial(err)
	} else {
		state.metricsEnd = metrics.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Timestamp().AsTime()
		metrics.ResourceLogs().MoveAndAppendTo(output.ResourceLogs())
	}
	activity, err := p.collectMonitoringActivity(ctx, c, state.version, monitoringIntervalStart(state.activityEnd, p.config.ControllerConfig.CollectionInterval), time.Time{})
	state.activityEnd = time.Now()
	if err != nil {
		collectionErrors.addPartial(err)
	} else {
		state.activityEnd = activity.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Timestamp().AsTime()
		activity.ResourceLogs().MoveAndAppendTo(output.ResourceLogs())
	}
	return output, collectionErrors.combine()
}

func (p *postgreSQLScraper) collectMonitoringMetrics(ctx context.Context, c client, version string, start, end time.Time) (plog.Logs, error) {
	limit := p.config.QueryMonitoring.MaxRows
	rows, err := c.getTopQuery(ctx, limit+1, p.excludedDatabases, p.logger)
	if err != nil {
		p.cache.Purge()
		return plog.NewLogs(), fmt.Errorf("query monitoring statistics: %w", err)
	}
	if int64(len(rows)) > limit {
		p.cache.Purge()
		return plog.NewLogs(), fmt.Errorf("query monitoring statistics exceed max_rows=%d; snapshot omitted", limit)
	}
	deltas, _, err := p.topQueryDeltas(rows)
	if err != nil {
		p.cache.Purge()
		return plog.NewLogs(), fmt.Errorf("query monitoring statistics: %w", err)
	}
	if end.IsZero() {
		end = time.Now()
	}
	queries := make([]any, 0, len(deltas))
	planAttempts := 0
	for _, row := range deltas {
		if attrString(row, "db.query.text") == "" {
			p.cache.Purge()
			return plog.NewLogs(), errors.New("query monitoring statistics have no obfuscated SQL")
		}
		clean := monitoringSelect(row, monitoringMetricAttributes)
		clean["user.name"] = attrString(clean, "postgresql.rolname")
		delete(clean, "postgresql.rolname")
		if top, ok := clean["postgresql.toplevel"].(string); ok {
			topLevel, err := strconv.ParseBool(top)
			if err != nil {
				p.cache.Purge()
				return plog.NewLogs(), errors.New("query monitoring toplevel must be boolean")
			}
			clean["postgresql.toplevel"] = topLevel
		}
		if plan := p.monitoringQueryPlan(ctx, row, &planAttempts); plan != "" {
			clean["postgresql.query_plan"] = plan
		}
		queries = append(queries, clean)
	}
	output, err := p.monitoringRecord(queryMetricsEvent, map[string]any{"queries": queries}, len(rows), len(queries), version, start, end)
	if err != nil {
		// Plans are optional. Preserve a complete statistics snapshot if their
		// combined size exceeds the configured record limit.
		removedPlan := false
		for _, value := range queries {
			row := value.(map[string]any)
			if _, ok := row["postgresql.query_plan"]; ok {
				delete(row, "postgresql.query_plan")
				removedPlan = true
			}
		}
		if removedPlan {
			output, err = p.monitoringRecord(queryMetricsEvent, map[string]any{"queries": queries}, len(rows), len(queries), version, start, end)
		}
	}
	if err != nil {
		p.cache.Purge()
	}
	return output, err
}

type monitoringPlanClient interface {
	explainMonitoringQuery(context.Context, string, string) (string, error)
}

func (p *postgreSQLScraper) monitoringQueryPlan(ctx context.Context, row map[string]any, attempts *int) string {
	cfg := p.config.QueryMonitoring.QueryPlans
	if !cfg.Enabled {
		return ""
	}
	key := topQueryIdentityFromRow(row).planCacheKey()
	if plan, ok := p.queryPlanCache.Get(key); ok {
		return plan
	}
	if *attempts >= cfg.MaxPerCollection {
		return ""
	}
	query := attrString(row, "postgresql.raw_query")
	queryID := attrString(row, "postgresql.queryid")
	if _, err := strconv.ParseInt(queryID, 10, 64); err != nil || !isExplainableQuery(query) {
		p.queryPlanCache.Add(key, "")
		return ""
	}
	*attempts++
	planCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	c, err := p.clientFactory.getClient(planCtx, attrString(row, "db.namespace"))
	if err != nil {
		p.queryPlanCache.Add(key, "")
		return ""
	}
	defer c.Close()
	var plan string
	if planner, ok := c.(monitoringPlanClient); ok {
		plan, err = planner.explainMonitoringQuery(planCtx, query, queryID)
	}
	if err != nil || len(plan) > cfg.MaxPlanBytes {
		// Error details and prepared SQL may contain sensitive query text.
		p.logger.Debug("query monitoring plan unavailable", zap.String("queryID", queryID))
		plan = ""
	}
	// Negative results share the TTL so denied/unsupported queries do not retry
	// on every collection. Identity includes database, role, query and toplevel.
	p.queryPlanCache.Add(key, plan)
	return plan
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
		if attrString(row, "postgresql.state") == "idle" || attrString(row, "db.namespace") == "" || attrString(row, "user.name") == "" {
			continue
		}
		if attrInt64(row, "postgresql.pid") <= 0 || attrString(row, "db.query.text") == "" {
			return plog.NewLogs(), errors.New("query monitoring activity has invalid PID or obfuscated SQL")
		}
		clean := monitoringSelect(row, monitoringActivityAttributes)
		duration := attrFloat64(row, postgresqlTotalExecTimeAttributeName) / 1000
		if duration < 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
			return plog.NewLogs(), errors.New("query monitoring activity duration must be finite and nonnegative")
		}
		clean["postgresql.total_exec_time"] = duration
		queryStart, err := monitoringQueryStart(attrString(row, "postgresql.query_start"))
		if err != nil {
			return plog.NewLogs(), err
		}
		queryTime, _ := time.Parse(time.RFC3339Nano, queryStart)
		if queryTime.After(end) {
			return plog.NewLogs(), errors.New("query monitoring query_start exceeds collection end; check database clock")
		}
		clean["postgresql.query_start"] = queryStart
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
	rl.Resource().Attributes().PutStr("db.version", version)
	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().SetName(metadata.ScopeName)
	sl.Scope().SetVersion("query-monitoring-v1")
	record := sl.LogRecords().AppendEmpty()
	record.SetEventName(event)
	record.SetTimestamp(pcommon.NewTimestampFromTime(end))
	record.SetObservedTimestamp(pcommon.NewTimestampFromTime(end))
	attrs := record.Attributes()
	attrs.PutStr("db.collection.schema.version", "1")
	attrs.PutStr("db.collection.id", uuid.NewString())
	attrs.PutInt("db.collection.start_time_unix_nano", start.UnixNano())
	attrs.PutInt("db.collection.end_time_unix_nano", end.UnixNano())
	attrs.PutBool("db.collection.complete", true)
	attrs.PutInt("db.collection.rows_observed", int64(observed))
	attrs.PutInt("db.collection.rows_emitted", int64(emitted))
	if err := record.Body().FromRaw(body); err != nil {
		return plog.NewLogs(), fmt.Errorf("query monitoring record: %w", err)
	}
	request := plogotlp.NewExportRequestFromLogs(output)
	encoded, err := request.MarshalProto()
	if err != nil {
		return plog.NewLogs(), err
	}
	jsonEncoded, err := request.MarshalJSON()
	if err != nil {
		return plog.NewLogs(), err
	}
	size := max(len(encoded), len(jsonEncoded))
	if size > p.config.QueryMonitoring.MaxPayloadBytes {
		return plog.NewLogs(), fmt.Errorf("query monitoring %s snapshot is %d bytes, exceeding max_payload_bytes=%d; snapshot omitted", event, size, p.config.QueryMonitoring.MaxPayloadBytes)
	}
	return output, nil
}

func monitoringQueryStart(value string) (string, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999Z07", "2006-01-02 15:04:05.999999999Z0700"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return "", errors.New("query monitoring query_start must contain a valid timestamp with timezone")
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

var monitoringMetricAttributes = []string{
	"db.namespace", "db.query.text", "db.query.tables", "db.query.commands",
	"postgresql.queryid", "postgresql.dbid", "postgresql.userid", "postgresql.toplevel", "postgresql.rolname",
	"postgresql.calls", "postgresql.rows", "postgresql.shared_blks_dirtied", "postgresql.shared_blks_hit",
	"postgresql.shared_blks_read", "postgresql.shared_blks_written", "postgresql.temp_blks_read", "postgresql.temp_blks_written",
	"postgresql.total_exec_time", "postgresql.total_plan_time",
}

var monitoringActivityAttributes = []string{
	"db.namespace", "user.name", "db.query.text", "db.query.tables", "db.query.commands",
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
