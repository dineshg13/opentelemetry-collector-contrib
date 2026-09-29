// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/template"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/sqlquery"
	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

const statementMetricPrefix = "postgresql.statement."

//go:embed templates/statementMetricsTemplate.tmpl
var statementMetricsTemplate string

var statementMetricsTmpl = template.Must(template.New("statementMetrics").Option("missingkey=error").Parse(statementMetricsTemplate))

// statementSeriesKey identifies one pg_stat_statements entry by the attributes
// recorded on its data points.
type statementSeriesKey struct {
	database string
	role     string
	queryID  string
	topLevel bool
}

// statementStats is one pg_stat_statements entry with lifetime counters.
type statementStats struct {
	key        statementSeriesKey
	text       string // obfuscated
	statsSince string
	statsReset string
	calls      int64
	rows       int64
	// dirtied, hit, read, written
	shared [4]int64
	// read, written
	temp        [2]int64
	execSeconds float64
	planSeconds float64
}

type statementStatsClient interface {
	getStatementStats(ctx context.Context, limit int64, excludedDatabases []string) ([]statementStats, int, error)
	getQueryStatisticsEpoch(ctx context.Context) (string, error)
}

// getStatementStats returns the most frequently called statements and the
// number of rows that were skipped because they could not be parsed or obfuscated.
func (c *postgreSQLClient) getStatementStats(ctx context.Context, limit int64, excludedDatabases []string) ([]statementStats, int, error) {
	buf := bytes.Buffer{}
	if err := statementMetricsTmpl.Execute(&buf, map[string]any{
		"limit":             limit,
		"excludedDatabases": quoteDatabaseList(excludedDatabases),
	}); err != nil {
		return nil, 0, fmt.Errorf("failed executing template: %w", err)
	}
	wrapped := sqlquery.NewDbClient(sqlquery.DbWrapper{Db: c.client}, buf.String(), zap.NewNop(), sqlquery.TelemetryConfig{})
	rows, err := wrapped.QueryRows(ctx)
	if err != nil && !errors.Is(err, sqlquery.ErrNullValueWarning) {
		return nil, 0, err
	}
	stats := make([]statementStats, 0, len(rows))
	skipped := 0
	for _, row := range rows {
		s, ok := parseStatementStats(row)
		if !ok {
			skipped++
			continue
		}
		stats = append(stats, s)
	}
	return stats, skipped, nil
}

func parseStatementStats(row map[string]string) (statementStats, bool) {
	topLevel, err := strconv.ParseBool(row["toplevel"])
	if err != nil || row["datname"] == "" || row["queryid"] == "" {
		return statementStats{}, false
	}
	text, err := obfuscateSQL(row["query"])
	if err != nil || text == "" {
		return statementStats{}, false
	}
	s := statementStats{
		key:        statementSeriesKey{database: row["datname"], role: row["rolname"], queryID: row["queryid"], topLevel: topLevel},
		text:       text,
		statsSince: row["stats_since"],
	}
	for _, field := range []struct {
		column string
		target *int64
	}{
		{"calls", &s.calls},
		{"rows", &s.rows},
		{"shared_blks_dirtied", &s.shared[0]},
		{"shared_blks_hit", &s.shared[1]},
		{"shared_blks_read", &s.shared[2]},
		{"shared_blks_written", &s.shared[3]},
		{"temp_blks_read", &s.temp[0]},
		{"temp_blks_written", &s.temp[1]},
	} {
		value, err := strconv.ParseInt(row[field.column], 10, 64)
		if err != nil || value < 0 {
			return statementStats{}, false
		}
		*field.target = value
	}
	for _, field := range []struct {
		column string
		target *float64
	}{{"total_exec_time", &s.execSeconds}, {"total_plan_time", &s.planSeconds}} {
		ms, err := strconv.ParseFloat(row[field.column], 64)
		if err != nil || ms < 0 || math.IsNaN(ms) || math.IsInf(ms, 0) {
			return statementStats{}, false
		}
		*field.target = ms / 1000
	}
	return s, true
}

func (c *postgreSQLClient) getQueryStatisticsEpoch(ctx context.Context) (string, error) {
	var epoch string
	// Epoch text avoids differences caused by session timezone/DateStyle settings.
	err := c.client.QueryRowContext(ctx, "/* otel-collector-ignore */ SELECT EXTRACT(EPOCH FROM stats_reset)::text FROM pg_stat_statements_info").Scan(&epoch)
	if err != nil {
		return "", fmt.Errorf("read statement reset epoch (statement metrics require pg_stat_statements extension 1.9 or newer): %w", err)
	}
	if epoch == "" {
		return "", errors.New("statement reset epoch is empty")
	}
	return epoch, nil
}

func (p *postgreSQLScraper) statementMetricsEnabled() bool {
	m := p.config.MetricsBuilderConfig.Metrics
	return m.PostgresqlStatementCalls.Enabled || m.PostgresqlStatementRows.Enabled ||
		m.PostgresqlStatementExecutionTime.Enabled || m.PostgresqlStatementPlanningTime.Enabled ||
		m.PostgresqlStatementSharedBlocks.Enabled || m.PostgresqlStatementTempBlocks.Enabled
}

// fetchStatementStats reads the global reset epoch before and after the
// statistics on PostgreSQL 14+, so counters that span a reset are never reported.
func (p *postgreSQLScraper) fetchStatementStats(ctx context.Context, c statementStatsClient, version string) ([]statementStats, int, error) {
	major, err := parseMajorVersion(version)
	if err != nil {
		return nil, 0, fmt.Errorf("PostgreSQL version: %w", err)
	}
	limit := p.config.StatementMetrics.MaxStatements
	if major < 14 {
		// PostgreSQL 13 has no pg_stat_statements_info; start times fall back to
		// the receiver start and resets are detected only by decreasing counters.
		return c.getStatementStats(ctx, limit, p.excludedDatabases)
	}
	before, err := c.getQueryStatisticsEpoch(ctx)
	if err != nil {
		return nil, 0, err
	}
	stats, skipped, err := c.getStatementStats(ctx, limit, p.excludedDatabases)
	if err != nil {
		return nil, 0, err
	}
	after, err := c.getQueryStatisticsEpoch(ctx)
	if err != nil {
		return nil, 0, err
	}
	if before != after {
		return nil, 0, errors.New("statement statistics reset during collection; statements omitted")
	}
	for i := range stats {
		stats[i].statsReset = after
	}
	return stats, skipped, nil
}

// collectStatementMetrics records cumulative pg_stat_statements counters. It
// returns the reset-aware start time of each recorded series, which the caller
// applies after the metrics builder has emitted the data points.
func (p *postgreSQLScraper) collectStatementMetrics(ctx context.Context, c client, errs *errsMux) map[statementSeriesKey]pcommon.Timestamp {
	if !p.statementMetricsEnabled() {
		return nil
	}
	statsClient, ok := c.(statementStatsClient)
	if !ok {
		errs.addPartial(errors.New("statement metrics are unavailable for this client"))
		return nil
	}
	if p.statementServerVersion == "" {
		version, err := c.getVersion(ctx)
		if err != nil {
			errs.addPartial(fmt.Errorf("statement metrics server version: %w", err))
			return nil
		}
		p.statementServerVersion = version
	}
	stats, skipped, err := p.fetchStatementStats(ctx, statsClient, p.statementServerVersion)
	if err != nil {
		errs.addPartial(fmt.Errorf("statement metrics: %w", err))
		return nil
	}
	now := time.Now()
	ts := pcommon.NewTimestampFromTime(now)
	starts := make(map[statementSeriesKey]pcommon.Timestamp, len(stats))
	for _, s := range stats {
		if p.isExcluded(s.key.database) {
			continue
		}
		if _, duplicate := starts[s.key]; duplicate {
			skipped++
			continue
		}
		starts[s.key] = statementStartTime(s.statsSince, s.statsReset, now)
		p.recordStatement(ts, s)
	}
	if skipped > 0 {
		// A statement that cannot be parsed or obfuscated is skipped on its own
		// so that the remaining statements are still reported.
		errs.addPartial(fmt.Errorf("statement metrics skipped %d pg_stat_statements entries with incomplete identity, text or counters", skipped))
	}
	return starts
}

func (p *postgreSQLScraper) recordStatement(ts pcommon.Timestamp, s statementStats) {
	mb, k := p.mb, s.key
	mb.RecordPostgresqlStatementCallsDataPoint(ts, s.calls, k.database, k.role, k.queryID, k.topLevel, s.text)
	mb.RecordPostgresqlStatementRowsDataPoint(ts, s.rows, k.database, k.role, k.queryID, k.topLevel, s.text)
	mb.RecordPostgresqlStatementExecutionTimeDataPoint(ts, s.execSeconds, k.database, k.role, k.queryID, k.topLevel, s.text)
	mb.RecordPostgresqlStatementPlanningTimeDataPoint(ts, s.planSeconds, k.database, k.role, k.queryID, k.topLevel, s.text)
	for i, op := range []metadata.AttributePostgresqlBlockOperation{
		metadata.AttributePostgresqlBlockOperationDirtied, metadata.AttributePostgresqlBlockOperationHit,
		metadata.AttributePostgresqlBlockOperationRead, metadata.AttributePostgresqlBlockOperationWritten,
	} {
		mb.RecordPostgresqlStatementSharedBlocksDataPoint(ts, s.shared[i], k.database, k.role, k.queryID, k.topLevel, s.text, op)
	}
	mb.RecordPostgresqlStatementTempBlocksDataPoint(ts, s.temp[0], k.database, k.role, k.queryID, k.topLevel, s.text, metadata.AttributePostgresqlBlockOperationRead)
	mb.RecordPostgresqlStatementTempBlocksDataPoint(ts, s.temp[1], k.database, k.role, k.queryID, k.topLevel, s.text, metadata.AttributePostgresqlBlockOperationWritten)
}

// statementStartTime returns when the entry's counters last started from zero:
// the per-entry stats_since (extension API 1.11), otherwise the global reset
// time (API 1.9). A reset therefore starts a new cumulative series. A start in
// the future (database clock ahead of the collector) returns zero, which keeps
// the metrics builder's stable start time.
func statementStartTime(statsSince, statsReset string, now time.Time) pcommon.Timestamp {
	var start time.Time
	if since, err := parsePostgresTimestamp(statsSince); err == nil {
		start = since
	} else if reset, err := parseEpochSeconds(statsReset); err == nil {
		start = reset
	}
	if start.IsZero() || !start.Before(now) {
		return 0
	}
	return pcommon.NewTimestampFromTime(start)
}

func parseEpochSeconds(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("empty epoch")
	}
	whole, fraction, _ := strings.Cut(value, ".")
	seconds, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	var nanos int64
	if fraction != "" {
		fraction = (fraction + "000000000")[:9]
		if nanos, err = strconv.ParseInt(fraction, 10, 64); err != nil {
			return time.Time{}, err
		}
	}
	return time.Unix(seconds, nanos), nil
}

func parsePostgresTimestamp(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("empty timestamp")
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z07"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", value)
}

// applyStatementStartTimes sets each statement data point's start time from its
// pg_stat_statements entry. Data points whose identity attributes were disabled
// keep the metrics builder's start time.
func applyStatementStartTimes(md pmetric.Metrics, starts map[statementSeriesKey]pcommon.Timestamp) {
	if len(starts) == 0 {
		return
	}
	for _, rm := range md.ResourceMetrics().All() {
		for _, sm := range rm.ScopeMetrics().All() {
			for _, m := range sm.Metrics().All() {
				if m.Type() != pmetric.MetricTypeSum || !strings.HasPrefix(m.Name(), statementMetricPrefix) {
					continue
				}
				for _, dp := range m.Sum().DataPoints().All() {
					key, ok := statementKeyFromAttributes(dp.Attributes())
					if !ok {
						continue
					}
					if start := starts[key]; start != 0 {
						dp.SetStartTimestamp(start)
					}
				}
			}
		}
	}
}

func statementKeyFromAttributes(attrs pcommon.Map) (statementSeriesKey, bool) {
	database, ok1 := attrs.Get(string(semconv.DBNamespaceKey))
	role, ok2 := attrs.Get("postgresql.rolname")
	queryID, ok3 := attrs.Get("postgresql.queryid")
	topLevel, ok4 := attrs.Get("postgresql.toplevel")
	if !ok1 || !ok2 || !ok3 || !ok4 || topLevel.Type() != pcommon.ValueTypeBool {
		return statementSeriesKey{}, false
	}
	return statementSeriesKey{database: database.Str(), role: role.Str(), queryID: queryID.Str(), topLevel: topLevel.Bool()}, true
}
