package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/house-of-cards/hoc/internal/store"
)

// TestStoreCoverage_AllCRUD exercises a large batch of uncovered CRUD methods in
// a single seeded scenario. Grouped into sub-tests so failure locations are
// obvious.
func TestStoreCoverage_AllCRUD(t *testing.T) {
	db := newTestDB(t)

	// ── Seed ministers ──────────────────────────────────────────────────────
	m1 := &store.Minister{ID: "m1", Title: "Backend", Runtime: "claude-code", Skills: `["go"]`, Status: "idle"}
	m2 := &store.Minister{ID: "m2", Title: "Frontend", Runtime: "claude-code", Skills: `["ts"]`, Status: "offline"}
	m3 := &store.Minister{ID: "m3", Title: "Empty", Runtime: "claude-code", Skills: "", Status: "offline"}
	for _, m := range []*store.Minister{m1, m2, m3} {
		if err := db.CreateMinister(m); err != nil {
			t.Fatalf("CreateMinister %s: %v", m.ID, err)
		}
	}

	t.Run("ListMinisters", func(t *testing.T) {
		all, err := db.ListMinisters()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 3 {
			t.Errorf("want 3, got %d", len(all))
		}
	})

	t.Run("UpdateMinisterWorktree/PID/Clear", func(t *testing.T) {
		if err := db.UpdateMinisterWorktree("m1", "/tmp/wt-m1"); err != nil {
			t.Fatalf("UpdateMinisterWorktree: %v", err)
		}
		if err := db.UpdateMinisterPID("m1", 1234); err != nil {
			t.Fatalf("UpdateMinisterPID: %v", err)
		}
		got, _ := db.GetMinister("m1")
		if got.Worktree.String != "/tmp/wt-m1" || got.Pid != 1234 {
			t.Errorf("worktree/pid not persisted: %+v", got)
		}

		withWT, err := db.ListMinistersWithWorktree()
		if err != nil {
			t.Fatal(err)
		}
		if len(withWT) != 1 || withWT[0].ID != "m1" {
			t.Errorf("ListMinistersWithWorktree = %v", withWT)
		}

		if err := db.ClearMinisterWorktree("m1"); err != nil {
			t.Fatalf("ClearMinisterWorktree: %v", err)
		}
		got, _ = db.GetMinister("m1")
		if got.Worktree.String != "" {
			t.Errorf("worktree should be empty after clear: %q", got.Worktree.String)
		}
	})

	t.Run("ListOfflineMinisters", func(t *testing.T) {
		offs, err := db.ListOfflineMinisters()
		if err != nil {
			t.Fatal(err)
		}
		// m2 has skills & offline; m3 has empty skills; m1 is idle. Only m2.
		if len(offs) != 1 || offs[0].ID != "m2" {
			t.Errorf("want [m2], got %v", offs)
		}
	})

	// ── Seed session + bills ────────────────────────────────────────────────
	s1 := &store.Session{ID: "s1", Title: "Sprint 1", Topology: "pipeline", Status: "active"}
	if err := db.CreateSession(s1); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	s2 := &store.Session{ID: "s2", Title: "Parallel", Topology: "fanout", Status: "active",
		AckMode: store.NullString("non-blocking")}
	if err := db.CreateSession(s2); err != nil {
		t.Fatalf("CreateSession s2: %v", err)
	}

	t.Run("ListSessions", func(t *testing.T) {
		all, err := db.ListSessions()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Errorf("want 2 sessions, got %d", len(all))
		}
	})

	t.Run("Session.EffectiveAckMode", func(t *testing.T) {
		got, _ := db.GetSession("s1")
		if got.EffectiveAckMode() != "blocking" {
			t.Errorf("pipeline auto = %q, want blocking", got.EffectiveAckMode())
		}
		got2, _ := db.GetSession("s2")
		if got2.EffectiveAckMode() != "non-blocking" {
			t.Errorf("explicit non-blocking = %q", got2.EffectiveAckMode())
		}
		// Auto + non-pipeline → non-blocking.
		sAuto := &store.Session{Topology: "fanout", AckMode: store.NullString("auto")}
		if sAuto.EffectiveAckMode() != "non-blocking" {
			t.Error("auto+fanout should resolve to non-blocking")
		}
	})

	t.Run("UpdateSessionProjects+GetProjectsSlice", func(t *testing.T) {
		if err := db.UpdateSessionProjects("s1", `["proj-a","proj-b"]`); err != nil {
			t.Fatal(err)
		}
		got, _ := db.GetSession("s1")
		ps := got.GetProjectsSlice()
		if len(ps) != 2 || ps[0] != "proj-a" {
			t.Errorf("projects = %v", ps)
		}

		// Legacy: Project field fallback.
		legacy := &store.Session{Project: store.NullString("legacy-proj")}
		if got := legacy.GetProjectsSlice(); len(got) != 1 || got[0] != "legacy-proj" {
			t.Errorf("legacy fallback = %v", got)
		}
		// Empty projects + no legacy → nil.
		empty := &store.Session{}
		if got := empty.GetProjectsSlice(); got != nil {
			t.Errorf("empty = %v", got)
		}
		// Malformed JSON → nil.
		bad := &store.Session{Projects: store.NullString("not-json")}
		if got := bad.GetProjectsSlice(); got != nil {
			t.Errorf("malformed = %v", got)
		}
	})

	t.Run("UpdateSessionStatus+UpdateSessionProject", func(t *testing.T) {
		if err := db.UpdateSessionStatus("s1", "paused"); err != nil {
			t.Fatal(err)
		}
		got, _ := db.GetSession("s1")
		if got.Status != "paused" {
			t.Errorf("status = %q", got.Status)
		}
		if err := db.UpdateSessionProject("s1", "single-proj"); err != nil {
			t.Fatal(err)
		}
		got, _ = db.GetSession("s1")
		if got.Project.String != "single-proj" {
			t.Errorf("project = %q", got.Project.String)
		}
	})

	// Seed bills with depends_on + assignee.
	b1 := &store.Bill{ID: "b1", SessionID: store.NullString("s1"), Title: "A", Status: "enacted",
		Assignee: store.NullString("m1")}
	b2 := &store.Bill{ID: "b2", SessionID: store.NullString("s1"), Title: "B", Status: "committee",
		DependsOn: store.NullString(`["b1"]`), Assignee: store.NullString("m1")}
	b3 := &store.Bill{ID: "b3", SessionID: store.NullString("s1"), Title: "C", Status: "draft",
		ParentBill: "b1", Assignee: store.NullString("m2")}
	for _, b := range []*store.Bill{b1, b2, b3} {
		if err := db.CreateBill(b); err != nil {
			t.Fatalf("CreateBill %s: %v", b.ID, err)
		}
	}

	t.Run("GetDownstreamBills", func(t *testing.T) {
		downs, err := db.GetDownstreamBills("b1")
		if err != nil {
			t.Fatal(err)
		}
		if len(downs) != 1 || downs[0].ID != "b2" {
			t.Errorf("downstream = %v", downs)
		}
	})

	t.Run("UpdateBillBranch+UpdateBillProject", func(t *testing.T) {
		if err := db.UpdateBillBranch("b1", "feat/b1"); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdateBillProject("b1", "proj-a"); err != nil {
			t.Fatal(err)
		}
		got, _ := db.GetBill("b1")
		if got.Branch.String != "feat/b1" || got.Project.String != "proj-a" {
			t.Errorf("branch/project = %+v", got)
		}
	})

	t.Run("ListBillsWithBranchBySession", func(t *testing.T) {
		bills, err := db.ListBillsWithBranchBySession("s1")
		if err != nil {
			t.Fatal(err)
		}
		if len(bills) != 1 || bills[0].ID != "b1" {
			t.Errorf("enacted+branch = %v", bills)
		}
	})

	t.Run("ListSubBills", func(t *testing.T) {
		subs, err := db.ListSubBills("b1")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 1 || subs[0].ID != "b3" {
			t.Errorf("subs = %v", subs)
		}
	})

	t.Run("ListBillsForCommittee", func(t *testing.T) {
		bs, err := db.ListBillsForCommittee()
		if err != nil {
			t.Fatal(err)
		}
		if len(bs) != 1 || bs[0].ID != "b2" {
			t.Errorf("committee = %v", bs)
		}
	})

	t.Run("UnassignBill", func(t *testing.T) {
		if err := db.UnassignBill("b3"); err != nil {
			t.Fatal(err)
		}
		got, _ := db.GetBill("b3")
		if got.Assignee.Valid && got.Assignee.String != "" {
			t.Errorf("assignee should be cleared: %q", got.Assignee.String)
		}
		// Status preserved.
		if got.Status != "draft" {
			t.Errorf("status should be preserved: %q", got.Status)
		}
	})

	// ── Seed gazettes + hansards ────────────────────────────────────────────
	g1 := &store.Gazette{ID: "g1", FromMinister: store.NullString("m1"), ToMinister: store.NullString("m2"),
		BillID: store.NullString("b1"), Summary: "hi"}
	g2 := &store.Gazette{ID: "g2", FromMinister: store.NullString("m1"),
		BillID: store.NullString("b1"), Type: store.NullString("question"), Summary: "Q",
		Payload: `{"context_health":{"cost_ratio":0.42,"turns_since":5}}`}
	if err := db.CreateGazette(g1); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateGazette(g2); err != nil {
		t.Fatal(err)
	}

	t.Run("ListGazettesForMinister", func(t *testing.T) {
		gs, err := db.ListGazettesForMinister("m2")
		if err != nil {
			t.Fatal(err)
		}
		if len(gs) < 1 {
			t.Errorf("want ≥1 gazette for m2, got %d", len(gs))
		}
	})

	t.Run("ListGazettesForBill", func(t *testing.T) {
		gs, err := db.ListGazettesForBill("b1")
		if err != nil {
			t.Fatal(err)
		}
		if len(gs) != 2 {
			t.Errorf("want 2, got %d", len(gs))
		}
	})

	t.Run("ListUnreadGazettes+MarkGazetteRead", func(t *testing.T) {
		unread, err := db.ListUnreadGazettes()
		if err != nil {
			t.Fatal(err)
		}
		if len(unread) < 1 {
			t.Fatal("want unread gazettes")
		}
		if err := db.MarkGazetteRead(unread[0].ID); err != nil {
			t.Fatal(err)
		}
		after, _ := db.ListUnreadGazettes()
		if len(after) >= len(unread) {
			t.Errorf("unread count should decrease: %d→%d", len(unread), len(after))
		}
	})

	t.Run("GetLatestContextHealth", func(t *testing.T) {
		ch, err := db.GetLatestContextHealth("m1")
		if err != nil {
			t.Fatalf("ctx health err: %v", err)
		}
		if ch == nil {
			t.Fatal("expected context health payload")
		}
		// No payload for m2 → nil, nil.
		ch2, err := db.GetLatestContextHealth("m2")
		if err != nil {
			t.Fatal(err)
		}
		if ch2 != nil {
			t.Errorf("want nil for m2, got %+v", ch2)
		}
	})

	// Seed hansards.
	hEnacted := &store.Hansard{ID: "h1", MinisterID: "m1", BillID: "b1",
		Outcome: store.NullString("enacted"), DurationS: 60, Quality: 0.8}
	hByElection := &store.Hansard{ID: "h2", MinisterID: "m1", BillID: "b2",
		Outcome: store.NullString("partial"), DurationS: 30, Quality: 0.4,
		Notes: store.NullString("补选触发")}
	if err := db.CreateHansard(hEnacted); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateHansard(hByElection); err != nil {
		t.Fatal(err)
	}

	t.Run("ListHansard+ByMinister+Recent+BySession", func(t *testing.T) {
		all, err := db.ListHansard()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 2 {
			t.Errorf("want 2 hansards, got %d", len(all))
		}
		byM, _ := db.ListHansardByMinister("m1")
		if len(byM) != 2 {
			t.Errorf("want 2 by m1, got %d", len(byM))
		}
		recent, _ := db.ListRecentHansard(1)
		if len(recent) != 1 {
			t.Errorf("limit=1 want 1, got %d", len(recent))
		}
		bySess, _ := db.ListHansardBySession("s1")
		if len(bySess) != 2 {
			t.Errorf("session hansard = %d", len(bySess))
		}
	})

	t.Run("UpdateHansardQuality+Metrics", func(t *testing.T) {
		if err := db.UpdateHansardQuality("h1", 0.95); err != nil {
			t.Fatal(err)
		}
		if err := db.UpdateHansardMetrics("h1", 2, 45); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ListByElectionHansard", func(t *testing.T) {
		bes, err := db.ListByElectionHansard(10)
		if err != nil {
			t.Fatal(err)
		}
		if len(bes) != 1 || bes[0].ID != "h2" {
			t.Errorf("by-election hansard = %v", bes)
		}
	})

	t.Run("GetWhipStats", func(t *testing.T) {
		// Add a stuck minister so StuckMinisters is exercised.
		if err := db.UpdateMinisterStatus("m1", "stuck"); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = db.UpdateMinisterStatus("m1", "idle") }()

		stats, err := db.GetWhipStats()
		if err != nil {
			t.Fatal(err)
		}
		if stats.ByElectionCount < 1 {
			t.Errorf("by-election count = %d", stats.ByElectionCount)
		}
		if stats.AvgDurationS <= 0 {
			t.Errorf("avg dur = %f", stats.AvgDurationS)
		}
		if len(stats.StuckMinisters) != 1 {
			t.Errorf("stuck = %d", len(stats.StuckMinisters))
		}
	})

	t.Run("FirstACKRate", func(t *testing.T) {
		// b1 is enacted but has a question-type gazette (g2). Add one enacted
		// bill with NO question to exercise the zero-round branch too.
		bClean := &store.Bill{ID: "b-clean", SessionID: store.NullString("s1"),
			Title: "No Q", Status: "enacted"}
		if err := db.CreateBill(bClean); err != nil {
			t.Fatal(err)
		}

		rate, err := db.FirstACKRate("s1")
		if err != nil {
			t.Fatal(err)
		}
		// Two enacted bills (b1, b-clean); b-clean has 0 questions → rate=0.5.
		if rate <= 0 || rate >= 1 {
			t.Errorf("expected mixed rate in (0,1), got %f", rate)
		}

		// Session with no enacted bills → 0.
		rate0, _ := db.FirstACKRate("s2")
		if rate0 != 0 {
			t.Errorf("empty session rate = %f", rate0)
		}
	})

	t.Run("EnactBillFromDone", func(t *testing.T) {
		// New bill that starts as draft.
		nb := &store.Bill{ID: "enact-me", SessionID: store.NullString("s1"),
			Title: "To enact", Status: "draft"}
		if err := db.CreateBill(nb); err != nil {
			t.Fatal(err)
		}
		if err := db.EnactBillFromDone("enact-me", "m1", "all green", `{"ok":true}`); err != nil {
			t.Fatalf("EnactBillFromDone: %v", err)
		}
		got, _ := db.GetBill("enact-me")
		if got.Status != "enacted" {
			t.Errorf("status = %q", got.Status)
		}
	})

	t.Run("GetAllSessionStats", func(t *testing.T) {
		stats, err := db.GetAllSessionStats()
		if err != nil {
			t.Fatal(err)
		}
		if len(stats) < 2 {
			t.Errorf("want stats for ≥2 sessions, got %d", len(stats))
		}
	})

	t.Run("DB+Ping", func(t *testing.T) {
		raw := db.DB()
		if raw == nil {
			t.Fatal("DB() returned nil")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			t.Errorf("Ping: %v", err)
		}
	})
}
