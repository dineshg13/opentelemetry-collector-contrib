// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/postgresqlreceiver"

import (
	"errors"
	"fmt"
	"math"

	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// A scraper owns one database server. Native IDs prevent similarly named queries,
// databases, or roles from sharing a baseline, including nested statements.
type topQueryIdentity struct {
	databaseID string
	userID     string
	queryID    string
	topLevel   string
}

func topQueryIdentityFromRow(row map[string]any) topQueryIdentity {
	return topQueryIdentity{
		databaseID: attrString(row, dbAttributePrefix+"dbid"),
		userID:     attrString(row, dbAttributePrefix+"userid"),
		queryID:    attrString(row, dbAttributePrefix+queryidColumnName),
		topLevel:   attrString(row, dbAttributePrefix+"toplevel"),
	}
}

func (id topQueryIdentity) planCacheKey() string {
	return fmt.Sprintf("%q/%q/%q/%q", id.databaseID, id.userID, id.queryID, id.topLevel)
}

var topQueryIntegerColumns = [...]string{
	callsColumnName, rowsColumnName,
	sharedBlksDirtiedColumnName, sharedBlksHitColumnName,
	sharedBlksReadColumnName, sharedBlksWrittenColumnName,
	tempBlksReadColumnName, tempBlksWrittenColumnName,
}

var topQueryDurationColumns = [...]string{totalExecTimeColumnName, totalPlanTimeColumnName}

// Keep an entire row atomically, preserving integer precision above 2^53.
type topQueryCounters struct {
	integers  [len(topQueryIntegerColumns)]int64
	durations [len(topQueryDurationColumns)]float64
}

func topQueryCountersFromRow(row map[string]any) (topQueryCounters, bool) {
	var counters topQueryCounters
	for i, column := range topQueryIntegerColumns {
		value, ok := row[dbAttributePrefix+column].(int64)
		if !ok || value < 0 {
			return topQueryCounters{}, false
		}
		counters.integers[i] = value
	}
	for i, column := range topQueryDurationColumns {
		value, ok := row[dbAttributePrefix+column].(float64)
		if !ok || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return topQueryCounters{}, false
		}
		counters.durations[i] = value
	}
	return counters, true
}

func (c topQueryCounters) delta(previous topQueryCounters) (topQueryCounters, bool) {
	var delta topQueryCounters
	for i, value := range c.integers {
		if value < previous.integers[i] {
			return topQueryCounters{}, false
		}
		delta.integers[i] = value - previous.integers[i]
	}
	for i, value := range c.durations {
		if value < previous.durations[i] {
			return topQueryCounters{}, false
		}
		delta.durations[i] = value - previous.durations[i]
	}
	return delta, true
}

func (c topQueryCounters) apply(row map[string]any) {
	for i, column := range topQueryIntegerColumns {
		row[dbAttributePrefix+column] = c.integers[i]
	}
	for i, column := range topQueryDurationColumns {
		row[dbAttributePrefix+column] = c.durations[i]
	}
}

// topQueryDeltas updates complete row baselines before presentation filtering.
// It replaces counters in returned rows with deltas and reports new/reset rows.
// The caller owns collection timestamps and must purge state after failed fetches.
func (p *postgreSQLScraper) topQueryDeltas(rows []map[string]any) (deltas []map[string]any, baselined int, err error) {
	seen := make(map[topQueryIdentity]struct{}, len(rows))
	var errs []error
	for _, row := range rows {
		database, ok := row[string(semconv.DBNamespaceKey)].(string)
		if !ok || p.isExcluded(database) {
			// A dropped database may leave a row with no namespace.
			continue
		}
		identity := topQueryIdentityFromRow(row)
		if identity.databaseID == "" || identity.userID == "" || identity.queryID == "" || identity.topLevel == "" {
			errs = append(errs, errors.New("top query row has incomplete native identity"))
			continue
		}
		current, valid := topQueryCountersFromRow(row)
		if !valid {
			errs = append(errs, errors.New("top query row has invalid counters"))
			continue
		}
		seen[identity] = struct{}{}
		previous, exists := p.cache.Get(identity)
		// Always advance the whole baseline, including after a reset or a scrape
		// with no calls. Never emit cumulative lifetime totals for a new row.
		p.cache.Add(identity, current)
		if !exists {
			baselined++
			continue
		}
		delta, valid := current.delta(previous)
		if !valid {
			baselined++
			continue
		}
		// Duration or planning changes alone do not establish an execution.
		if delta.integers[0] == 0 {
			continue
		}
		delta.apply(row)
		deltas = append(deltas, row)
	}
	// Rows absent from a successful collection may have been evicted/reset or
	// fallen outside the fetch limit. Reappearance must establish a baseline.
	for _, identity := range p.cache.Keys() {
		if _, ok := seen[identity]; !ok {
			p.cache.Remove(identity)
		}
	}
	return deltas, baselined, errors.Join(errs...)
}
