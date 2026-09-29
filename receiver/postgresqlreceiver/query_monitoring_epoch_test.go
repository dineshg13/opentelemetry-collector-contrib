// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const monitoringEpochPattern = `EXTRACT\(EPOCH FROM stats_reset\).*pg_stat_statements_info`

func expectMonitoringEpoch(mock sqlmock.Sqlmock, epoch string) {
	mock.ExpectQuery(monitoringEpochPattern).WillReturnRows(sqlmock.NewRows([]string{"stats_reset"}).AddRow(epoch))
}

func expectMonitoringStatistics(mock sqlmock.Sqlmock, pattern string) *sqlmock.ExpectedQuery {
	expectMonitoringEpoch(mock, "1")
	query := mock.ExpectQuery(pattern)
	expectMonitoringEpoch(mock, "1")
	return query
}

func TestMonitoringResetEpochRebaselinesGrowingCounters(t *testing.T) {
	for _, test := range []struct {
		name, version string
		global, entry []string
		want          []int64
	}{
		{"global reset", "16.15", []string{"1", "2", "2"}, []string{"", "", ""}, []int64{0, 0, 1}},
		{"entry reset or reallocation", "17.0", []string{"1", "1", "1"}, []string{"epoch1", "epoch2", "epoch2"}, []int64{0, 0, 1}},
		{"unchanged epochs", "17.0", []string{"1", "1", "1"}, []string{"epoch1", "epoch1", "epoch1"}, []int64{0, 50, 1}},
		{"PG13 missing epoch columns", "13.0", nil, []string{"", "", ""}, []int64{0, 50, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, c, mock := monitoringTestClient(t)
			start, end := testMonitoringWindow()
			for i, calls := range []int64{100, 150, 151} {
				if test.global != nil {
					expectMonitoringEpoch(mock, test.global[i])
				}
				row := queryObservation(calls, calls*2, float64(calls*3))
				row.statsSince = test.entry[i]
				mock.ExpectQuery("LIMIT 10001").WillReturnRows(topQueryObservationRows(row))
				if test.global != nil {
					expectMonitoringEpoch(mock, test.global[i])
				}
				logs, err := p.collectMonitoringMetrics(t.Context(), c, test.version, start, end)
				require.NoError(t, err)
				rows := monitoringTestRecord(t, logs, queryMetricsEvent).Body().Map().AsRaw()["queries"].([]any)
				if test.want[i] == 0 {
					require.Empty(t, rows)
				} else {
					require.Len(t, rows, 1)
					attributes := rows[0].(map[string]any)
					assert.Equal(t, test.want[i], attributes["postgresql.calls"])
					assert.NotContains(t, attributes, "postgresql.stats_reset")
					assert.NotContains(t, attributes, "postgresql.stats_since")
				}
			}
		})
	}
}

func TestMonitoringEpochFailurePurgesBaseline(t *testing.T) {
	for _, failure := range []string{"missing view", "before read", "after read", "empty epoch", "reset during fetch"} {
		t.Run(failure, func(t *testing.T) {
			p, c, mock := monitoringTestClient(t)
			start, end := testMonitoringWindow()
			expectMonitoringStatistics(mock, "LIMIT 10001").WillReturnRows(topQueryObservationRows(queryObservation(100, 200, 300)))
			_, err := p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
			require.NoError(t, err)
			require.Equal(t, 1, p.cache.Len())
			switch failure {
			case "missing view":
				mock.ExpectQuery(monitoringEpochPattern).WillReturnError(&pq.Error{Code: "42P01", Message: "relation does not exist"})
			case "before read":
				mock.ExpectQuery(monitoringEpochPattern).WillReturnError(errors.New("permission denied"))
			case "empty epoch":
				expectMonitoringEpoch(mock, "")
			default:
				expectMonitoringEpoch(mock, "1")
				mock.ExpectQuery("LIMIT 10001").WillReturnRows(topQueryObservationRows(queryObservation(150, 300, 450)))
				if failure == "after read" {
					mock.ExpectQuery(monitoringEpochPattern).WillReturnError(errors.New("connection lost"))
				} else {
					expectMonitoringEpoch(mock, "2")
				}
			}
			logs, err := p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
			require.Error(t, err)
			if failure == "missing view" {
				assert.ErrorContains(t, err, "requires pg_stat_statements extension 1.9 or newer")
			}
			require.Zero(t, logs.LogRecordCount(), "an unverified epoch must not emit a false complete or empty metrics snapshot")
			require.Zero(t, p.cache.Len())
			expectMonitoringStatistics(mock, "LIMIT 10001").WillReturnRows(topQueryObservationRows(queryObservation(155, 310, 465)))
			logs, err = p.collectMonitoringMetrics(t.Context(), c, "16.15", start, end)
			require.NoError(t, err)
			require.Empty(t, monitoringTestRecord(t, logs, queryMetricsEvent).Body().Map().AsRaw()["queries"])
		})
	}
}

func TestMonitoringEpochFailurePreservesActivity(t *testing.T) {
	p, _, mock := monitoringTestClient(t)
	mock.ExpectQuery(monitoringEpochPattern).WillReturnError(&pq.Error{Code: "42P01"})
	mock.ExpectQuery("(?s)FROM pg_stat_activity sa.*LIMIT 10001").WillReturnRows(monitoringActivityRows())
	mock.ExpectQuery("(?s)COUNT\\(\\*\\).*LIMIT 10001").WillReturnRows(monitoringConnectionRows())
	logs, err := p.scrapeQueryMonitoring(t.Context(), &queryMonitoringState{version: "16.15"})
	require.ErrorContains(t, err, "requires pg_stat_statements extension 1.9 or newer")
	monitoringTestRecord(t, logs, queryActivityEvent)
}
