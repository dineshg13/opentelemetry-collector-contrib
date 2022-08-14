package simpleexporter

import (
	"context"
	"fmt"

	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const (
	// The value of "type" key in configuration.
	typeStr = "simpleexporter"

	stability = component.StabilityLevelInDevelopment
)

// NewFactory creates a factory for Logging exporter
func NewFactory() component.ExporterFactory {
	return component.NewExporterFactory(
		typeStr,
		createDefaultConfig,
		component.WithTracesExporter(createTracesExporter, stability),
		component.WithMetricsExporter(createMetricsExporter, stability),
		component.WithLogsExporter(createLogsExporter, stability),
	)
}

// Config defines configuration for logging exporter.
type Config struct {
	config.ExporterSettings `mapstructure:",squash"` // squash ensures fields are correctly decoded in embedded struct

	// LogLevel defines log level of the logging exporter; options are debug, info, warn, error.
	LogLevel zapcore.Level `mapstructure:"loglevel"`
}

var _ config.Exporter = (*Config)(nil)

// Validate checks if the exporter configuration is valid
func (cfg *Config) Validate() error {
	return nil
}

func createDefaultConfig() config.Exporter {
	return &Config{
		ExporterSettings: config.NewExporterSettings(config.NewComponentID(typeStr)),
		LogLevel:         zapcore.InfoLevel,
	}
}

type loggingexporter struct {
	LogLevel         zapcore.Level
	tracesMarshaler  ptrace.Marshaler
	logsMarshaler    plog.Marshaler
	metricsMarshaler pmetric.Marshaler
}

func newLoggingExporter(cfg *Config, set component.ExporterCreateSettings) *loggingexporter {
	return &loggingexporter{
		LogLevel:         cfg.LogLevel,
		tracesMarshaler:  ptrace.NewJSONMarshaler(),
		logsMarshaler:    plog.NewJSONMarshaler(),
		metricsMarshaler: pmetric.NewJSONMarshaler(),
	}
}

func (l *loggingexporter) pushTraces(ctx context.Context, ld ptrace.Traces) error {

	j, err := l.tracesMarshaler.MarshalTraces(ld)
	if err != nil {
		return err
	}
	fmt.Printf("## traces %v\n", string(j))
	return nil
}

func (l *loggingexporter) pushMetrics(ctx context.Context, ld pmetric.Metrics) error {
	m, err := l.metricsMarshaler.MarshalMetrics(ld)
	if err != nil {
		return err
	}
	fmt.Printf("## metrics %v\n", string(m))
	return nil
}
func (l *loggingexporter) pushLogs(ctx context.Context, ld plog.Logs) error {
	m, err := l.logsMarshaler.MarshalLogs(ld)
	if err != nil {
		return err
	}
	fmt.Printf("## logs %v\n", string(m))
	return nil
}
func createTracesExporter(_ context.Context, set component.ExporterCreateSettings, config config.Exporter) (component.TracesExporter, error) {
	cfg := config.(*Config)
	l := newLoggingExporter(cfg, set)
	return exporterhelper.NewTracesExporter(cfg, set, l.pushTraces)
}

func createMetricsExporter(_ context.Context, set component.ExporterCreateSettings, config config.Exporter) (component.MetricsExporter, error) {
	cfg := config.(*Config)
	l := newLoggingExporter(cfg, set)
	return exporterhelper.NewMetricsExporter(cfg, set, l.pushMetrics)
}

func createLogsExporter(_ context.Context, set component.ExporterCreateSettings, config config.Exporter) (component.LogsExporter, error) {
	cfg := config.(*Config)
	l := newLoggingExporter(cfg, set)
	return exporterhelper.NewLogsExporter(cfg, set, l.pushLogs)

}
