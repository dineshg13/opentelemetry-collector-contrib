// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package postgresqlreceiver

import (
	"context"
	"errors"
	"fmt"
)

type queryStatisticsEpochClient interface {
	getQueryStatisticsEpoch(context.Context) (string, error)
}

func (c *postgreSQLClient) getQueryStatisticsEpoch(ctx context.Context) (string, error) {
	var epoch string
	// Epoch text avoids differences caused by session timezone/DateStyle settings.
	err := c.client.QueryRowContext(ctx, "/* otel-collector-ignore */ SELECT EXTRACT(EPOCH FROM stats_reset)::text FROM pg_stat_statements_info").Scan(&epoch)
	if err != nil {
		return "", fmt.Errorf("read statement reset epoch (query monitoring requires pg_stat_statements extension 1.9 or newer): %w", err)
	}
	if epoch == "" {
		return "", errors.New("statement reset epoch is empty")
	}
	return epoch, nil
}

func (p *postgreSQLScraper) monitoringQueryRows(ctx context.Context, c client, version string, limit int64) ([]map[string]any, error) {
	major, err := parseMajorVersion(version)
	if err != nil {
		return nil, fmt.Errorf("query monitoring PostgreSQL version: %w", err)
	}
	if major < 14 {
		// PostgreSQL 13 has no pg_stat_statements_info. Its legacy-compatible
		// counters retain decrease-based reset detection only.
		return c.getTopQuery(ctx, limit, p.excludedDatabases, p.logger)
	}
	epochClient, ok := c.(queryStatisticsEpochClient)
	if !ok {
		return nil, errors.New("query monitoring statement reset epoch is unavailable")
	}
	before, err := epochClient.getQueryStatisticsEpoch(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := c.getTopQuery(ctx, limit, p.excludedDatabases, p.logger)
	if err != nil {
		return nil, err
	}
	after, err := epochClient.getQueryStatisticsEpoch(ctx)
	if err != nil {
		return nil, err
	}
	if before != after {
		return nil, errors.New("statement statistics reset during collection; snapshot omitted")
	}
	for _, row := range rows {
		// Internal baseline metadata; the OTLP attribute allowlist excludes it.
		row[dbAttributePrefix+"stats_reset"] = after
	}
	return rows, nil
}
