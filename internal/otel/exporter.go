package otel

import (
	"io"
	"os"
)

// ExporterConfig holds the configuration for the observability layer.
type ExporterConfig struct {
	// Type selects the export backend: "stdout" | "otlp" | "nop"
	Type string
	// OTLPEndpoint is the gRPC endpoint for OTLP export (e.g. "localhost:4317").
	OTLPEndpoint string
	// ServiceName is the logical name of this service in traces/metrics.
	ServiceName string
	// Writer overrides the output writer for "stdout" mode (default: os.Stdout).
	Writer io.Writer
}

// DefaultExporterConfig returns sensible defaults (stdout, service=house-of-cards).
func DefaultExporterConfig() ExporterConfig {
	return ExporterConfig{
		Type:        "nop",
		ServiceName: "house-of-cards",
		Writer:      os.Stdout,
	}
}

// InitFromConfig initialises the global provider and metric registry from cfg.
// Call this once at program startup. Subsequent calls are ignored.
func InitFromConfig(cfg ExporterConfig) {
	initProviders(cfg, false)
}

// InitFromConfigWithWriter is like InitFromConfig but always replaces the
// global provider (for testing). The writer overrides the stdout target.
func InitFromConfigWithWriter(cfg ExporterConfig, w io.Writer) {
	cfg.Writer = w
	initProviders(cfg, true)
}

func initProviders(cfg ExporterConfig, forTest bool) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "house-of-cards"
	}
	if cfg.Writer == nil {
		cfg.Writer = os.Stdout
	}

	var (
		spanExp   Exporter
		metricExp MetricExporter
	)

	switch cfg.Type {
	case "stdout":
		se := NewStdoutExporter(cfg.Writer)
		spanExp = se
		metricExp = NewStdoutMetricExporter(se)
	case "otlp":
		spanExp = NewOTLPExporter(cfg.OTLPEndpoint)
		metricExp = NopMetricExporter{}
	default:
		spanExp = NopExporter{}
		metricExp = NopMetricExporter{}
	}

	p := &Provider{serviceName: cfg.ServiceName, exporter: spanExp}
	if forTest {
		setGlobalProviderForTest(p)
	} else {
		SetGlobalProvider(p)
	}
	initGlobalMetrics(cfg.ServiceName, metricExp)
}
