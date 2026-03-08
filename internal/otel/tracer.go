// Package otel provides a lightweight observability layer for House of Cards.
//
// It exposes OTEL-compatible Tracer/Span abstractions and supports three
// export modes: stdout (JSON lines), otlp (stub, future), nop (disabled).
//
// Usage:
//
//	provider := otel.NewProvider("house-of-cards", "stdout")
//	tracer  := provider.Tracer("whip")
//	ctx, span := tracer.Start(ctx, "whip.tick")
//	defer span.End()
//	span.SetAttr("bills.ready", 3)
package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// contextKey is the private key type for storing spans in context.
type contextKey struct{}

// ─────────────────────────────────────────────
// Span
// ─────────────────────────────────────────────

// Span represents a single unit of work in a distributed trace.
type Span struct {
	TraceID   string         `json:"trace_id"`
	SpanID    string         `json:"span_id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Name      string         `json:"name"`
	Service   string         `json:"service"`
	StartTime time.Time      `json:"start_time"`
	EndTime   time.Time      `json:"end_time,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
	Status    string         `json:"status"` // ok | error
	ErrMsg    string         `json:"error,omitempty"`

	exporter Exporter
	ended    bool
	mu       sync.Mutex
}

// SetAttr attaches a key-value attribute to the span.
func (s *Span) SetAttr(key string, val any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Attrs == nil {
		s.Attrs = make(map[string]any)
	}
	s.Attrs[key] = val
}

// RecordError marks the span as errored and records the error message.
func (s *Span) RecordError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = "error"
	s.ErrMsg = err.Error()
}

// End finalises the span and sends it to the exporter.
func (s *Span) End() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.ended = true
	s.EndTime = time.Now()
	if s.Status == "" {
		s.Status = "ok"
	}
	if s.exporter != nil {
		s.exporter.ExportSpan(s)
	}
}

// ─────────────────────────────────────────────
// Tracer
// ─────────────────────────────────────────────

// Tracer creates spans for a named component.
type Tracer struct {
	service  string
	name     string
	exporter Exporter
}

var spanCounter uint64 // monotonic counter for unique span IDs

// Start creates and returns a new child span. The span is stored in the
// returned context so nested calls inherit the trace/parent IDs.
func (t *Tracer) Start(ctx context.Context, name string, attrs ...map[string]any) (context.Context, *Span) {
	atomic.AddUint64(&spanCounter, 1)
	id := fmt.Sprintf("%016x", atomic.LoadUint64(&spanCounter))

	// Derive trace ID and parent ID from any parent span.
	traceID := id
	parentID := ""
	if parent := SpanFromContext(ctx); parent != nil {
		traceID = parent.TraceID
		parentID = parent.SpanID
	}

	combined := make(map[string]any)
	for _, m := range attrs {
		for k, v := range m {
			combined[k] = v
		}
	}

	span := &Span{
		TraceID:   traceID,
		SpanID:    id,
		ParentID:  parentID,
		Name:      name,
		Service:   t.service,
		StartTime: time.Now(),
		Attrs:     combined,
		exporter:  t.exporter,
	}

	return context.WithValue(ctx, contextKey{}, span), span
}

// SpanFromContext retrieves the active span from a context, or nil.
func SpanFromContext(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(contextKey{})
	if v == nil {
		return nil
	}
	s, _ := v.(*Span)
	return s
}

// ─────────────────────────────────────────────
// Provider
// ─────────────────────────────────────────────

// Provider is the root factory for Tracer instances.
type Provider struct {
	serviceName string
	exporter    Exporter
}

// NewProvider creates a Provider for the given service and export mode.
// exporterType: "stdout" | "otlp" | "nop"
func NewProvider(serviceName, exporterType string) *Provider {
	var exp Exporter
	switch exporterType {
	case "stdout":
		exp = NewStdoutExporter(os.Stdout)
	case "otlp":
		exp = NewOTLPExporter("") // stub; endpoint configured separately
	default:
		exp = NopExporter{}
	}
	return &Provider{serviceName: serviceName, exporter: exp}
}

// NewProviderWithWriter creates a Provider that writes span JSON to w.
// Useful for testing.
func NewProviderWithWriter(serviceName string, w io.Writer) *Provider {
	return &Provider{serviceName: serviceName, exporter: NewStdoutExporter(w)}
}

// Tracer returns a Tracer scoped to the named component.
func (p *Provider) Tracer(name string) *Tracer {
	return &Tracer{service: p.serviceName, name: name, exporter: p.exporter}
}

// ─────────────────────────────────────────────
// Global provider (set once at startup)
// ─────────────────────────────────────────────

var (
	globalProvider *Provider
	globalOnce     sync.Once
)

// SetGlobalProvider sets the package-level provider used by GlobalTracer().
// Must be called once at program startup (cmd/root.go).
func SetGlobalProvider(p *Provider) {
	globalOnce.Do(func() {
		globalProvider = p
	})
}

// setGlobalProviderForTest replaces the global provider unconditionally.
// Exported for use by otel_test via InitFromConfigWithWriter.
func setGlobalProviderForTest(p *Provider) {
	globalProvider = p
	// Reset the sync.Once so a subsequent SetGlobalProvider call works.
	globalOnce = sync.Once{}
}

// GlobalTracer returns a Tracer from the global provider.
// If SetGlobalProvider has not been called, returns a nop tracer.
func GlobalTracer(name string) *Tracer {
	if globalProvider == nil {
		return &Tracer{exporter: NopExporter{}}
	}
	return globalProvider.Tracer(name)
}

// ─────────────────────────────────────────────
// Exporter interface
// ─────────────────────────────────────────────

// Exporter receives finished spans for output.
type Exporter interface {
	ExportSpan(span *Span)
}

// ── stdout exporter ──

// StdoutExporter writes spans as JSON lines to a writer.
type StdoutExporter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewStdoutExporter creates a stdout-format exporter.
func NewStdoutExporter(w io.Writer) *StdoutExporter {
	return &StdoutExporter{w: w}
}

// ExportSpan marshals span to JSON and writes to w.
func (e *StdoutExporter) ExportSpan(span *Span) {
	e.mu.Lock()
	defer e.mu.Unlock()
	b, err := json.Marshal(span)
	if err != nil {
		return
	}
	fmt.Fprintf(e.w, "%s\n", b)
}

// ── otlp stub exporter ──

// OTLPExporter is a placeholder for future gRPC/HTTP OTLP export.
// Currently it logs to stderr and does nothing else.
type OTLPExporter struct {
	endpoint string
}

// NewOTLPExporter creates an OTLP exporter stub.
func NewOTLPExporter(endpoint string) *OTLPExporter {
	return &OTLPExporter{endpoint: endpoint}
}

// ExportSpan is a stub — not yet implemented.
func (e *OTLPExporter) ExportSpan(_ *Span) {
	// TODO: implement gRPC/HTTP OTLP export
}

// ── nop exporter ──

// NopExporter discards all spans (observability disabled).
type NopExporter struct{}

// ExportSpan discards the span.
func (NopExporter) ExportSpan(_ *Span) {}
