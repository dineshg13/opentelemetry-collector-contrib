// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telltailmetricsconnector // import "github.com/open-telemetry/opentelemetry-collector-contrib/connector/telltailmetricsconnector"

import (
	"context"

	"github.com/lightstep/go-expohisto/structure"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/pdatautil"
)

const (
	eventNameMetric    = "metric"
	attrMetricName     = "metric.name"
	attrMetricType     = "metric.type"
	attrMetricValue    = "metric.value"
	metricTypeCounter  = "counter"
	metricTypeHisto    = "histogram"
	expoHistoMaxSize   = 160
)

// metricKey groups data points under the same Metric object.
type metricKey struct {
	resourceIdx int
	name        string
	typ         string
}

type metricsConnector struct {
	component.StartFunc
	component.ShutdownFunc
	logger             *zap.Logger
	metricsConsumer    consumer.Metrics
	excludeFromMetrics map[string]struct{}
	includeSpanAttrs   []string
}

func (*metricsConnector) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (c *metricsConnector) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	md := c.extractMetrics(td)
	if md.DataPointCount() == 0 {
		return nil
	}
	return c.metricsConsumer.ConsumeMetrics(ctx, md)
}

// accumKey identifies the metric (resource + name + type).
type accumKey struct {
	rmIdx int
	name  string
	typ   string
}

// counterAccum accumulates counter data points with identical dimensions.
type counterAccum struct {
	dims  pcommon.Map
	value float64
	ts    pcommon.Timestamp
}

// histAccum accumulates histogram data points with identical dimensions.
type histAccum struct {
	dims pcommon.Map
	hist *structure.Float64
	ts   pcommon.Timestamp
}

func (c *metricsConnector) extractMetrics(td ptrace.Traces) pmetric.Metrics {
	// Phase 1: Accumulate — collect all metric events, aggregate by (accumKey, dimHash).
	type counterMap = map[[16]byte]*counterAccum
	type histMap = map[[16]byte]*histAccum

	counterAccums := make(map[accumKey]counterMap)
	histAccums := make(map[accumKey]histMap)

	// Track which resources we've seen so we can copy them in Phase 2.
	resourceSeen := make(map[int]pcommon.Resource)

	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)

		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				events := span.Events()
				for l := 0; l < events.Len(); l++ {
					event := events.At(l)
					if event.Name() != eventNameMetric {
						continue
					}

					attrs := event.Attributes()
					nameVal, nameOk := attrs.Get(attrMetricName)
					typeVal, typeOk := attrs.Get(attrMetricType)
					valueVal, valueOk := attrs.Get(attrMetricValue)

					if !nameOk || !typeOk || !valueOk {
						c.logger.Warn("skipping metric event with missing required attributes",
							zap.Bool("has_name", nameOk),
							zap.Bool("has_type", typeOk),
							zap.Bool("has_value", valueOk),
						)
						continue
					}

					metricName := nameVal.Str()
					metricType := typeVal.Str()
					metricValue := valueVal.Double()
					timestamp := event.Timestamp()

					if metricType != metricTypeCounter && metricType != metricTypeHisto {
						c.logger.Warn("skipping metric event with unknown type",
							zap.String("metric.name", metricName),
							zap.String("metric.type", metricType),
						)
						continue
					}

					// Build dimension attributes.
					dims := pcommon.NewMap()
					attrs.Range(func(ak string, av pcommon.Value) bool {
						if ak == attrMetricName || ak == attrMetricType || ak == attrMetricValue {
							return true // skip reserved
						}
						if _, excluded := c.excludeFromMetrics[ak]; excluded {
							return true // skip: moved to traces
						}
						av.CopyTo(dims.PutEmpty(ak))
						return true
					})

					// Include span attributes as dimensions (event attr wins).
					for _, sak := range c.includeSpanAttrs {
						if _, already := dims.Get(sak); already {
							continue // event attribute wins
						}
						if sv, ok := span.Attributes().Get(sak); ok {
							sv.CopyTo(dims.PutEmpty(sak))
						}
					}

					// Remember resource.
					if _, ok := resourceSeen[i]; !ok {
						resourceSeen[i] = rs.Resource()
					}

					key := accumKey{rmIdx: i, name: metricName, typ: metricType}
					dimHash := pdatautil.MapHash(dims)

					switch metricType {
					case metricTypeCounter:
						m, ok := counterAccums[key]
						if !ok {
							m = make(counterMap)
							counterAccums[key] = m
						}
						if acc, ok := m[dimHash]; ok {
							acc.value += metricValue
							if timestamp > acc.ts {
								acc.ts = timestamp
							}
						} else {
							d := pcommon.NewMap()
							dims.CopyTo(d)
							m[dimHash] = &counterAccum{dims: d, value: metricValue, ts: timestamp}
						}

					case metricTypeHisto:
						m, ok := histAccums[key]
						if !ok {
							m = make(histMap)
							histAccums[key] = m
						}
						if acc, ok := m[dimHash]; ok {
							acc.hist.Update(metricValue)
							if timestamp > acc.ts {
								acc.ts = timestamp
							}
						} else {
							d := pcommon.NewMap()
							dims.CopyTo(d)
							h := structure.NewFloat64(structure.NewConfig(structure.WithMaxSize(expoHistoMaxSize)))
							h.Update(metricValue)
							m[dimHash] = &histAccum{dims: d, hist: h, ts: timestamp}
						}
					}
				}
			}
		}
	}

	// Phase 2: Flush — build pmetric.Metrics from accumulators.
	md := pmetric.NewMetrics()

	// Track ResourceMetrics per resource index.
	rmByIdx := make(map[int]pmetric.ResourceMetrics)

	getOrCreateRM := func(idx int) pmetric.ResourceMetrics {
		if rm, ok := rmByIdx[idx]; ok {
			return rm
		}
		rm := md.ResourceMetrics().AppendEmpty()
		resourceSeen[idx].CopyTo(rm.Resource())
		rmByIdx[idx] = rm
		return rm
	}

	// Flush counters.
	for key, dimMap := range counterAccums {
		rm := getOrCreateRM(key.rmIdx)
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName("telltailmetricsconnector")
		m := sm.Metrics().AppendEmpty()
		m.SetName(key.name)
		sum := m.SetEmptySum()
		sum.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)
		sum.SetIsMonotonic(true)

		for _, acc := range dimMap {
			dp := sum.DataPoints().AppendEmpty()
			dp.SetStartTimestamp(acc.ts)
			dp.SetTimestamp(acc.ts)
			dp.SetDoubleValue(acc.value)
			acc.dims.CopyTo(dp.Attributes())
		}
	}

	// Flush histograms.
	for key, dimMap := range histAccums {
		rm := getOrCreateRM(key.rmIdx)
		sm := rm.ScopeMetrics().AppendEmpty()
		sm.Scope().SetName("telltailmetricsconnector")
		m := sm.Metrics().AppendEmpty()
		m.SetName(key.name)
		eh := m.SetEmptyExponentialHistogram()
		eh.SetAggregationTemporality(pmetric.AggregationTemporalityDelta)

		for _, acc := range dimMap {
			dp := eh.DataPoints().AppendEmpty()
			dp.SetStartTimestamp(acc.ts)
			dp.SetTimestamp(acc.ts)
			acc.dims.CopyTo(dp.Attributes())
			fillExpoHistoDP(dp, acc.hist)
		}
	}

	return md
}

// fillExpoHistoDP populates an ExponentialHistogramDataPoint from a pre-accumulated histogram.
func fillExpoHistoDP(dp pmetric.ExponentialHistogramDataPoint, hist *structure.Float64) {
	dp.SetCount(hist.Count())
	dp.SetSum(hist.Sum())
	dp.SetScale(hist.Scale())
	dp.SetZeroCount(hist.ZeroCount())
	if hist.Count() > 0 {
		dp.SetMin(hist.Min())
		dp.SetMax(hist.Max())
	}

	copyBucketRange(hist.Positive(), dp.Positive())
	copyBucketRange(hist.Negative(), dp.Negative())
}

// copyBucketRange copies bucket data from the go-expohisto structure to the pmetric representation.
func copyBucketRange(src *structure.Buckets, dest pmetric.ExponentialHistogramDataPointBuckets) {
	dest.SetOffset(src.Offset())
	dest.BucketCounts().EnsureCapacity(int(src.Len()))
	for i := uint32(0); i < src.Len(); i++ {
		dest.BucketCounts().Append(src.At(i))
	}
}
