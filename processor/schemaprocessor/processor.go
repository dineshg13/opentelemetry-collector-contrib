// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package schemaprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/schemaprocessor"

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/schemaprocessor/internal/translation"
)

type schemaprocessor struct {
	telemetry component.TelemetrySettings
	config    *Config

	log *zap.Logger

	manager translation.Manager
}

func newSchemaProcessor(_ context.Context, conf component.Config, set processor.Settings) (*schemaprocessor, error) {
	cfg, ok := conf.(*Config)
	if !ok {
		return nil, errors.New("invalid configuration provided")
	}

	m, err := translation.NewManager(
		cfg.Targets,
		set.Logger.Named("schema-manager"),
	)
	if err != nil {
		return nil, err
	}
	return &schemaprocessor{
		config:    cfg,
		telemetry: set.TelemetrySettings,
		log:       set.Logger,
		manager:   m,
	}, nil
}

func (t schemaprocessor) processLogs(ctx context.Context, ld plog.Logs) (plog.Logs, error) {
	if t.manager == nil {
		return ld, nil
	}
	for rl := 0; rl < ld.ResourceLogs().Len(); rl++ {
		rLog := ld.ResourceLogs().At(rl)
		resourceSchemaURL := rLog.SchemaUrl()
		if resourceSchemaURL != "" {
			tr, err := t.manager.
				RequestTranslation(ctx, resourceSchemaURL)
			if err != nil {
				t.log.Error("failed to request translation", zap.Error(err))
				return ld, err
			}

			fmt.Printf("resourceSchemaURL: %s\n", resourceSchemaURL)
			err = tr.ApplyAllResourceChanges(rLog, resourceSchemaURL)
			if err != nil {
				t.log.Error("failed to apply resource changes", zap.Error(err))
				return ld, err
			}
		}
		for sl := 0; sl < rLog.ScopeLogs().Len(); sl++ {
			log := rLog.ScopeLogs().At(sl)
			logSchemaURL := log.SchemaUrl()
			if logSchemaURL == "" {
				logSchemaURL = resourceSchemaURL
			}

			fmt.Printf("logSchemaURL: %s\n", logSchemaURL)
			tr, err := t.manager.
				RequestTranslation(ctx, logSchemaURL)
			if err != nil {
				t.log.Error("failed to request translation", zap.Error(err))
				continue
			}
			err = tr.ApplyScopeLogChanges(log, logSchemaURL)
			if err != nil {
				t.log.Error("failed to apply scope log changes", zap.Error(err))
				continue
			}
		}
	}
	return ld, nil
}

func (t schemaprocessor) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	if t.manager == nil {
		return md, nil
	}
	for rm := 0; rm < md.ResourceMetrics().Len(); rm++ {
		rMetric := md.ResourceMetrics().At(rm)
		resourceSchemaURL := rMetric.SchemaUrl()
		if resourceSchemaURL != "" {
			tr, err := t.manager.
				RequestTranslation(ctx, resourceSchemaURL)
			if err != nil {
				t.log.Error("failed to request translation", zap.Error(err))
				return md, err
			}
			err = tr.ApplyAllResourceChanges(rMetric, resourceSchemaURL)
			if err != nil {
				t.log.Error("failed to apply resource changes", zap.Error(err))
				return md, nil
			}
		}
		for sm := 0; sm < rMetric.ScopeMetrics().Len(); sm++ {
			metric := rMetric.ScopeMetrics().At(sm)
			metricSchemaURL := metric.SchemaUrl()
			if metricSchemaURL == "" {
				metricSchemaURL = resourceSchemaURL
			}
			tr, err := t.manager.
				RequestTranslation(ctx, metricSchemaURL)
			if err != nil {
				t.log.Error("failed to request translation", zap.Error(err))
				continue
			}
			err = tr.ApplyScopeMetricChanges(metric, metricSchemaURL)
			if err != nil {
				t.log.Error("failed to apply scope metric changes", zap.Error(err))
			}
		}
	}
	return md, nil
}

func (t *schemaprocessor) processTraces(ctx context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	t.log.Debug("processing traces")
	if t.manager == nil {
		return td, nil
	}
	for rt := 0; rt < td.ResourceSpans().Len(); rt++ {
		rTrace := td.ResourceSpans().At(rt)
		// todo(ankit) do i need to check if this is empty?
		resourceSchemaURL := rTrace.SchemaUrl()
		if resourceSchemaURL != "" {
			t.log.Debug("requesting translation for resourceSchemaURL", zap.String("resourceSchemaURL", resourceSchemaURL))
			tr, err := t.manager.
				RequestTranslation(ctx, resourceSchemaURL)
			if err != nil {
				t.log.Error("failed to request translation", zap.Error(err))
				return td, err
			}
			err = tr.ApplyAllResourceChanges(rTrace, resourceSchemaURL)
			if err != nil {
				t.log.Error("failed to apply resource changes", zap.Error(err))
				return td, err
			}
		}
		for ss := 0; ss < rTrace.ScopeSpans().Len(); ss++ {
			span := rTrace.ScopeSpans().At(ss)
			spanSchemaURL := span.SchemaUrl()
			if spanSchemaURL == "" {
				spanSchemaURL = resourceSchemaURL
			}
			tr, err := t.manager.
				RequestTranslation(ctx, spanSchemaURL)
			if err != nil {
				t.log.Error("failed to request translation", zap.Error(err))
				continue
			}
			err = tr.ApplyScopeSpanChanges(span, spanSchemaURL)
			if err != nil {
				t.log.Error("failed to apply scope span changes", zap.Error(err))
			}
		}
	}
	return td, nil
}

// start will load the remote file definition if it isn't already cached
// and resolve the schema translation file
func (t *schemaprocessor) start(ctx context.Context, host component.Host) error {
	// Check for additional extensions that can be checked first before
	// perfomring the http request
	// TODO(MovieStoreGuy): Check for storage extensions

	client, err := t.config.ToClient(ctx, host, t.telemetry)
	if err != nil {
		return err
	}
	t.manager.AddProvider(translation.NewHTTPProvider(client))

	go func(ctx context.Context) {
		for _, schemaURL := range t.config.Prefetch {
			t.log.Info("prefetching schema", zap.String("url", schemaURL))
			_, _ = t.manager.RequestTranslation(ctx, schemaURL)
		}
	}(ctx)

	return nil
}
