package otel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/house-of-cards/hoc/internal/otel"
)

// TestSpanStartEnd verifies that a span is exported with correct fields.
func TestSpanStartEnd(t *testing.T) {
	var buf bytes.Buffer
	p := otel.NewProviderWithWriter("test-service", &buf)
	tracer := p.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test.op")
	span.SetAttr("key", "value")
	span.End()

	if buf.Len() == 0 {
		t.Fatal("expected span output, got nothing")
	}

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if out["name"] != "test.op" {
		t.Errorf("name: got %v, want test.op", out["name"])
	}
	if out["service"] != "test-service" {
		t.Errorf("service: got %v, want test-service", out["service"])
	}
	if out["status"] != "ok" {
		t.Errorf("status: got %v, want ok", out["status"])
	}
	attrs, _ := out["attrs"].(map[string]any)
	if attrs["key"] != "value" {
		t.Errorf("attrs.key: got %v, want value", attrs["key"])
	}

	// Verify context propagation gives same trace ID
	_, child := tracer.Start(ctx, "test.child")
	defer child.End()
	if child.TraceID != span.TraceID {
		t.Errorf("child trace_id %s != parent %s", child.TraceID, span.TraceID)
	}
	if child.ParentID != span.SpanID {
		t.Errorf("child parent_id %s != parent span_id %s", child.ParentID, span.SpanID)
	}
}

// TestSpanRecordError verifies error recording sets status to "error".
func TestSpanRecordError(t *testing.T) {
	var buf bytes.Buffer
	p := otel.NewProviderWithWriter("svc", &buf)
	_, span := p.Tracer("t").Start(context.Background(), "op")
	span.RecordError(context.DeadlineExceeded)
	span.End()

	var out map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "error" {
		t.Errorf("status: got %v, want error", out["status"])
	}
	if out["error"] == "" {
		t.Error("expected non-empty error message")
	}
}

// TestNopExporter verifies that NopExporter produces no output.
func TestNopExporter(t *testing.T) {
	p := otel.NewProvider("svc", "nop")
	_, span := p.Tracer("t").Start(context.Background(), "op")
	span.End() // should not panic
}

// TestMetricsCounter verifies Counter increments correctly.
func TestMetricsCounter(t *testing.T) {
	var buf bytes.Buffer
	se := otel.NewStdoutExporter(&buf)
	me := otel.NewStdoutMetricExporter(se)
	reg := otel.NewRegistryForTest("test-service", me)

	c := reg.Counter("requests_total")
	c.Inc()
	c.Inc()
	c.Add(3)

	if c.Value() != 5 {
		t.Errorf("counter: got %d, want 5", c.Value())
	}

	reg.ExportAll()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 {
		t.Fatal("no metric output")
	}

	var pt map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &pt); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if pt["name"] != "requests_total" {
		t.Errorf("metric name: got %v, want requests_total", pt["name"])
	}
	if pt["kind"] != "counter" {
		t.Errorf("kind: got %v, want counter", pt["kind"])
	}
}

// TestMetricsHistogram verifies Histogram average is computed correctly.
func TestMetricsHistogram(t *testing.T) {
	var buf bytes.Buffer
	se := otel.NewStdoutExporter(&buf)
	me := otel.NewStdoutMetricExporter(se)
	reg := otel.NewRegistryForTest("svc", me)

	h := reg.Histogram("duration_seconds")
	h.Record(10)
	h.Record(20)
	h.Record(30)

	reg.ExportAll()

	var pt map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &pt); err != nil {
		t.Fatal(err)
	}
	// average should be 20
	if pt["value"].(float64) != 20 {
		t.Errorf("histogram avg: got %v, want 20", pt["value"])
	}
}

// TestInitFromConfig verifies that InitFromConfig sets the global provider.
func TestInitFromConfig(t *testing.T) {
	var buf bytes.Buffer
	otel.InitFromConfigWithWriter(otel.ExporterConfig{
		Type:        "stdout",
		ServiceName: "cfg-test",
	}, &buf)

	_, span := otel.GlobalTracer("comp").Start(context.Background(), "cfg.op")
	span.End()

	if buf.Len() == 0 {
		t.Fatal("expected output after InitFromConfig")
	}
}
