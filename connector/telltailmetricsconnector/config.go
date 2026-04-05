// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telltailmetricsconnector // import "github.com/open-telemetry/opentelemetry-collector-contrib/connector/telltailmetricsconnector"

import (
	"fmt"

	"go.opentelemetry.io/collector/confmap/xconfmap"
)

// Config defines the configuration for the telltailmetrics connector.
type Config struct {
	// MoveToTraces removes these attribute keys from metric data points and
	// promotes them onto the parent span. Data points that become identical
	// after removal are aggregated (summed for counters, merged for histograms).
	MoveToTraces []string `mapstructure:"move_to_traces"`

	// MoveToMetrics copies these span-level attributes onto every metric data
	// point as additional dimensions. Event attributes win on collision.
	MoveToMetrics []string `mapstructure:"move_to_metrics"`

	// prevent unkeyed literal initialization
	_ struct{}
}

var _ xconfmap.Validator = (*Config)(nil)

// Validate checks that the configuration is valid.
func (c Config) Validate() error {
	// Check for duplicates within MoveToTraces.
	seen := make(map[string]struct{}, len(c.MoveToTraces))
	for _, k := range c.MoveToTraces {
		if _, dup := seen[k]; dup {
			return fmt.Errorf("duplicate key %q in move_to_traces", k)
		}
		seen[k] = struct{}{}
	}

	// Check for duplicates within MoveToMetrics.
	seenM := make(map[string]struct{}, len(c.MoveToMetrics))
	for _, k := range c.MoveToMetrics {
		if _, dup := seenM[k]; dup {
			return fmt.Errorf("duplicate key %q in move_to_metrics", k)
		}
		seenM[k] = struct{}{}
	}

	// Check for overlap between the two lists.
	for _, k := range c.MoveToMetrics {
		if _, overlap := seen[k]; overlap {
			return fmt.Errorf("key %q appears in both move_to_traces and move_to_metrics", k)
		}
	}

	return nil
}
