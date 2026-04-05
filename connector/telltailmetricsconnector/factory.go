// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate make mdatagen

package telltailmetricsconnector // import "github.com/open-telemetry/opentelemetry-collector-contrib/connector/telltailmetricsconnector"

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/connector"
	"go.opentelemetry.io/collector/consumer"

	"github.com/open-telemetry/opentelemetry-collector-contrib/connector/telltailmetricsconnector/internal/metadata"
)

// NewFactory creates a factory for the telltailmetrics connector.
func NewFactory() connector.Factory {
	return connector.NewFactory(
		metadata.Type,
		createDefaultConfig,
		connector.WithTracesToMetrics(createTracesToMetrics, metadata.TracesToMetricsStability),
		connector.WithTracesToTraces(createTracesToTraces, metadata.TracesToTracesStability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{}
}

func createTracesToMetrics(_ context.Context, set connector.Settings, cfg component.Config, next consumer.Metrics) (connector.Traces, error) {
	c := cfg.(*Config)

	excludeFromMetrics := make(map[string]struct{}, len(c.MoveToTraces))
	for _, k := range c.MoveToTraces {
		excludeFromMetrics[k] = struct{}{}
	}

	return &metricsConnector{
		logger:             set.Logger,
		metricsConsumer:    next,
		excludeFromMetrics: excludeFromMetrics,
		includeSpanAttrs:   c.MoveToMetrics,
	}, nil
}

func createTracesToTraces(_ context.Context, set connector.Settings, cfg component.Config, next consumer.Traces) (connector.Traces, error) {
	c := cfg.(*Config)

	return &tracesConnector{
		logger:        set.Logger,
		tracesConsumer: next,
		promoteToSpan: c.MoveToTraces,
	}, nil
}
