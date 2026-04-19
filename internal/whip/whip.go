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
	"crypto/rand"
	"fmt"
	"log/slog"
	"time"

	"github.com/house-of-cards/hoc/internal/config"
	"github.com/house-of-cards/hoc/internal/otel"
	"github.com/house-of-cards/hoc/internal/store"
)

const (
	tickInterval    = 10 * time.Second
	tickTimeout     = 8 * time.Second  // Phase 2: tick 超时保护
	gracePeriod     = 30 * time.Second // working → stuck 宽限期
	stuckThreshold  = 5 * time.Minute  // stuck → byElection 阈值
	hansardInterval = 60 * time.Second
)

// Whip is the daemon that drives session progress.
type Whip struct {
	db     *store.DB
	hocDir string
	tracer *otel.Tracer
	cfg    *config.Config

	// lastContextAlert records the last time we alerted a given minister about
	// context-window exhaustion, so tick() does not re-send the same warning on
	// every 10-second cycle. Key is minister ID.
	lastContextAlert map[string]time.Time
}

// whipMetrics is a convenience bundle of OTEL counters/histograms used by Whip.
var whipMetrics struct {
	byElectionTotal *otel.Counter
	conflictsTotal  *otel.Counter
	billDuration    *otel.Histogram
	ministersActive *otel.Counter
}

// New returns a new Whip bound to the given database and hocDir.
// cfg may be nil for backward compatibility (defaults will be used).
func New(db *store.DB, hocDir string, cfgs ...*config.Config) *Whip {
	m := otel.Metrics()
	whipMetrics.byElectionTotal = m.Counter("hoc_by_election_total")
	whipMetrics.conflictsTotal = m.Counter("hoc_conflicts_total")
	whipMetrics.billDuration = m.Histogram("hoc_bills_duration_seconds")
	whipMetrics.ministersActive = m.Counter("hoc_ministers_active_total")

	var cfg *config.Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	return &Whip{
		db:               db,
		hocDir:           hocDir,
		tracer:           otel.GlobalTracer("whip"),
		cfg:              cfg,
		lastContextAlert: make(map[string]time.Time),
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
	ctx, cancel := context.WithTimeout(context.Background(), tickTimeout)
	defer cancel()

	_, span := w.tracer.Start(ctx, "whip.tick")
	defer span.End()

	w.threeLineWhip()
	w.orderPaper()
	w.pollDoneFiles()
	w.pollAckFiles()             // Phase 2: ACK protocol
	w.pollIdleMinisterReassign() // Phase 3B: Hook queue auto-reassign
	w.committeeAutomation()
	w.checkContextHealth()
	w.gazetteDispatch()
	w.autoscale()
}

// scaleUpThreshold returns the configured scale-up threshold, defaulting to 2.
func (w *Whip) scaleUpThreshold() int {
	if w.cfg != nil && w.cfg.Whip.ScaleUpThreshold > 0 {
		return w.cfg.Whip.ScaleUpThreshold
	}
	return 2
}

// scaleDownThreshold returns the configured scale-down threshold, defaulting to 2.
func (w *Whip) scaleDownThreshold() int {
	if w.cfg != nil && w.cfg.Whip.ScaleDownThreshold > 0 {
		return w.cfg.Whip.ScaleDownThreshold
	}
	return 2
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// gazetteID generates a unique ID for gazettes using timestamp + random bytes.
func gazetteID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("gaz-%s-%x", time.Now().Format("20060102150405"), b)
}
