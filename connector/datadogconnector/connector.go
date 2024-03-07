// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogconnector // import "github.com/open-telemetry/opentelemetry-collector-contrib/connector/datadogconnector"

import (
	"context"
	"fmt"

	pb "github.com/DataDog/datadog-agent/pkg/proto/pbgo/trace"
	traceconfig "github.com/DataDog/datadog-agent/pkg/trace/config"
	"github.com/DataDog/datadog-agent/pkg/trace/timing"
	"github.com/DataDog/datadog-go/v5/statsd"
	"github.com/DataDog/opentelemetry-mapping-go/pkg/otlp/attributes"
	"github.com/DataDog/opentelemetry-mapping-go/pkg/otlp/metrics"
	"github.com/patrickmn/go-cache"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	semconv "go.opentelemetry.io/collector/semconv/v1.17.0"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/datadog"
)

// traceToMetricConnector is the schema for connector
type traceToMetricConnector struct {
	metricsConsumer consumer.Metrics // the next component in the pipeline to ingest metrics after connector
	logger          *zap.Logger

	// agent specifies the agent used to ingest traces and output APM Stats.
	// It is implemented by the traceagent structure; replaced in tests.
	agent datadog.Ingester

	// translator specifies the translator used to transform APM Stats Payloads
	// from the agent to OTLP Metrics.
	translator *metrics.Translator

	enableContainerStats bool
	containerTagAttrs    map[string]string
	containerTagCache    *cache.Cache

	// in specifies the channel through which the agent will output Stats Payloads
	// resulting from ingested traces.
	in chan *pb.StatsPayload

	// exit specifies the exit channel, which will be closed upon shutdown.
	exit chan struct{}
}

var _ component.Component = (*traceToMetricConnector)(nil) // testing that the connectorImp properly implements the type Component interface

// function to create a new connector
func newTraceToMetricConnector(set component.TelemetrySettings, cfg component.Config, metricsConsumer consumer.Metrics, metricsClient statsd.ClientInterface, timingReporter timing.Reporter) (*traceToMetricConnector, error) {
	set.Logger.Info("Building datadog connector for traces to metrics")
	in := make(chan *pb.StatsPayload, 100)
	set.MeterProvider = noop.NewMeterProvider() // disable metrics for the connector
	attributesTranslator, err := attributes.NewTranslator(set)
	if err != nil {
		return nil, fmt.Errorf("failed to create attributes translator: %w", err)
	}
	trans, err := metrics.NewTranslator(set, attributesTranslator)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics translator: %w", err)
	}

	ctags := make(map[string]string, len(cfg.(*Config).Traces.ContainerTagAttributes))
	for _, val := range cfg.(*Config).Traces.ContainerTagAttributes {
		ctags[val] = ""
	}
	ddtags := attributes.ContainerTagFromAttributes(ctags)

	ctx := context.Background()
	return &traceToMetricConnector{
		logger:               set.Logger,
		agent:                datadog.NewAgentWithConfig(ctx, getTraceAgentCfg(cfg.(*Config).Traces, attributesTranslator), in, metricsClient, timingReporter),
		translator:           trans,
		in:                   in,
		metricsConsumer:      metricsConsumer,
		enableContainerStats: cfg.(*Config).Traces.EnableContainerStats,
		containerTagAttrs:    ddtags,
		containerTagCache:    cache.New(cache.DefaultExpiration, cache.DefaultExpiration),
		exit:                 make(chan struct{}),
	}, nil
}

func getTraceAgentCfg(cfg TracesConfig, attributesTranslator *attributes.Translator) *traceconfig.AgentConfig {
	acfg := traceconfig.New()
	acfg.OTLPReceiver.AttributesTranslator = attributesTranslator
	acfg.OTLPReceiver.SpanNameRemappings = cfg.SpanNameRemappings
	acfg.OTLPReceiver.SpanNameAsResourceName = cfg.SpanNameAsResourceName
	acfg.Ignore["resource"] = cfg.IgnoreResources
	acfg.ComputeStatsBySpanKind = cfg.ComputeStatsBySpanKind
	acfg.PeerTagsAggregation = cfg.PeerTagsAggregation
	acfg.PeerTags = cfg.PeerTags
	if cfg.EnableContainerStats {
		acfg.Features["enable_cid_stats"] = struct{}{}
	}
	if v := cfg.TraceBuffer; v > 0 {
		acfg.TraceBuffer = v
	}
	return acfg
}

// Start implements the component.Component interface.
func (c *traceToMetricConnector) Start(_ context.Context, _ component.Host) error {
	c.logger.Info("Starting datadogconnector")
	c.agent.Start()
	go c.run()
	return nil
}

// Shutdown implements the component.Component interface.
func (c *traceToMetricConnector) Shutdown(context.Context) error {
	c.logger.Info("Shutting down datadog connector")
	c.logger.Info("Stopping datadog agent")
	// stop the agent and wait for the run loop to exit
	c.agent.Stop()
	c.exit <- struct{}{} // signal exit
	<-c.exit             // wait for close
	return nil
}

// Capabilities implements the consumer interface.
// tells use whether the component(connector) will mutate the data passed into it. if set to true the connector does modify the data
func (c *traceToMetricConnector) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (c *traceToMetricConnector) ConsumeTraces(ctx context.Context, traces ptrace.Traces) error {
	if c.enableContainerStats && len(c.containerTagAttrs) > 0 {
		for i := 0; i < traces.ResourceSpans().Len(); i++ {
			rs := traces.ResourceSpans().At(i)
			attrs := rs.Resource().Attributes()
			containerID, ok := attrs.Get(semconv.AttributeContainerID)
			if !ok {
				continue
			}
			ddContainerTags := attributes.ContainerTagsFromResourceAttributes(attrs)
			for attr := range c.containerTagAttrs {
				if val, ok := ddContainerTags[attr]; ok {
					var cacheVal []string
					if val, ok := c.containerTagCache.Get(containerID.AsString()); ok {
						cacheVal = val.([]string)
					}
					cacheVal = append(cacheVal, val)
					c.containerTagCache.Set(containerID.AsString(), cacheVal, cache.DefaultExpiration)
				}
			}
		}
	}
	c.agent.Ingest(ctx, traces)
	return nil
}

// run awaits incoming stats resulting from the agent's ingestion, converts them
// to metrics and flushes them using the configured metrics exporter.
func (c *traceToMetricConnector) run() {
	defer close(c.exit)
	for {
		select {
		case stats := <-c.in:
			if len(stats.Stats) == 0 {
				continue
			}
			var mx pmetric.Metrics
			var err error
			// Enrich the stats with container tags
			if c.enableContainerStats {
				for _, stat := range stats.Stats {
					if stat.ContainerID != "" {
						if tags, ok := c.containerTagCache.Get(stat.ContainerID); ok {
							tagList := tags.([]string)
							stat.Tags = append(stat.Tags, tagList...)
						}
					}
				}
			}

			c.logger.Debug("Received stats payload", zap.Any("stats", stats))

			mx, err = c.translator.StatsToMetrics(stats)
			if err != nil {
				c.logger.Error("Failed to convert stats to metrics", zap.Error(err))
				continue
			}
			// APM stats as metrics
			ctx := context.TODO()

			// send metrics to the consumer or next component in pipeline
			if err := c.metricsConsumer.ConsumeMetrics(ctx, mx); err != nil {
				c.logger.Error("Failed ConsumeMetrics", zap.Error(err))
				return
			}
		case <-c.exit:
			return
		}
	}
}
