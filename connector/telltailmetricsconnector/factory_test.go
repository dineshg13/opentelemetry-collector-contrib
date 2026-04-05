// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telltailmetricsconnector

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/connector/connectortest"
	"go.opentelemetry.io/collector/consumer/consumertest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/connector/telltailmetricsconnector/internal/metadata"
)

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	require.Equal(t, metadata.Type, factory.Type())

	cfg := factory.CreateDefaultConfig()
	assert.NotNil(t, cfg)
}

func TestCreateTracesToMetrics(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	c, err := factory.CreateTracesToMetrics(t.Context(), connectortest.NewNopSettings(metadata.Type), cfg, consumertest.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, c)

	_, ok := c.(*metricsConnector)
	assert.True(t, ok)
}

func TestCreateTracesToTraces(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	c, err := factory.CreateTracesToTraces(t.Context(), connectortest.NewNopSettings(metadata.Type), cfg, consumertest.NewNop())
	require.NoError(t, err)
	assert.NotNil(t, c)

	_, ok := c.(*tracesConnector)
	assert.True(t, ok)
}

func TestConfigThreadedToMetricsConnector(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.MoveToTraces = []string{"color", "shape"}
	cfg.MoveToMetrics = []string{"http.method"}

	c, err := factory.CreateTracesToMetrics(t.Context(), connectortest.NewNopSettings(metadata.Type), cfg, consumertest.NewNop())
	require.NoError(t, err)

	mc := c.(*metricsConnector)
	assert.Contains(t, mc.excludeFromMetrics, "color")
	assert.Contains(t, mc.excludeFromMetrics, "shape")
	assert.Equal(t, []string{"http.method"}, mc.includeSpanAttrs)
}

func TestConfigThreadedToTracesConnector(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.MoveToTraces = []string{"color", "shape"}

	c, err := factory.CreateTracesToTraces(t.Context(), connectortest.NewNopSettings(metadata.Type), cfg, consumertest.NewNop())
	require.NoError(t, err)

	tc := c.(*tracesConnector)
	assert.Equal(t, []string{"color", "shape"}, tc.promoteToSpan)
}
