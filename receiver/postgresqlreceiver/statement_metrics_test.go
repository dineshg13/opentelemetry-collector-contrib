// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.opentelemetry.io/collector/scraper/scrapererror"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

const statementEpochPattern = `EXTRACT\(EPOCH FROM stats_reset\).*pg_stat_statements_info`

var statementColumns = []string{
	"datname", "rolname", "queryid", "toplevel", "stats_since", "query", "calls", "rows",
	"total_exec_time", "total_plan_time", "shared_blks_dirtied", "shared_blks_hit",
	"shared_blks_read", "shared_blks_written", "temp_blks_read", "temp_blks_written",
}

type statementRow struct {
	queryID, topLevel, statsSince string
	calls, rows                   int64
	executionMS                   float64
}

func statementRows(rows ...statementRow) *sqlmock.Rows {
	result := sqlmock.NewRows(statementColumns)
	for _, r := range rows {
		result.AddRow("postgres", "app", r.queryID, r.topLevel, r.statsSince, "SELECT 42",
			fmt.Sprint(r.calls), fmt.Sprint(r.rows), fmt.Sprint(r.executionMS), "100",
			"1", "2", "3", "4", "5", "6")
	}
	return result
}

func epochRows(epoch string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"stats_reset"}).AddRow(epoch)
}

func statementTestScraper(t *testing.T, version string) (sqlmock.Sqlmock, func() (pmetric.Metrics, error)) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		assert.NoError(t, db.Close())
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	cfg := createDefaultConfig().(*Config)
	m := &cfg.MetricsBuilderConfig.Metrics
	m.PostgresqlStatementCalls.Enabled = true
	m.PostgresqlStatementRows.Enabled = true
	m.PostgresqlStatementExecutionTime.Enabled = true
	m.PostgresqlStatementPlanningTime.Enabled = true
	m.PostgresqlStatementSharedBlocks.Enabled = true
	m.PostgresqlStatementTempBlocks.Enabled = true
	p, err := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), cfg, mockSimpleClientFactory{db: db}, newCache(1), newTTLCache[string](1, time.Second))
	require.NoError(t, err)
	p.dbVersion = version
	c, err := p.clientFactory.getClient(t.Context(), defaultPostgreSQLDatabase)
	require.NoError(t, err)
	return mock, func() (pmetric.Metrics, error) {
		var errs errsMux
		starts := p.collectStatementMetrics(t.Context(), c, &errs)
		md := p.mb.Emit()
		applyStatementStartTimes(md, starts)
		return md, errs.combine()
	}
}

func statementMetric(t *testing.T, md pmetric.Metrics, name string) pmetric.Metric {
	t.Helper()
	for _, rm := range md.ResourceMetrics().All() {
		for _, sm := range rm.ScopeMetrics().All() {
			for _, m := range sm.Metrics().All() {
				if m.Name() == name {
					return m
				}
			}
		}
	}
	require.Failf(t, "metric not found", name)
	return pmetric.Metric{}
}

func statementPoint(t *testing.T, m pmetric.Metric, queryID string) pmetric.NumberDataPoint {
	t.Helper()
	for _, dp := range m.Sum().DataPoints().All() {
		if id, _ := dp.Attributes().Get("postgresql.queryid"); id.Str() == queryID {
			return dp
		}
	}
	require.Failf(t, "data point not found", queryID)
	return pmetric.NumberDataPoint{}
}

func TestStatementMetricsAreCumulativeWithResetAwareStart(t *testing.T) {
	mock, collect := statementTestScraper(t, "16.15")
	a := statementRow{queryID: "1", topLevel: "true", calls: 10, rows: 20, executionMS: 1500}
	b := statementRow{queryID: "-2", topLevel: "false", calls: 3, rows: 4, executionMS: 250}
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1790000000250000"))
	mock.ExpectQuery("ORDER BY calls DESC.*LIMIT 5000").WillReturnRows(statementRows(a, b))
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1790000000250000"))

	md, err := collect()
	require.NoError(t, err)
	calls := statementMetric(t, md, "postgresql.statement.calls")
	require.True(t, calls.Sum().IsMonotonic())
	require.Equal(t, pmetric.AggregationTemporalityCumulative, calls.Sum().AggregationTemporality())
	require.Equal(t, 2, calls.Sum().DataPoints().Len())

	point := statementPoint(t, calls, "1")
	assert.Equal(t, int64(10), point.IntValue(), "counters are reported as lifetime totals, not deltas")
	start := time.Unix(1790000000, 250000000).UTC()
	assert.Equal(t, start, point.StartTimestamp().AsTime())
	assert.Less(t, point.StartTimestamp(), point.Timestamp())
	attrs := point.Attributes().AsRaw()
	assert.Equal(t, "postgres", attrs["db.namespace"])
	assert.Equal(t, "app", attrs["postgresql.rolname"])
	assert.Equal(t, true, attrs["postgresql.toplevel"])
	assert.Equal(t, "SELECT ?", attrs["db.query.text"], "query text is obfuscated")
	assert.False(t, statementPoint(t, calls, "-2").Attributes().AsRaw()["postgresql.toplevel"].(bool))

	assert.InDelta(t, 1.5, statementPoint(t, statementMetric(t, md, "postgresql.statement.execution.time"), "1").DoubleValue(), 1e-12)
	assert.InDelta(t, 0.1, statementPoint(t, statementMetric(t, md, "postgresql.statement.planning.time"), "1").DoubleValue(), 1e-12)
	assert.Equal(t, int64(20), statementPoint(t, statementMetric(t, md, "postgresql.statement.rows"), "1").IntValue())
	shared := statementMetric(t, md, "postgresql.statement.shared_blocks").Sum().DataPoints()
	assert.Equal(t, 8, shared.Len(), "four operations per statement")
	for _, dp := range shared.All() {
		assert.Equal(t, start, dp.StartTimestamp().AsTime(), "every series of an entry shares its start")
		if op, _ := dp.Attributes().Get("postgresql.block.operation"); op.Str() == "hit" {
			assert.Equal(t, int64(2), dp.IntValue())
		}
	}
	assert.Equal(t, 4, statementMetric(t, md, "postgresql.statement.temp_blocks").Sum().DataPoints().Len(), "read and written per statement")
}

func TestStatementMetricsPreferEntryStatsSince(t *testing.T) {
	mock, collect := statementTestScraper(t, "17.2")
	row := statementRow{queryID: "5", topLevel: "true", statsSince: "1790676000500000", calls: 5}
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1700000000000000"))
	mock.ExpectQuery("LIMIT 5000").WillReturnRows(statementRows(row))
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1700000000000000"))
	md, err := collect()
	require.NoError(t, err)
	point := statementPoint(t, statementMetric(t, md, "postgresql.statement.calls"), "5")
	assert.Equal(t, time.Date(2026, 9, 29, 10, 0, 0, 500000000, time.UTC), point.StartTimestamp().AsTime())
}

func TestStatementMetricsPG13KeepsBuilderStart(t *testing.T) {
	mock, collect := statementTestScraper(t, "13.4")
	mock.ExpectQuery("LIMIT 5000").WillReturnRows(statementRows(statementRow{queryID: "7", topLevel: "true", calls: 1}))
	md, err := collect()
	require.NoError(t, err)
	point := statementPoint(t, statementMetric(t, md, "postgresql.statement.calls"), "7")
	assert.NotZero(t, point.StartTimestamp())
	assert.Less(t, point.StartTimestamp(), point.Timestamp())
}

func TestStatementMetricsOmittedWhenStatisticsResetDuringCollection(t *testing.T) {
	mock, collect := statementTestScraper(t, "16.15")
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1"))
	mock.ExpectQuery("LIMIT 5000").WillReturnRows(statementRows(statementRow{queryID: "1", topLevel: "true", calls: 1}))
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("2"))
	md, err := collect()
	require.ErrorContains(t, err, "reset during collection")
	require.True(t, scrapererror.IsPartialScrapeError(err))
	assert.Zero(t, md.DataPointCount(), "counters read across a reset are not reported")
}

func TestStatementMetricsEpochFailures(t *testing.T) {
	for _, failure := range []string{"missing view", "before read", "after read", "empty epoch"} {
		t.Run(failure, func(t *testing.T) {
			mock, collect := statementTestScraper(t, "16.15")
			switch failure {
			case "missing view":
				mock.ExpectQuery(statementEpochPattern).WillReturnError(&pq.Error{Code: "42P01", Message: "relation does not exist"})
			case "before read":
				mock.ExpectQuery(statementEpochPattern).WillReturnError(errors.New("permission denied"))
			case "empty epoch":
				mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows(""))
			default:
				mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1"))
				mock.ExpectQuery("LIMIT 5000").WillReturnRows(statementRows(statementRow{queryID: "1", topLevel: "true", calls: 1}))
				mock.ExpectQuery(statementEpochPattern).WillReturnError(errors.New("connection lost"))
			}
			md, err := collect()
			require.Error(t, err)
			if failure == "missing view" {
				assert.ErrorContains(t, err, "require pg_stat_statements extension 1.9 or newer")
			}
			require.Zero(t, md.DataPointCount(), "counters without a verified reset epoch are not reported")
		})
	}
}

func TestStatementMetricsSkipOnlyInvalidEntries(t *testing.T) {
	mock, collect := statementTestScraper(t, "16.15")
	good := statementRow{queryID: "1", topLevel: "true", calls: 1}
	bad := statementRow{queryID: "2", topLevel: "maybe", calls: 2}
	negative := statementRow{queryID: "3", topLevel: "true", calls: -1}
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1"))
	mock.ExpectQuery("LIMIT 5000").WillReturnRows(statementRows(good, bad, negative))
	mock.ExpectQuery(statementEpochPattern).WillReturnRows(epochRows("1"))
	md, err := collect()
	require.ErrorContains(t, err, "skipped 2 pg_stat_statements entries")
	assert.Equal(t, 1, statementMetric(t, md, "postgresql.statement.calls").Sum().DataPoints().Len())
}

func TestStatementMetricsDisabledRunNoQueries(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cfg := createDefaultConfig().(*Config)
	p, err := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), cfg, mockSimpleClientFactory{db: db}, newCache(1), newTTLCache[string](1, time.Second))
	require.NoError(t, err)
	c, err := p.clientFactory.getClient(t.Context(), defaultPostgreSQLDatabase)
	require.NoError(t, err)
	var errs errsMux
	assert.Nil(t, p.collectStatementMetrics(t.Context(), c, &errs))
	assert.NoError(t, errs.combine())
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestStatementStartTime(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	micros := func(ts time.Time) string { return fmt.Sprint(ts.UnixMicro()) }
	for _, test := range []struct {
		name, since, reset string
		want               time.Time
	}{
		{"entry since", micros(now.Add(-time.Hour)), micros(now.Add(-2 * time.Hour)), now.Add(-time.Hour)},
		{"global reset", "", "1790000000000001", time.UnixMicro(1790000000000001)},
		{"future start", micros(now.Add(time.Second)), "", time.Time{}},
		{"unknown", "", "", time.Time{}},
		{"malformed", "yesterday", "soon", time.Time{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := statementStartTime(test.since, test.reset, now)
			if test.want.IsZero() {
				assert.Zero(t, got)
				return
			}
			assert.Equal(t, pcommon.NewTimestampFromTime(test.want), got)
		})
	}
}

func TestStatementMetricsConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	require.Equal(t, int64(5000), cfg.StatementMetrics.MaxStatements)
	cm := confmap.NewFromStringMap(map[string]any{"username": "monitor", "password": "test"})
	require.NoError(t, cm.Unmarshal(cfg))
	require.NoError(t, confmap.Validate(cfg))
	for _, value := range []int64{0, 100001} {
		modified := *cfg
		modified.StatementMetrics.MaxStatements = value
		require.Error(t, confmap.Validate(&modified))
	}
}
