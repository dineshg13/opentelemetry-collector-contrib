// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"go.opentelemetry.io/collector/scraper/scrapererror"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

func TestQueryMonitoringConfig(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	require.False(t, cfg.QueryMonitoring.Enabled)
	require.Equal(t, int64(10000), cfg.QueryMonitoring.MaxRows)
	require.Equal(t, 1024*1024, cfg.QueryMonitoring.MaxPayloadBytes)
	cm := confmap.NewFromStringMap(map[string]any{
		"username": "monitor", "password": "test", "query_monitoring": map[string]any{"enabled": true},
	})
	require.NoError(t, cm.Unmarshal(cfg))
	require.NoError(t, confmap.Validate(cfg))
	for _, test := range []struct {
		name   string
		modify func(*Config)
	}{
		{"zero rows", func(c *Config) { c.QueryMonitoring.MaxRows = 0 }},
		{"too many rows", func(c *Config) { c.QueryMonitoring.MaxRows = 100001 }},
		{"small payload", func(c *Config) { c.QueryMonitoring.MaxPayloadBytes = 1023 }},
		{"large payload", func(c *Config) { c.QueryMonitoring.MaxPayloadBytes = 4*1024*1024 + 1 }},
		{"zero interval", func(c *Config) { c.ControllerConfig.CollectionInterval = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			modified := *cfg
			test.modify(&modified)
			require.Error(t, confmap.Validate(&modified))
		})
	}
}

func TestQueryMonitoringFactoryLifecycle(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := createDefaultConfig().(*Config)
		cfg.Username, cfg.Password = "monitor", "test"
		cfg.QueryMonitoring.Enabled = enabled
		cfg.ControllerConfig.InitialDelay = time.Hour
		cfg.LogsBuilderConfig.Events.DbServerQuerySample.Enabled = true
		cfg.LogsBuilderConfig.Events.DbServerTopQuery.Enabled = true
		r, err := NewFactory().CreateLogs(t.Context(), receivertest.NewNopSettings(metadata.Type), cfg, consumertest.NewNop())
		require.NoError(t, err)
		require.NoError(t, r.Start(t.Context(), componenttest.NewNopHost()))
		require.NoError(t, r.Shutdown(t.Context()))
	}
}

func monitoringTestClient(t *testing.T) (*postgreSQLScraper, client, sqlmock.Sqlmock) {
	t.Helper()
	scraper, mock := newTopQueryTestScraper(t)
	scraper.config.QueryMonitoring.Enabled = true
	c, err := scraper.clientFactory.getClient(t.Context(), defaultPostgreSQLDatabase)
	require.NoError(t, err)
	return scraper, c, mock
}

func testMonitoringWindow() (time.Time, time.Time) {
	end := time.Date(2026, 9, 22, 12, 0, 5, 0, time.UTC)
	return end.Add(-10 * time.Second), end
}

func monitoringTestRecord(t *testing.T, logs plog.Logs, event string) plog.LogRecord {
	t.Helper()
	require.Equal(t, 1, logs.LogRecordCount())
	rl := logs.ResourceLogs().At(0)
	service, exists := rl.Resource().Attributes().Get("service.name")
	require.True(t, exists)
	require.NotEmpty(t, service.Str())
	scope := rl.ScopeLogs().At(0)
	require.Equal(t, metadata.ScopeName, scope.Scope().Name())
	record := scope.LogRecords().At(0)
	require.Equal(t, event, record.EventName())
	attrs := record.Attributes().AsRaw()
	require.Equal(t, "1", attrs["db.collection.schema.version"])
	require.Equal(t, true, attrs["db.collection.complete"])
	_, err := uuid.Parse(attrs["db.collection.id"].(string))
	require.NoError(t, err)
	require.Less(t, attrs["db.collection.start_time_unix_nano"].(int64), attrs["db.collection.end_time_unix_nano"].(int64))
	require.Equal(t, record.Timestamp().AsTime().UnixNano(), attrs["db.collection.end_time_unix_nano"])
	return record
}

func TestQueryMonitoringMetricsIgnoresTopN(t *testing.T) {
	p, c, mock := monitoringTestClient(t)
	p.config.TopQueryCollection.TopNQuery = 1
	start, end := testMonitoringWindow()
	observations := []topQueryObservation{queryObservation(10, 20, 300), queryObservation(100, 200, 3000), queryObservation(1000, 2000, 30000)}
	for i := range observations {
		observations[i].queryID = []string{"1", "2", "3"}[i]
	}
	for scrape := range 2 {
		mock.ExpectQuery("LIMIT 10001").WillReturnRows(topQueryObservationRows(observations...))
		logs, err := p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
		require.NoError(t, err)
		record := monitoringTestRecord(t, logs, queryMetricsEvent)
		queries := record.Body().Map().AsRaw()["queries"].([]any)
		require.Len(t, queries, scrape*len(observations))
		if scrape == 1 {
			for _, value := range queries {
				row := value.(map[string]any)
				assert.Equal(t, int64(1), row["postgresql.calls"])
				assert.InDelta(t, 0.5, row["postgresql.total_exec_time"], 1e-12)
				assert.Equal(t, true, row["postgresql.toplevel"])
				assert.Equal(t, "app", row["user.name"])
				assert.NotContains(t, row, "postgresql.raw_query")
				assert.NotContains(t, row, "postgresql.rolname")
			}
		}
		for i := range observations {
			observations[i].calls++
			observations[i].rows += 2
			observations[i].executionMS += 500
		}
		start, end = end, end.Add(10*time.Second)
	}
}

func monitoringActivityRows() *sqlmock.Rows {
	return newSQLMockRows(querySampleColumns, map[string]any{
		querySampleColumnDatname: "dbm", querySampleColumnUsename: "app",
		querySampleColumnState: "active", querySampleColumnPID: "123",
		querySampleColumnQueryStart:           "2026-09-22T12:00:00.123456Z",
		querySampleColumnQuery:                "/* secret-comment,traceparent='00-0123456789abcdef0123456789abcdef-0123456789abcdef-01' */ SELECT * FROM customers WHERE password = 'secret-value'",
		querySampleColumnDurationMilliseconds: "1250", querySampleColumnQueryID: "42",
		querySampleColumnClientPort: "1234", querySampleColumnClientAddr: "127.0.0.1",
		querySampleColumnBlockingPIDs: "{456,789}", querySampleColumnApplicationName: "application",
	})
}

func monitoringConnectionRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"datname", "usename", "application_name", "state", "connections"}).
		AddRow("dbm", "app", "application", "active", "1").
		AddRow("dbm", "app", "application", "idle", "7")
}

func TestQueryMonitoringActivityPreservesSnapshotAndContext(t *testing.T) {
	p, c, mock := monitoringTestClient(t)
	start, end := testMonitoringWindow()
	var previousID any
	for range 2 {
		mock.ExpectQuery("(?s)AT TIME ZONE 'UTC'.*backend_type = 'client backend'.*state != 'idle'.*LIMIT 10001").WillReturnRows(monitoringActivityRows())
		mock.ExpectQuery("(?s)COUNT\\(\\*\\).*backend_type = 'client backend'.*LIMIT 10001").WillReturnRows(monitoringConnectionRows())
		logs, err := p.collectMonitoringActivity(t.Context(), c, "16.15", start, end)
		require.NoError(t, err)
		record := monitoringTestRecord(t, logs, queryActivityEvent)
		id := record.Attributes().AsRaw()["db.collection.id"]
		assert.NotEqual(t, previousID, id, "new observation of the same running query is a new collection")
		previousID = id
		body := record.Body().Map().AsRaw()
		sessions := body["sessions"].([]any)
		require.Len(t, sessions, 1)
		row := sessions[0].(map[string]any)
		assert.Equal(t, 1.25, row["postgresql.total_exec_time"])
		assert.Equal(t, "2026-09-22T12:00:00.123456Z", row["postgresql.query_start"])
		assert.Equal(t, []any{int64(456), int64(789)}, row["postgresql.blocking.pids"])
		assert.Equal(t, "0123456789abcdef0123456789abcdef", row["trace_id"])
		assert.Equal(t, "0123456789abcdef", row["span_id"])
		assert.Equal(t, int64(1), row["trace_flags"])
		assert.Equal(t, []any{"customers"}, row["db.query.tables"])
		assert.Equal(t, []any{"SELECT"}, row["db.query.commands"])
		connections := body["connections"].([]any)
		require.Len(t, connections, 2)
		assert.Equal(t, int64(7), connections[1].(map[string]any)["postgresql.connections"])
		encoded, err := plogotlp.NewExportRequestFromLogs(logs).MarshalJSON()
		require.NoError(t, err)
		for _, private := range []string{"secret-comment", "secret-value", "raw_query", querySampleTraceContextKey} {
			assert.NotContains(t, string(encoded), private)
		}
		// Ordinary OTLP serialization/retry preserves the source IDs.
		roundTrip := plogotlp.NewExportRequest()
		require.NoError(t, roundTrip.UnmarshalJSON(encoded))
		assert.Equal(t, id, monitoringTestRecord(t, roundTrip.Logs(), queryActivityEvent).Attributes().AsRaw()["db.collection.id"])
		start, end = end, end.Add(10*time.Second)
	}
}

func TestQueryMonitoringEmptyAndFailedCollections(t *testing.T) {
	for _, failure := range []string{"none", "metrics", "activity", "connections"} {
		t.Run(failure, func(t *testing.T) {
			p, _, mock := monitoringTestClient(t)
			mock.ExpectQuery("SHOW server_version").WillReturnRows(sqlmock.NewRows([]string{"server_version"}).AddRow("16.15"))
			q := mock.ExpectQuery("LIMIT 10001")
			if failure == "metrics" {
				q.WillReturnError(errors.New("statistics denied"))
			} else {
				q.WillReturnRows(topQueryObservationRows())
			}
			q = mock.ExpectQuery("(?s)FROM pg_stat_activity sa.*LIMIT 10001")
			if failure == "activity" {
				q.WillReturnError(errors.New("activity denied"))
			} else {
				q.WillReturnRows(sqlmock.NewRows(querySampleColumns))
				q = mock.ExpectQuery("(?s)COUNT\\(\\*\\).*LIMIT 10001")
				if failure == "connections" {
					q.WillReturnError(errors.New("connections denied"))
				} else {
					q.WillReturnRows(sqlmock.NewRows([]string{"datname", "usename", "application_name", "state", "connections"}))
				}
			}
			logs, err := p.scrapeQueryMonitoring(t.Context(), &queryMonitoringState{})
			if failure == "none" {
				require.NoError(t, err)
				require.Equal(t, 2, logs.LogRecordCount(), "empty successful snapshots must be emitted")
			} else {
				require.Error(t, err)
				require.True(t, scrapererror.IsPartialScrapeError(err))
				require.Equal(t, 1, logs.LogRecordCount(), "successful family survives, failed family emits no false empty snapshot")
			}
		})
	}
}

func TestQueryMonitoringLimits(t *testing.T) {
	t.Run("statistics cap clears baseline", func(t *testing.T) {
		p, c, mock := monitoringTestClient(t)
		p.config.QueryMonitoring.MaxRows = 1
		start, end := testMonitoringWindow()
		mock.ExpectQuery("LIMIT 2").WillReturnRows(topQueryObservationRows(queryObservation(1, 2, 3), queryObservation(4, 5, 6)))
		logs, err := p.collectMonitoringMetrics(t.Context(), c, "16", start, end)
		require.ErrorContains(t, err, "exceed max_rows")
		require.Zero(t, logs.LogRecordCount())
		require.Zero(t, p.cache.Len())
	})
	t.Run("connection cap does not emit a partial activity snapshot", func(t *testing.T) {
		p, c, mock := monitoringTestClient(t)
		p.config.QueryMonitoring.MaxRows = 1
		start, end := testMonitoringWindow()
		mock.ExpectQuery("(?s)FROM pg_stat_activity sa.*LIMIT 2").WillReturnRows(monitoringActivityRows())
		mock.ExpectQuery("(?s)COUNT\\(\\*\\).*LIMIT 2").WillReturnRows(monitoringConnectionRows())
		logs, err := p.collectMonitoringActivity(t.Context(), c, "16", start, end)
		require.ErrorContains(t, err, "connection groups exceed")
		require.Zero(t, logs.LogRecordCount())
	})
	t.Run("clock skew does not emit future activity", func(t *testing.T) {
		p, c, mock := monitoringTestClient(t)
		start, end := testMonitoringWindow()
		mock.ExpectQuery("(?s)FROM pg_stat_activity sa.*LIMIT 10001").WillReturnRows(monitoringActivityRows())
		mock.ExpectQuery("(?s)COUNT\\(\\*\\).*LIMIT 10001").WillReturnRows(monitoringConnectionRows())
		logs, err := p.collectMonitoringActivity(t.Context(), c, "16", start.Add(-time.Hour), end.Add(-time.Hour))
		require.ErrorContains(t, err, "query_start exceeds collection end")
		require.Zero(t, logs.LogRecordCount())
	})
	t.Run("payload cap includes OTLP envelope", func(t *testing.T) {
		p, _, _ := monitoringTestClient(t)
		p.config.QueryMonitoring.MaxPayloadBytes = 1024
		start, end := testMonitoringWindow()
		logs, err := p.monitoringRecord(queryMetricsEvent, map[string]any{"queries": []any{map[string]any{"db.query.text": strings.Repeat("x", 1000)}}}, 1, 1, "16", start, end)
		require.ErrorContains(t, err, "exceeding max_payload_bytes")
		require.Zero(t, logs.LogRecordCount())
	})
	t.Run("invalid interval is not emitted", func(t *testing.T) {
		p, _, _ := monitoringTestClient(t)
		_, end := testMonitoringWindow()
		logs, err := p.monitoringRecord(queryActivityEvent, map[string]any{"sessions": []any{}, "connections": []any{}}, 0, 0, "16", end, end)
		require.Error(t, err)
		require.Zero(t, logs.LogRecordCount())
	})
}

func TestQueryMonitoringConnectionsExcludeDatabases(t *testing.T) {
	p, c, mock := monitoringTestClient(t)
	connectionClient := c.(queryConnectionClient)
	mock.ExpectQuery("(?s)backend_type = 'client backend'.*NOT IN \\('private''db'\\).*GROUP BY.*LIMIT 11").WillReturnRows(monitoringConnectionRows())
	rows, err := connectionClient.getQueryConnections(t.Context(), 11, []string{"private'db"}, p.logger)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "idle", rows[1]["postgresql.state"])
	assert.Equal(t, int64(7), rows[1]["postgresql.connections"])
}

func TestMonitoringTimestampAndBlockingPIDs(t *testing.T) {
	for _, value := range []string{"2026-09-22 14:00:00.123456+02", "2026-09-22 14:00:00.123456+02:00", "2026-09-22T12:00:00.123456Z"} {
		actual, err := monitoringQueryStart(value)
		require.NoError(t, err)
		assert.Equal(t, "2026-09-22T12:00:00.123456Z", actual)
	}
	for _, value := range []string{"", "2026-09-22 12:00:00", "not-a-date"} {
		_, err := monitoringQueryStart(value)
		require.Error(t, err)
	}
	for _, value := range []string{"{0}", "{-1}", "{NULL}", "123", "{1,}", "{9223372036854775808}"} {
		_, err := monitoringBlockingPIDs(value)
		require.Error(t, err)
	}
	actual, err := monitoringBlockingPIDs("{}")
	require.NoError(t, err)
	assert.Empty(t, actual)
}
