// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/receiver/receivertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver/internal/metadata"
)

type topQueryObservation struct {
	databaseID, userID, queryID, topLevel string
	statsSince                            string
	calls, rows                           int64
	executionMS, planningMS               float64
}

func queryObservation(calls, rows int64, executionMS float64) topQueryObservation {
	return topQueryObservation{
		databaseID: "1", userID: "2", queryID: "9223372036854775807", topLevel: "true",
		calls: calls, rows: rows, executionMS: executionMS, planningMS: 100,
	}
}

func topQueryObservationRows(observations ...topQueryObservation) *sqlmock.Rows {
	rows := sqlmock.NewRows(topQueryColumns)
	for _, observation := range observations {
		values := map[string]driver.Value{
			"dbid": observation.databaseID, "userid": observation.userID,
			queryidColumnName: observation.queryID, "toplevel": observation.topLevel,
			"stats_since": observation.statsSince,
			// Names intentionally stay identical: native IDs must distinguish the rows.
			"datname": "postgres", "rolname": "app", "query": "SELECT 42",
			callsColumnName: fmt.Sprint(observation.calls), rowsColumnName: fmt.Sprint(observation.rows),
			totalExecTimeColumnName: fmt.Sprint(observation.executionMS),
			totalPlanTimeColumnName: fmt.Sprint(observation.planningMS),
		}
		row := make([]driver.Value, len(topQueryColumns))
		for i, column := range topQueryColumns {
			row[i] = values[column]
			if row[i] == nil {
				row[i] = "100"
			}
		}
		rows.AddRow(row...)
	}
	return rows
}

func newTopQueryTestScraper(t *testing.T) (*postgreSQLScraper, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		assert.NoError(t, db.Close())
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	cfg := createDefaultConfig().(*Config)
	cfg.LogsBuilderConfig.Events.DbServerTopQuery.Enabled = true
	scraper, err := newTopQueryScraper(receivertest.NewNopSettings(metadata.Type), cfg, mockSimpleClientFactory{db: db})
	require.NoError(t, err)
	return scraper, mock
}

func TestTopQueryBaselinesAndResets(t *testing.T) {
	for _, test := range []struct {
		name         string
		observations []topQueryObservation
		expected     []int
		lastCalls    int64
		lastRows     int64
		lastExec     float64
	}{
		{
			name:         "initial baseline excludes lifetime totals",
			observations: []topQueryObservation{queryObservation(1000, 2000, 3000), queryObservation(1002, 2003, 3500)},
			expected:     []int{0, 1}, lastCalls: 2, lastRows: 3, lastExec: 0.5,
		},
		{
			name:         "duration changes without calls only update baseline",
			observations: []topQueryObservation{queryObservation(10, 20, 100), queryObservation(10, 20, 150), queryObservation(11, 21, 150)},
			expected:     []int{0, 0, 1}, lastCalls: 1, lastRows: 1, lastExec: 0,
		},
		{
			name:         "reset establishes lower baseline for recovery",
			observations: []topQueryObservation{queryObservation(1000, 2000, 3000), queryObservation(1, 2, 3), queryObservation(3, 5, 503)},
			expected:     []int{0, 0, 1}, lastCalls: 2, lastRows: 3, lastExec: 0.5,
		},
		{
			name:         "one counter reset invalidates whole row",
			observations: []topQueryObservation{queryObservation(10, 20, 100), queryObservation(11, 1, 200), queryObservation(12, 3, 250)},
			expected:     []int{0, 0, 1}, lastCalls: 1, lastRows: 2, lastExec: 0.05,
		},
		{
			name:         "integer deltas retain precision above two to the fifty third",
			observations: []topQueryObservation{queryObservation(1<<53, 1<<53, 100), queryObservation(1<<53+1, 1<<53+3, 200)},
			expected:     []int{0, 1}, lastCalls: 1, lastRows: 3, lastExec: 0.1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			scraper, mock := newTopQueryTestScraper(t)
			for i, observation := range test.observations {
				mock.ExpectQuery("LIMIT 1000").WillReturnRows(topQueryObservationRows(observation))
				logs, err := scraper.scrapeTopQuery(t.Context(), 1000, 200, 0, 0)
				require.NoError(t, err)
				require.Equal(t, test.expected[i], logs.LogRecordCount())
				if i == len(test.observations)-1 {
					attrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().AsRaw()
					assert.Equal(t, test.lastCalls, attrs[dbAttributePrefix+callsColumnName])
					assert.Equal(t, test.lastRows, attrs[dbAttributePrefix+rowsColumnName])
					assert.InDelta(t, test.lastExec, attrs[dbAttributePrefix+totalExecTimeColumnName], 1e-12)
				}
			}
		})
	}
}

func TestTopQueryNativeIdentity(t *testing.T) {
	scraper, mock := newTopQueryTestScraper(t)
	observations := []topQueryObservation{
		queryObservation(10, 20, 100), queryObservation(100, 200, 1000),
		queryObservation(1000, 2000, 10000), queryObservation(10000, 20000, 100000),
	}
	observations[1].databaseID = "3"
	observations[2].userID = "4"
	observations[3].topLevel = "false"
	for scrape := range 2 {
		mock.ExpectQuery("LIMIT 1000").WillReturnRows(topQueryObservationRows(observations...))
		logs, err := scraper.scrapeTopQuery(t.Context(), 1000, 200, 0, 0)
		require.NoError(t, err)
		assert.Equal(t, scrape*len(observations), logs.LogRecordCount())
		for i := range observations {
			observations[i].calls++
			observations[i].rows += 2
			observations[i].executionMS += 250
		}
		if scrape > 0 {
			for _, record := range logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().All() {
				attrs := record.Attributes().AsRaw()
				assert.Equal(t, int64(1), attrs[dbAttributePrefix+callsColumnName])
				assert.Equal(t, int64(2), attrs[dbAttributePrefix+rowsColumnName])
				assert.InDelta(t, 0.25, attrs[dbAttributePrefix+totalExecTimeColumnName], 1e-12)
				assert.Equal(t, "9223372036854775807", attrs[dbAttributePrefix+queryidColumnName])
			}
		}
	}
}

func TestTopQueryMissingObservationRebaselines(t *testing.T) {
	for _, gap := range []string{"missing row", "collection failure", "cache eviction", "restart"} {
		t.Run(gap, func(t *testing.T) {
			scraper, mock := newTopQueryTestScraper(t)
			mock.ExpectQuery("LIMIT 1000").WillReturnRows(topQueryObservationRows(queryObservation(100, 200, 300)))
			logs, err := scraper.scrapeTopQuery(t.Context(), 1000, 200, 0, 0)
			require.NoError(t, err)
			require.Zero(t, logs.LogRecordCount())
			switch gap {
			case "missing row", "collection failure":
				query := mock.ExpectQuery("LIMIT 1000")
				if gap == "collection failure" {
					query.WillReturnError(errors.New("database unavailable"))
				} else {
					query.WillReturnRows(topQueryObservationRows())
				}
				logs, err = scraper.scrapeTopQuery(t.Context(), 1000, 200, 0, 0)
				if gap == "collection failure" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				require.Zero(t, logs.LogRecordCount())
			case "cache eviction":
				scraper.cache.Resize(1)
				scraper.cache.Add(topQueryIdentity{queryID: "another"}, topQueryCounters{})
			case "restart":
				scraper, mock = newTopQueryTestScraper(t)
			}
			mock.ExpectQuery("LIMIT 1000").WillReturnRows(topQueryObservationRows(queryObservation(1000, 2000, 3000)))
			logs, err = scraper.scrapeTopQuery(t.Context(), 1000, 200, 0, 0)
			require.NoError(t, err)
			require.Zero(t, logs.LogRecordCount(), "reappearing rows must not include unobserved intervals")
			mock.ExpectQuery("LIMIT 1000").WillReturnRows(topQueryObservationRows(queryObservation(1001, 2002, 3500)))
			logs, err = scraper.scrapeTopQuery(t.Context(), 1000, 200, 0, 0)
			require.NoError(t, err)
			require.Equal(t, 1, logs.LogRecordCount())
			attrs := logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().AsRaw()
			assert.Equal(t, int64(1), attrs[dbAttributePrefix+callsColumnName])
			assert.Equal(t, int64(2), attrs[dbAttributePrefix+rowsColumnName])
			assert.Equal(t, 0.5, attrs[dbAttributePrefix+totalExecTimeColumnName])
		})
	}
}
