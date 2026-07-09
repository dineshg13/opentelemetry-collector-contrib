// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDimensionAndAttributeNamesDropOTelPrefix(t *testing.T) {
	// The "dimensions."/"attributes." prefix is an OTel-side classification hint on the
	// incoming key; the wide event column must carry the bare name.
	require.Equal(t, "host.name", dimensionName("dimensions.host.name"))
	require.Equal(t, "cpu", dimensionName("cpu")) // metric datapoint attr, already bare
	require.Equal(t, "region", dimensionName("dimensions.region"))
	require.Equal(t, "userId", attributeName("attributes.userId"))
	require.Equal(t, "productId", attributeName("productId"))

	// service.name (used by observeIdentity) round-trips to a bare key.
	require.Equal(t, "service.name", dimensionName("service.name"))
}

func TestNormalizedWideUnit(t *testing.T) {
	cases := map[string]string{
		"":                "1",
		"1":               "1",
		"s":               "s",
		"{thread}":        "1",
		"{connections}":   "1",
		"connections":     "connections",
		"s{cpu}":          "s",
		"By{transmitted}": "By",
		"  ":              "1",
		"{a}{b}":          "1",
	}
	for in, want := range cases {
		require.Equalf(t, want, normalizedWideUnit(in), "normalizedWideUnit(%q)", in)
	}

	// UCUM annotation-only units are dimensionless, so "{thread}" reconciles with a
	// plain "1" — this is the pair that previously produced a unit conflict.
	require.Equal(t, normalizedWideUnit("1"), normalizedWideUnit("{thread}"))

	// No single string rule can reconcile both observed conflict pairs: making
	// "{connections}" equal a bare "connections" would break the "{thread}"=="1"
	// case. The residual mismatch is handled by the non-fatal drop-and-count path,
	// not by fabricating an equivalence here.
	require.NotEqual(t, normalizedWideUnit("connections"), normalizedWideUnit("{connections}"))
}
