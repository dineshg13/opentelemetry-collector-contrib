// Copyright  The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pmetric"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/telemetryquerylanguage/contexts/tqlmetrics"
)

func Test_convertGaugeToSum(t *testing.T) {
	gaugeInput := pmetric.NewMetric()
	gaugeInput.SetDataType(pmetric.MetricDataTypeGauge)

	dp1 := gaugeInput.Gauge().DataPoints().AppendEmpty()
	dp1.SetIntVal(10)

	dp2 := gaugeInput.Gauge().DataPoints().AppendEmpty()
	dp2.SetDoubleVal(14.5)

	sumInput := pmetric.NewMetric()
	sumInput.SetDataType(pmetric.MetricDataTypeSum)

	histogramInput := pmetric.NewMetric()
	histogramInput.SetDataType(pmetric.MetricDataTypeHistogram)

	expoHistogramInput := pmetric.NewMetric()
	expoHistogramInput.SetDataType(pmetric.MetricDataTypeExponentialHistogram)

	summaryInput := pmetric.NewMetric()
	summaryInput.SetDataType(pmetric.MetricDataTypeSummary)

	tests := []struct {
		name          string
		stringAggTemp string
		monotonic     bool
		input         pmetric.Metric
		want          func(pmetric.Metric)
	}{
		{
			name:          "convert gauge to cumulative sum",
			stringAggTemp: "cumulative",
			monotonic:     false,
			input:         gaugeInput,
			want: func(metric pmetric.Metric) {
				gaugeInput.CopyTo(metric)

				dps := gaugeInput.Gauge().DataPoints()

				metric.SetDataType(pmetric.MetricDataTypeSum)
				metric.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityCumulative)
				metric.Sum().SetIsMonotonic(false)

				dps.CopyTo(metric.Sum().DataPoints())
			},
		},
		{
			name:          "convert gauge to delta sum",
			stringAggTemp: "delta",
			monotonic:     true,
			input:         gaugeInput,
			want: func(metric pmetric.Metric) {
				gaugeInput.CopyTo(metric)

				dps := gaugeInput.Gauge().DataPoints()

				metric.SetDataType(pmetric.MetricDataTypeSum)
				metric.Sum().SetAggregationTemporality(pmetric.MetricAggregationTemporalityDelta)
				metric.Sum().SetIsMonotonic(true)

				dps.CopyTo(metric.Sum().DataPoints())
			},
		},
		{
			name:          "noop for sum",
			stringAggTemp: "delta",
			monotonic:     true,
			input:         sumInput,
			want: func(metric pmetric.Metric) {
				sumInput.CopyTo(metric)
			},
		},
		{
			name:          "noop for histogram",
			stringAggTemp: "delta",
			monotonic:     true,
			input:         histogramInput,
			want: func(metric pmetric.Metric) {
				histogramInput.CopyTo(metric)
			},
		},
		{
			name:          "noop for exponential histogram",
			stringAggTemp: "delta",
			monotonic:     true,
			input:         expoHistogramInput,
			want: func(metric pmetric.Metric) {
				expoHistogramInput.CopyTo(metric)
			},
		},
		{
			name:          "noop for summary",
			stringAggTemp: "delta",
			monotonic:     true,
			input:         summaryInput,
			want: func(metric pmetric.Metric) {
				summaryInput.CopyTo(metric)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric := pmetric.NewMetric()
			tt.input.CopyTo(metric)

			ctx := tqlmetrics.MetricTransformContext{
				Metric: metric,
			}

			exprFunc, _ := convertGaugeToSum(tt.stringAggTemp, tt.monotonic)
			exprFunc(ctx)

			expected := pmetric.NewMetric()
			tt.want(expected)

			assert.Equal(t, expected, metric)
		})
	}
}

func Test_convertGaugeToSum_validation(t *testing.T) {
	tests := []struct {
		name          string
		stringAggTemp string
	}{
		{
			name:          "invalid aggregation temporality",
			stringAggTemp: "not a real aggregation temporality",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convertGaugeToSum(tt.stringAggTemp, true)
			assert.Error(t, err, "unknown aggregation temporality: not a real aggregation temporality")
		})
	}
}
