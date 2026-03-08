package otel

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ─────────────────────────────────────────────
// Metric types
// ─────────────────────────────────────────────

// Counter is a monotonically increasing integer metric.
type Counter struct {
	name    string
	service string
	val     uint64
	exp     MetricExporter
}

// Add increments the counter by delta (must be > 0).
func (c *Counter) Add(delta uint64) {
	atomic.AddUint64(&c.val, delta)
}

// Inc increments the counter by 1.
func (c *Counter) Inc() { c.Add(1) }

// Value returns the current counter value.
func (c *Counter) Value() uint64 { return atomic.LoadUint64(&c.val) }

// Snapshot returns a MetricPoint for export.
func (c *Counter) Snapshot() MetricPoint {
	return MetricPoint{
		Name:    c.name,
		Service: c.service,
		Kind:    "counter",
		Value:   float64(atomic.LoadUint64(&c.val)),
		Time:    time.Now(),
	}
}

// ─────────────────────────────────────────────

// Histogram tracks a distribution of values (count + sum + buckets).
type Histogram struct {
	name    string
	service string
	mu      sync.Mutex
	count   uint64
	sum     float64
	exp     MetricExporter
}

// Record adds an observation.
func (h *Histogram) Record(val float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += val
}

// Snapshot returns a MetricPoint for export.
func (h *Histogram) Snapshot() MetricPoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	avg := 0.0
	if h.count > 0 {
		avg = h.sum / float64(h.count)
	}
	return MetricPoint{
		Name:    h.name,
		Service: h.service,
		Kind:    "histogram",
		Value:   avg,
		Extra: map[string]any{
			"count": h.count,
			"sum":   h.sum,
		},
		Time: time.Now(),
	}
}

// ─────────────────────────────────────────────
// MetricPoint — exportable snapshot
// ─────────────────────────────────────────────

// MetricPoint is a single exported metric observation.
type MetricPoint struct {
	Name    string         `json:"name"`
	Service string         `json:"service"`
	Kind    string         `json:"kind"` // counter | histogram
	Value   float64        `json:"value"`
	Extra   map[string]any `json:"extra,omitempty"`
	Time    time.Time      `json:"time"`
}

// ─────────────────────────────────────────────
// MetricRegistry
// ─────────────────────────────────────────────

// MetricRegistry holds all registered metrics for a provider.
type MetricRegistry struct {
	mu       sync.RWMutex
	counters map[string]*Counter
	hists    map[string]*Histogram
	service  string
	exp      MetricExporter
}

// newMetricRegistry creates an internal registry.
func newMetricRegistry(service string, exp MetricExporter) *MetricRegistry {
	return &MetricRegistry{
		counters: make(map[string]*Counter),
		hists:    make(map[string]*Histogram),
		service:  service,
		exp:      exp,
	}
}

// Counter returns (creating if needed) a named counter.
func (r *MetricRegistry) Counter(name string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{name: name, service: r.service, exp: r.exp}
	r.counters[name] = c
	return c
}

// Histogram returns (creating if needed) a named histogram.
func (r *MetricRegistry) Histogram(name string) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.hists[name]; ok {
		return h
	}
	h := &Histogram{name: name, service: r.service, exp: r.exp}
	r.hists[name] = h
	return h
}

// ExportAll sends all metric snapshots to the exporter.
func (r *MetricRegistry) ExportAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.counters {
		r.exp.ExportMetric(c.Snapshot())
	}
	for _, h := range r.hists {
		r.exp.ExportMetric(h.Snapshot())
	}
}

// ─────────────────────────────────────────────
// MetricExporter interface
// ─────────────────────────────────────────────

// MetricExporter receives metric snapshots for output.
type MetricExporter interface {
	ExportMetric(pt MetricPoint)
}

// StdoutMetricExporter writes MetricPoints as JSON lines to stdout.
type StdoutMetricExporter struct {
	mu sync.Mutex
	e  *StdoutExporter // reuse the same writer
}

// NewStdoutMetricExporter creates a metric exporter that shares the span writer.
func NewStdoutMetricExporter(exp *StdoutExporter) *StdoutMetricExporter {
	return &StdoutMetricExporter{e: exp}
}

// ExportMetric writes the metric as a JSON line.
func (e *StdoutMetricExporter) ExportMetric(pt MetricPoint) {
	e.e.mu.Lock()
	defer e.e.mu.Unlock()
	b, err := json.Marshal(pt)
	if err != nil {
		return
	}
	fmt.Fprintf(e.e.w, "%s\n", b)
}

// NopMetricExporter discards all metrics.
type NopMetricExporter struct{}

// ExportMetric is a no-op.
func (NopMetricExporter) ExportMetric(_ MetricPoint) {}

// ─────────────────────────────────────────────
// Global metric registry (shared with Provider)
// ─────────────────────────────────────────────

var globalMetrics *MetricRegistry

// Metrics returns the global metric registry.
// Returns a nop registry if the global provider has not been initialized.
func Metrics() *MetricRegistry {
	if globalMetrics == nil {
		return newMetricRegistry("nop", NopMetricExporter{})
	}
	return globalMetrics
}

// initGlobalMetrics is called by SetGlobalProvider.
func initGlobalMetrics(service string, exp MetricExporter) {
	globalMetrics = newMetricRegistry(service, exp)
}

// NewRegistryForTest creates a MetricRegistry for unit tests without affecting
// the global state.
func NewRegistryForTest(service string, exp MetricExporter) *MetricRegistry {
	return newMetricRegistry(service, exp)
}
