// Package whip implements the Whip daemon — the system's driving force.
//
// The Whip runs a background tick loop every 10 seconds performing three duties:
//  1. Three-Line Whip  — heartbeat / liveness check on all working Ministers.
//  2. Order Paper       — DAG engine: find ready Bills and auto-assign to idle Ministers.
//  3. Gazette Dispatch  — mark unread Gazettes as delivered (logged).
//
// Every 60 seconds it also refreshes the Speaker context.md and logs a Hansard snapshot.
package whip

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/house-of-cards/hoc/internal/otel"
	"github.com/house-of-cards/hoc/internal/store"
)

const (
	tickInterval    = 10 * time.Second
	gracePeriod     = 30 * time.Second // working → stuck 宽限期
	stuckThreshold  = 5 * time.Minute  // stuck → byElection 阈值
	hansardInterval = 60 * time.Second
)

// Whip is the daemon that drives session progress.
type Whip struct {
	db     *store.DB
	hocDir string
	tracer *otel.Tracer
}

// whipMetrics is a convenience bundle of OTEL counters/histograms used by Whip.
var whipMetrics struct {
	byElectionTotal *otel.Counter
	conflictsTotal  *otel.Counter
	billDuration    *otel.Histogram
	ministersActive *otel.Counter
}

// New returns a new Whip bound to the given database and hocDir.
// Logging is handled via the global slog logger configured in cmd/root.go.
func New(db *store.DB, hocDir string) *Whip {
	m := otel.Metrics()
	whipMetrics.byElectionTotal = m.Counter("hoc_by_election_total")
	whipMetrics.conflictsTotal = m.Counter("hoc_conflicts_total")
	whipMetrics.billDuration = m.Histogram("hoc_bills_duration_seconds")
	whipMetrics.ministersActive = m.Counter("hoc_ministers_active_total")

	return &Whip{
		db:     db,
		hocDir: hocDir,
		tracer: otel.GlobalTracer("whip"),
	}
}

// Run starts the Whip main loop. It blocks until ctx is cancelled.
func (w *Whip) Run(ctx context.Context) {
	slog.Info("党鞭就位 (Whip is seated). 开始监控...")

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	hansardTicker := time.NewTicker(hansardInterval)
	defer hansardTicker.Stop()

	// Run an immediate tick on startup.
	w.tick()

	for {
		select {
		case <-ctx.Done():
			slog.Info("党鞭休会 (Whip dismissed).")
			return
		case <-ticker.C:
			w.tick()
		case <-hansardTicker.C:
			w.hansardUpdate()
		}
	}
}

// tick performs all 10-second duties.
func (w *Whip) tick() {
	_, span := w.tracer.Start(context.Background(), "whip.tick")
	defer span.End()

	w.threeLineWhip()
	w.orderPaper()
	w.pollDoneFiles()
	w.pollIdleMinisterReassign() // Phase 3B: Hook queue auto-reassign
	w.committeeAutomation()
	w.gazetteDispatch()
	w.autoscale()
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func gazetteID() string {
	return fmt.Sprintf("gazette-%x", time.Now().UnixNano())
}
