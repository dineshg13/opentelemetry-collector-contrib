// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:build integration

package postgresqlreceiver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

// This opt-in test creates and drops only its uniquely named test database.
// Credentials are read from the environment and never placed in evidence files.
func TestQueryMonitoringLivePostgres(t *testing.T) {
	endpoint := os.Getenv("POSTGRESQL_MONITORING_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set POSTGRESQL_MONITORING_TEST_ENDPOINT to run against a disposable PostgreSQL 16 server")
	}
	password := os.Getenv("POSTGRESQL_MONITORING_TEST_PASSWORD")
	require.NotEmpty(t, password)
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", monitoringTestDSN(endpoint, "postgres", "postgres", password, "ddot-m3-admin"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, admin.Close()) })
	var version string
	require.NoError(t, admin.QueryRowContext(ctx, "SHOW server_version").Scan(&version))
	require.True(t, strings.HasPrefix(version, "16."), "this test validates PostgreSQL 16")
	database := "ddot_m3_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.ExecContext(ctx, "CREATE DATABASE "+database)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, dropErr := admin.ExecContext(cleanupCtx, "DROP DATABASE "+database+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	setup, err := sql.Open("postgres", monitoringTestDSN(endpoint, database, "postgres", password, "ddot-m3-setup"))
	require.NoError(t, err)
	_, err = setup.ExecContext(ctx, "CREATE TABLE public.m3_probe (id integer PRIMARY KEY); INSERT INTO public.m3_probe SELECT generate_series(1,5); GRANT SELECT ON public.m3_probe TO dbm_app, dbm_monitor")
	require.NoError(t, err)
	require.NoError(t, setup.Close())

	cfg := createDefaultConfig().(*Config)
	cfg.AddrConfig.Endpoint = endpoint
	cfg.Username = "dbm_monitor"
	cfg.Password = configopaque.String(password)
	cfg.ClientConfig.Insecure = true
	cfg.QueryMonitoring.Enabled = true
	statements := &cfg.MetricsBuilderConfig.Metrics
	statements.PostgresqlStatementCalls.Enabled = true
	statements.PostgresqlStatementRows.Enabled = true
	statements.PostgresqlStatementExecutionTime.Enabled = true
	statements.PostgresqlStatementPlanningTime.Enabled = true
	statements.PostgresqlStatementSharedBlocks.Enabled = true
	statements.PostgresqlStatementTempBlocks.Enabled = true
	// Collection is scoped to the disposable database. This keeps captured OTLP
	// evidence limited to synthetic statements even on a shared PoC server.
	dbs, err := admin.QueryContext(ctx, "SELECT datname FROM pg_database WHERE datname != $1", database)
	require.NoError(t, err)
	for dbs.Next() {
		var name string
		require.NoError(t, dbs.Scan(&name))
		cfg.ExcludeDatabases = append(cfg.ExcludeDatabases, name)
	}
	require.NoError(t, dbs.Err())
	require.NoError(t, dbs.Close())
	makeScraper := func(config *Config) *postgreSQLScraper {
		p, makeErr := newPostgreSQLScraper(receivertest.NewNopSettings(metadata.Type), config, newDefaultClientFactory(config),
			newCache(1), newTTLCache[string](1, time.Second))
		require.NoError(t, makeErr)
		t.Cleanup(func() { assert.NoError(t, p.shutdown(context.Background())) })
		return p
	}
	p := makeScraper(cfg)
	state := &queryMonitoringState{}
	application := "ddot-m3-" + database[len("ddot_m3_"):len("ddot_m3_")+8]
	app, err := sql.Open("postgres", monitoringTestDSN(endpoint, database, "dbm_app", password, application))
	require.NoError(t, err)
	app.SetMaxOpenConns(3)
	t.Cleanup(func() { assert.NoError(t, app.Close()) })
	connections := make([]*sql.Conn, 3)
	for i := range connections {
		connections[i], err = app.Conn(ctx)
		require.NoError(t, err)
		connection := connections[i]
		t.Cleanup(func() { assert.NoError(t, connection.Close()) })
		_, err = connection.ExecContext(ctx, "SELECT 1")
		require.NoError(t, err)
	}
	query := "SELECT id FROM public.m3_probe ORDER BY id"
	execute := func() {
		rows, queryErr := connections[0].QueryContext(ctx, query)
		require.NoError(t, queryErr)
		count := 0
		for rows.Next() {
			var id int
			require.NoError(t, rows.Scan(&id))
			count++
		}
		require.NoError(t, rows.Err())
		require.NoError(t, rows.Close())
		require.Equal(t, 5, count)
	}
	collectStatements := func() pmetric.Metrics {
		c, clientErr := p.clientFactory.getClient(ctx, defaultPostgreSQLDatabase)
		require.NoError(t, clientErr)
		defer c.Close()
		var errs errsMux
		starts := p.collectStatementMetrics(ctx, c, &errs)
		require.NoError(t, errs.combine())
		md := p.mb.Emit()
		applyStatementStartTimes(md, starts)
		return md
	}
	execute()
	before := collectStatements()
	for range 3 {
		execute()
	}
	after := collectStatements()
	beforeCalls, beforeStart := monitoringStatementPoint(t, before, "postgresql.statement.calls", query)
	afterCalls, afterStart := monitoringStatementPoint(t, after, "postgresql.statement.calls", query)
	beforeRows, _ := monitoringStatementPoint(t, before, "postgresql.statement.rows", query)
	afterRows, _ := monitoringStatementPoint(t, after, "postgresql.statement.rows", query)
	callsDelta, rowsDelta := afterCalls.IntValue()-beforeCalls.IntValue(), afterRows.IntValue()-beforeRows.IntValue()
	assert.Equal(t, int64(3), callsDelta, "cumulative calls advance by the executions between scrapes")
	assert.Equal(t, int64(15), rowsDelta)
	assert.Equal(t, beforeStart, afterStart, "the series start is stable without a statistics reset")
	assert.Less(t, afterStart, afterCalls.Timestamp())
	execution, _ := monitoringStatementPoint(t, after, "postgresql.statement.execution.time", query)
	assert.GreaterOrEqual(t, execution.DoubleValue(), float64(0))

	const traceID = "0123456789abcdef0123456789abcdef"
	const spanID = "0123456789abcdef"
	activeResult := make(chan error, 1)
	go func() {
		_, activeErr := connections[0].ExecContext(ctx, "/*traceparent='00-"+traceID+"-"+spanID+"-01'*/ SELECT pg_sleep(5), id FROM public.m3_probe WHERE id = 1")
		activeResult <- activeErr
	}()
	require.Eventually(t, func() bool {
		var active int
		countErr := admin.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND application_name = $2 AND state = 'active' AND query LIKE '%pg_sleep%'", database, application).Scan(&active)
		return countErr == nil && active == 1
	}, 5*time.Second, 20*time.Millisecond)
	captured, err := p.scrapeQueryMonitoring(ctx, state)
	require.NoError(t, err)
	activityBody := monitoringIntegrationBody(t, captured, queryActivityEvent)
	sessions := activityBody["sessions"].([]any)
	require.Len(t, sessions, 1)
	session := sessions[0].(map[string]any)
	assert.Equal(t, traceID, session["trace_id"])
	assert.Equal(t, spanID, session["span_id"])
	assert.Equal(t, int64(1), session["trace_flags"])
	counts := map[string]int64{}
	for _, value := range activityBody["connections"].([]any) {
		row := value.(map[string]any)
		if row["postgresql.application_name"] == application {
			counts[row["postgresql.state"].(string)] += row["postgresql.connections"].(int64)
		}
	}
	assert.Equal(t, int64(1), counts["active"])
	assert.Equal(t, int64(2), counts["idle"])
	require.NoError(t, <-activeResult)

	idle, err := p.scrapeQueryMonitoring(ctx, state)
	require.NoError(t, err)
	require.Empty(t, monitoringIntegrationBody(t, idle, queryActivityEvent)["sessions"])
	failedCfg := *cfg
	failedCfg.AddrConfig.Endpoint = "127.0.0.1:1"
	failed := makeScraper(&failedCfg)
	failedRecords, err := failed.scrapeQueryMonitoring(ctx, &queryMonitoringState{})
	require.Error(t, err)
	require.Zero(t, failedRecords.LogRecordCount())

	encoded, err := plogotlp.NewExportRequestFromLogs(captured).MarshalJSON()
	require.NoError(t, err)
	if strings.Contains(string(encoded), password) {
		t.Fatal("credential unexpectedly present in OTLP capture")
	}
	require.NotContains(t, string(encoded), "postgresql.raw_query")
	require.NotContains(t, string(encoded), querySampleTraceContextKey)
	if output := os.Getenv("POSTGRESQL_MONITORING_TEST_CAPTURE"); output != "" {
		require.NoError(t, os.WriteFile(output, encoded, 0o600))
	}
	metricsEncoded, err := pmetricotlp.NewExportRequestFromMetrics(after).MarshalJSON()
	require.NoError(t, err)
	require.NotContains(t, string(metricsEncoded), password)
	if output := os.Getenv("POSTGRESQL_MONITORING_TEST_METRICS_CAPTURE"); output != "" {
		require.NoError(t, os.WriteFile(output, metricsEncoded, 0o600))
	}
	evidence := map[string]any{
		"verified_at": time.Now().UTC().Format(time.RFC3339), "postgres_version": version,
		"database": database, "database_cleanup": "registered DROP DATABASE for this test database only",
		"observed_calls_delta": callsDelta, "observed_rows_delta": rowsDelta,
		"statement_metrics_temporality": "cumulative", "statement_series_start_stable": beforeStart == afterStart,
		"active_connections": counts["active"], "idle_connections": counts["idle"],
		"sqlcommenter_context_verified": true, "context_source": "synthetic standard W3C SQL comment; no SDK/backend claim",
		"idle_activity_empty": true, "connection_failure_emits_no_snapshot": true,
		"captured_otlp_records": captured.LogRecordCount(), "capture_scope": "only the disposable synthetic database",
	}
	if output := os.Getenv("POSTGRESQL_MONITORING_TEST_EVIDENCE"); output != "" {
		data, encodeErr := json.MarshalIndent(evidence, "", "  ")
		require.NoError(t, encodeErr)
		require.NoError(t, os.WriteFile(output, append(data, '\n'), 0o600))
	}
	t.Logf("PostgreSQL %s: calls=3 rows=15 active=1 idle=2; context and failure checks passed", version)
}

func monitoringTestDSN(endpoint, database, user, password, application string) string {
	u := url.URL{Scheme: "postgres", Host: endpoint, Path: database, User: url.UserPassword(user, password)}
	u.RawQuery = url.Values{"sslmode": {"disable"}, "connect_timeout": {"5"}, "application_name": {application}}.Encode()
	return u.String()
}

func monitoringIntegrationBody(t *testing.T, logs plog.Logs, name string) map[string]any {
	t.Helper()
	for _, resource := range logs.ResourceLogs().All() {
		for _, scope := range resource.ScopeLogs().All() {
			for _, record := range scope.LogRecords().All() {
				if record.EventName() == name {
					return record.Body().Map().AsRaw()
				}
			}
		}
	}
	t.Fatal(fmt.Sprintf("missing %s record", name))
	return nil
}

func monitoringStatementPoint(t *testing.T, md pmetric.Metrics, name, text string) (pmetric.NumberDataPoint, pcommon.Timestamp) {
	t.Helper()
	for _, resource := range md.ResourceMetrics().All() {
		for _, scope := range resource.ScopeMetrics().All() {
			for _, metric := range scope.Metrics().All() {
				if metric.Name() != name {
					continue
				}
				for _, dp := range metric.Sum().DataPoints().All() {
					if value, _ := dp.Attributes().Get("db.query.text"); value.Str() == text {
						return dp, dp.StartTimestamp()
					}
				}
			}
		}
	}
	t.Fatalf("missing %s data point for %s", name, text)
	return pmetric.NumberDataPoint{}, 0
}
