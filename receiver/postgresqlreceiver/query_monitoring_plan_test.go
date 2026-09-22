// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestMonitoringPlanConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Username, cfg.Password = "monitor", "test"
	cfg.QueryMonitoring.Enabled = true
	require.False(t, cfg.QueryMonitoring.QueryPlans.Enabled)
	cfg.QueryMonitoring.QueryPlans.Enabled = true
	require.NoError(t, confmap.Validate(cfg))
	for _, modify := range []func(*QueryMonitoringPlans){
		func(c *QueryMonitoringPlans) { c.MaxPerCollection = 0 },
		func(c *QueryMonitoringPlans) { c.MaxPerCollection = 21 },
		func(c *QueryMonitoringPlans) { c.Timeout = 0 },
		func(c *QueryMonitoringPlans) { c.Timeout = 6 * time.Second },
		func(c *QueryMonitoringPlans) { c.MaxPlanBytes = 1023 },
		func(c *QueryMonitoringPlans) { c.MaxPlanBytes = 2 * 1024 * 1024 },
		func(c *QueryMonitoringPlans) { c.CacheSize = 0 },
		func(c *QueryMonitoringPlans) { c.CacheSize = 10001 },
		func(c *QueryMonitoringPlans) { c.CacheTTL = 0 },
	} {
		modified := *cfg
		modify(&modified.QueryMonitoring.QueryPlans)
		require.Error(t, confmap.Validate(&modified))
	}
}

func expectMonitoringPlan(mock sqlmock.Sqlmock, queryID, query, plan string) {
	mock.ExpectQuery(regexp.QuoteMeta("/* otel-collector-ignore */ SET plan_cache_mode = force_generic_plan;PREPARE otel_" + queryID + " AS " + query + ";")).
		WillReturnRows(sqlmock.NewRows([]string{"result"}))
	mock.ExpectQuery("SELECT COALESCE.*pg_prepared_statements").WillReturnRows(sqlmock.NewRows([]string{"param_count"}).AddRow("0"))
	// Exact SQL ensures plan collection never introduces ANALYZE.
	mock.ExpectQuery(regexp.QuoteMeta("EXPLAIN(FORMAT JSON) EXECUTE otel_" + queryID + ";")).
		WillReturnRows(sqlmock.NewRows([]string{"QUERY PLAN"}).AddRow(plan))
	mock.ExpectExec("DEALLOCATE PREPARE otel_" + queryID).WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestMonitoringPlansBoundAttemptsAndCacheNativeIdentity(t *testing.T) {
	p, c, mock := monitoringTestClient(t)
	p.config.QueryMonitoring.QueryPlans.Enabled = true
	p.config.QueryMonitoring.QueryPlans.MaxPerCollection = 1
	start, end := testMonitoringWindow()
	a, b := queryObservation(10, 20, 30), queryObservation(10, 20, 30)
	a.queryID, b.queryID = "42", "42"
	b.userID = "3"
	expectMonitoringStatistics(mock, "LIMIT 10001").WillReturnRows(topQueryObservationRows(a, b))
	_, err := p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
	require.NoError(t, err)
	for collection := range 3 {
		a.calls++
		b.calls++
		expectMonitoringStatistics(mock, "LIMIT 10001").WillReturnRows(topQueryObservationRows(a, b))
		if collection < 2 {
			expectMonitoringPlan(mock, "42", "SELECT 42", `[{"Plan":{"Node Type":"Result","Filter":"(id = 42)"}}]`)
		}
		logs, err := p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
		require.NoError(t, err)
		queries := monitoringTestRecord(t, logs, queryMetricsEvent).Body().Map().AsRaw()["queries"].([]any)
		require.Len(t, queries, 2)
		for i, value := range queries {
			row := value.(map[string]any)
			if collection == 0 && i == 1 {
				assert.NotContains(t, row, "postgresql.query_plan")
				continue
			}
			plan := row["postgresql.query_plan"].(string)
			assert.NotContains(t, plan, "42")
			assert.Contains(t, plan, "Result")
			assert.Equal(t, int64(1), row["postgresql.calls"])
		}
	}
}

func TestMonitoringPlanFailuresKeepMetrics(t *testing.T) {
	for _, failure := range []string{"permission", "timeout", "oversized", "snapshot size"} {
		t.Run(failure, func(t *testing.T) {
			p, c, mock := monitoringTestClient(t)
			core, logs := observer.New(zapcore.DebugLevel)
			p.logger = zap.New(core)
			p.config.QueryMonitoring.QueryPlans.Enabled = true
			p.config.QueryMonitoring.QueryPlans.Timeout = 10 * time.Millisecond
			p.config.QueryMonitoring.QueryPlans.MaxPlanBytes = 1024
			start, end := testMonitoringWindow()
			observation := queryObservation(10, 20, 30)
			observation.queryID = "42"
			expectMonitoringStatistics(mock, "LIMIT 10001").WillReturnRows(topQueryObservationRows(observation))
			_, err := p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
			require.NoError(t, err)
			for collection := range 2 {
				observation.calls++
				expectMonitoringStatistics(mock, "LIMIT 10001").WillReturnRows(topQueryObservationRows(observation))
				if collection == 0 {
					switch failure {
					case "oversized", "snapshot size":
						if failure == "snapshot size" {
							p.config.QueryMonitoring.QueryPlans.MaxPlanBytes = 20000
							p.config.QueryMonitoring.MaxPayloadBytes = 5000
						}
						expectMonitoringPlan(mock, "42", "SELECT 42", `[{"Plan":{"Node Type":"`+strings.Repeat("x", 6000)+`"}}]`)
					default:
						prepare := mock.ExpectQuery("SET plan_cache_mode.*PREPARE otel_42")
						if failure == "timeout" {
							prepare.WillDelayFor(100 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"result"}))
						} else {
							prepare.WillReturnError(errors.New("permission denied for private query"))
						}
						mock.ExpectExec("DEALLOCATE PREPARE otel_42").WillReturnResult(sqlmock.NewResult(0, 0))
					}
				}
				logs, err := p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
				require.NoError(t, err)
				queries := monitoringTestRecord(t, logs, queryMetricsEvent).Body().Map().AsRaw()["queries"].([]any)
				require.Len(t, queries, 1)
				row := queries[0].(map[string]any)
				assert.Equal(t, int64(1), row["postgresql.calls"])
				assert.NotContains(t, row, "postgresql.query_plan")
			}
			for _, entry := range logs.All() {
				assert.NotContains(t, entry.Message, "private query")
				assert.NotContains(t, entry.ContextMap(), "preparedQuery")
				assert.NotContains(t, entry.ContextMap(), "query")
				assert.NotContains(t, entry.ContextMap(), "error")
			}
		})
	}
}

func TestMonitoringPlanCacheExpires(t *testing.T) {
	p, _, mock := monitoringTestClient(t)
	p.config.QueryMonitoring.QueryPlans.Enabled = true
	p.queryPlanCache = newTTLCache[string](1, 10*time.Millisecond)
	row := map[string]any{
		"postgresql.dbid": "1", "postgresql.userid": "2", "postgresql.queryid": "42",
		"postgresql.toplevel": true, "db.namespace": "postgres", "postgresql.raw_query": "SELECT 42",
	}
	attempts := 0
	expectMonitoringPlan(mock, "42", "SELECT 42", `[{"Plan":{"Node Type":"Result"}}]`)
	require.NotEmpty(t, p.monitoringQueryPlan(t.Context(), row, &attempts))
	require.Equal(t, 1, attempts)
	require.NotEmpty(t, p.monitoringQueryPlan(t.Context(), row, &attempts))
	require.Equal(t, 1, attempts)
	// Expiry is the behavior under test; a new attempt must follow the TTL.
	time.Sleep(20 * time.Millisecond)
	expectMonitoringPlan(mock, "42", "SELECT 42", `[{"Plan":{"Node Type":"Result"}}]`)
	require.NotEmpty(t, p.monitoringQueryPlan(t.Context(), row, &attempts))
	require.Equal(t, 2, attempts)
}
