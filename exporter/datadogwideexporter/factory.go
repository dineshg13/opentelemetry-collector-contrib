// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter"

import (
	"context"
	"sync"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configoptional"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const typeStr = "datadogwide"

var stability = component.StabilityLevelAlpha

type factory struct {
	mu        sync.Mutex
	exporters map[component.ID]*wideExporter
}

func NewFactory() exporter.Factory {
	f := &factory{exporters: make(map[component.ID]*wideExporter)}
	return exporter.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		exporter.WithTraces(f.createTracesExporter, stability),
		exporter.WithMetrics(f.createMetricsExporter, stability),
		exporter.WithLogs(f.createLogsExporter, stability),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		QueueSettings:   configoptional.Some(exporterhelper.NewDefaultQueueConfig()),
		BackOffConfig:   configretry.NewDefaultBackOffConfig(),
		TimeoutSettings: exporterhelper.TimeoutConfig{Timeout: defaultHTTPTimeout},
		API: APIConfig{
			Site: defaultSite,
		},
		Wide: WideConfig{
			FlushInterval:    defaultFlushInterval,
			MaxEnvelopeBytes: DefaultWideEnvelopeMaxBytes,
		},
		Correlation: CorrelationConfig{
			GraceWindow:   defaultGraceWindow,
			OrphanTimeout: defaultOrphanTimeout,
			SweepInterval: defaultSweepInterval,
		},
	}
}

func (f *factory) createTracesExporter(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Traces, error) {
	oCfg := cfg.(*Config)
	exp, err := f.acquire(set, oCfg)
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewTraces(
		ctx,
		set,
		cfg,
		exp.consumeTraces,
		exporterhelper.WithStart(exp.start),
		exporterhelper.WithShutdown(func(ctx context.Context) error { return f.release(set.ID, ctx) }),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		exporterhelper.WithTimeout(oCfg.TimeoutSettings),
		exporterhelper.WithRetry(oCfg.BackOffConfig),
		exporterhelper.WithQueue(oCfg.QueueSettings),
	)
}

func (f *factory) createMetricsExporter(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Metrics, error) {
	oCfg := cfg.(*Config)
	exp, err := f.acquire(set, oCfg)
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewMetrics(
		ctx,
		set,
		cfg,
		exp.consumeMetrics,
		exporterhelper.WithStart(exp.start),
		exporterhelper.WithShutdown(func(ctx context.Context) error { return f.release(set.ID, ctx) }),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		exporterhelper.WithTimeout(oCfg.TimeoutSettings),
		exporterhelper.WithRetry(oCfg.BackOffConfig),
		exporterhelper.WithQueue(oCfg.QueueSettings),
	)
}

func (f *factory) createLogsExporter(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Logs, error) {
	oCfg := cfg.(*Config)
	exp, err := f.acquire(set, oCfg)
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewLogs(
		ctx,
		set,
		cfg,
		exp.consumeLogs,
		exporterhelper.WithStart(exp.start),
		exporterhelper.WithShutdown(func(ctx context.Context) error { return f.release(set.ID, ctx) }),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		exporterhelper.WithTimeout(oCfg.TimeoutSettings),
		exporterhelper.WithRetry(oCfg.BackOffConfig),
		exporterhelper.WithQueue(oCfg.QueueSettings),
	)
}

func (f *factory) acquire(set exporter.Settings, cfg *Config) (*wideExporter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if exp := f.exporters[set.ID]; exp != nil {
		exp.refs++
		return exp, nil
	}
	exp, err := newWideExporter(set, cfg)
	if err != nil {
		return nil, err
	}
	exp.refs = 1
	f.exporters[set.ID] = exp
	return exp, nil
}

func (f *factory) release(id component.ID, ctx context.Context) error {
	f.mu.Lock()
	exp := f.exporters[id]
	if exp == nil {
		f.mu.Unlock()
		return nil
	}
	exp.refs--
	if exp.refs > 0 {
		f.mu.Unlock()
		return nil
	}
	delete(f.exporters, id)
	f.mu.Unlock()
	return exp.shutdown(ctx)
}
