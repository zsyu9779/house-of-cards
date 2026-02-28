// Package store provides SQLite storage layer for House of Cards
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
	path string
}

func NewDB(homeDir string) (*DB, error) {
	stateDir := filepath.Join(homeDir, ".hoc")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	dbPath := filepath.Join(stateDir, "state.db")
	conn, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db := &DB{conn: conn, path: dbPath}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS ministers (
		id TEXT PRIMARY KEY,
		title TEXT,
		runtime TEXT NOT NULL,
		skills TEXT,
		status TEXT DEFAULT 'offline',
		pid INTEGER DEFAULT 0,
		worktree TEXT,
		heartbeat DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		topology TEXT NOT NULL,
		config TEXT,
		status TEXT DEFAULT 'active',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS bills (
		id TEXT PRIMARY KEY,
		session_id TEXT,
		title TEXT NOT NULL,
		description TEXT,
		status TEXT DEFAULT 'draft',
		assignee TEXT REFERENCES ministers(id),
		depends_on TEXT,
		branch TEXT,
		portfolio TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS gazettes (
		id TEXT PRIMARY KEY,
		from_minister TEXT,
		to_minister TEXT,
		bill_id TEXT,
		type TEXT,
		summary TEXT NOT NULL,
		artifacts TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		read_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS hansard (
		id TEXT PRIMARY KEY,
		minister_id TEXT NOT NULL,
		bill_id TEXT NOT NULL,
		outcome TEXT,
		duration_s INTEGER DEFAULT 0,
		skills_used TEXT,
		quality REAL DEFAULT 0,
		notes TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`
	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}
	// Idempotent migrations for columns added after initial schema.
	_, _ = db.conn.Exec(`ALTER TABLE bills ADD COLUMN portfolio TEXT DEFAULT ''`)
	return nil
}

// NullString converts a plain string to sql.NullString.
// Empty string is treated as NULL.
func NullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// Minister CRUD
type Minister struct {
	ID        string
	Title     string
	Runtime   string
	Skills    string
	Status    string
	Pid       int
	Worktree  sql.NullString
	Heartbeat sql.NullTime
	CreatedAt time.Time
}

func (db *DB) CreateMinister(m *Minister) error {
	_, err := db.conn.Exec(
		`INSERT INTO ministers (id, title, runtime, skills, status, pid) VALUES (?, ?, ?, ?, ?, ?)`,
		m.ID, m.Title, m.Runtime, m.Skills, m.Status, m.Pid,
	)
	return err
}

func (db *DB) GetMinister(id string) (*Minister, error) {
	var m Minister
	err := db.conn.QueryRow(
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, created_at 
		 FROM ministers WHERE id = ?`, id,
	).Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (db *DB) ListMinisters() ([]*Minister, error) {
	rows, err := db.conn.Query(`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, created_at FROM ministers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.CreatedAt); err != nil {
			return nil, err
		}
		ministers = append(ministers, &m)
	}
	return ministers, nil
}

func (db *DB) UpdateMinisterStatus(id, status string) error {
	_, err := db.conn.Exec(`UPDATE ministers SET status = ? WHERE id = ?`, status, id)
	return err
}

func (db *DB) UpdateMinisterHeartbeat(id string) error {
	_, err := db.conn.Exec(`UPDATE ministers SET heartbeat = ? WHERE id = ?`, time.Now(), id)
	return err
}

func (db *DB) DeleteMinister(id string) error {
	_, err := db.conn.Exec(`DELETE FROM ministers WHERE id = ?`, id)
	return err
}

// Session CRUD
type Session struct {
	ID        string
	Title     string
	Topology  string
	Config    sql.NullString
	Status    string
	CreatedAt time.Time
}

func (db *DB) CreateSession(s *Session) error {
	_, err := db.conn.Exec(
		`INSERT INTO sessions (id, title, topology, config, status) VALUES (?, ?, ?, ?, ?)`,
		s.ID, s.Title, s.Topology, s.Config, s.Status,
	)
	return err
}

func (db *DB) GetSession(id string) (*Session, error) {
	var s Session
	err := db.conn.QueryRow(
		`SELECT id, title, topology, COALESCE(config,''), status, created_at FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.Title, &s.Topology, &s.Config, &s.Status, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) ListSessions() ([]*Session, error) {
	rows, err := db.conn.Query(`SELECT id, title, topology, COALESCE(config,''), status, created_at FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Title, &s.Topology, &s.Config, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}

// Bill CRUD
type Bill struct {
	ID          string
	SessionID   sql.NullString
	Title       string
	Description sql.NullString
	Status      string
	Assignee    sql.NullString
	DependsOn   sql.NullString
	Branch      sql.NullString
	Portfolio   sql.NullString
	CreatedAt   time.Time
	UpdatedAt   sql.NullTime
}

func (db *DB) CreateBill(b *Bill) error {
	_, err := db.conn.Exec(
		`INSERT INTO bills (id, session_id, title, description, status, assignee, depends_on, branch, portfolio)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.SessionID, b.Title, b.Description, b.Status, b.Assignee, b.DependsOn, b.Branch, b.Portfolio,
	)
	return err
}

func (db *DB) GetBill(id string) (*Bill, error) {
	var b Bill
	err := db.conn.QueryRow(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), created_at, updated_at
		 FROM bills WHERE id = ?`, id,
	).Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (db *DB) ListBills() ([]*Bill, error) {
	rows, err := db.conn.Query(`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), created_at, updated_at FROM bills`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, &b)
	}
	return bills, nil
}

func (db *DB) UpdateBillStatus(id, status string) error {
	_, err := db.conn.Exec(`UPDATE bills SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now(), id)
	return err
}

func (db *DB) AssignBill(billID, ministerID string) error {
	_, err := db.conn.Exec(`UPDATE bills SET assignee = ?, updated_at = ? WHERE id = ?`, ministerID, time.Now(), billID)
	return err
}

func (db *DB) UpdateBillBranch(billID, branch string) error {
	_, err := db.conn.Exec(`UPDATE bills SET branch = ?, updated_at = ? WHERE id = ?`, branch, time.Now(), billID)
	return err
}

// Gazette CRUD
type Gazette struct {
	ID            string
	FromMinister  sql.NullString
	ToMinister    sql.NullString
	BillID        sql.NullString
	Type          sql.NullString
	Summary       string
	Artifacts     sql.NullString
	CreatedAt     time.Time
	ReadAt        sql.NullTime
}

func (db *DB) CreateGazette(g *Gazette) error {
	_, err := db.conn.Exec(
		`INSERT INTO gazettes (id, from_minister, to_minister, bill_id, type, summary, artifacts) 
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.FromMinister, g.ToMinister, g.BillID, g.Type, g.Summary, g.Artifacts,
	)
	return err
}

func (db *DB) ListGazettesForMinister(ministerID string) ([]*Gazette, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(from_minister,''), COALESCE(to_minister,''), COALESCE(bill_id,''), COALESCE(type,''), summary, COALESCE(artifacts,''), created_at, read_at 
		 FROM gazettes WHERE to_minister = ? OR to_minister IS NULL ORDER BY created_at DESC`, ministerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gazettes []*Gazette
	for rows.Next() {
		var g Gazette
		if err := rows.Scan(&g.ID, &g.FromMinister, &g.ToMinister, &g.BillID, &g.Type, &g.Summary, &g.Artifacts, &g.CreatedAt, &g.ReadAt); err != nil {
			return nil, err
		}
		gazettes = append(gazettes, &g)
	}
	return gazettes, nil
}

// Hansard CRUD
type Hansard struct {
	ID         string
	MinisterID string
	BillID     string
	Outcome    sql.NullString
	DurationS  int
	SkillsUsed sql.NullString
	Quality    float64
	Notes      sql.NullString
	CreatedAt  time.Time
}

func (db *DB) CreateHansard(h *Hansard) error {
	_, err := db.conn.Exec(
		`INSERT INTO hansard (id, minister_id, bill_id, outcome, duration_s, skills_used, quality, notes) 
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ID, h.MinisterID, h.BillID, h.Outcome, h.DurationS, h.SkillsUsed, h.Quality, h.Notes,
	)
	return err
}

func (db *DB) UpdateMinisterWorktree(id, worktreePath string) error {
	_, err := db.conn.Exec(`UPDATE ministers SET worktree = ? WHERE id = ?`, worktreePath, id)
	return err
}

func (db *DB) UpdateMinisterPID(id string, pid int) error {
	_, err := db.conn.Exec(`UPDATE ministers SET pid = ? WHERE id = ?`, pid, id)
	return err
}

func (db *DB) ListBillsBySession(sessionID string) ([]*Bill, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), created_at, updated_at
		 FROM bills WHERE session_id = ?`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, &b)
	}
	return bills, nil
}

func (db *DB) ListGazettes() ([]*Gazette, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(from_minister,''), COALESCE(to_minister,''), COALESCE(bill_id,''), COALESCE(type,''), summary, COALESCE(artifacts,''), created_at, read_at
		 FROM gazettes ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gazettes []*Gazette
	for rows.Next() {
		var g Gazette
		if err := rows.Scan(&g.ID, &g.FromMinister, &g.ToMinister, &g.BillID, &g.Type, &g.Summary, &g.Artifacts, &g.CreatedAt, &g.ReadAt); err != nil {
			return nil, err
		}
		gazettes = append(gazettes, &g)
	}
	return gazettes, nil
}

func (db *DB) ListGazettesForBill(billID string) ([]*Gazette, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(from_minister,''), COALESCE(to_minister,''), COALESCE(bill_id,''), COALESCE(type,''), summary, COALESCE(artifacts,''), created_at, read_at
		 FROM gazettes WHERE bill_id = ? ORDER BY created_at DESC`, billID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gazettes []*Gazette
	for rows.Next() {
		var g Gazette
		if err := rows.Scan(&g.ID, &g.FromMinister, &g.ToMinister, &g.BillID, &g.Type, &g.Summary, &g.Artifacts, &g.CreatedAt, &g.ReadAt); err != nil {
			return nil, err
		}
		gazettes = append(gazettes, &g)
	}
	return gazettes, nil
}

func (db *DB) UpdateSessionStatus(id, status string) error {
	_, err := db.conn.Exec(`UPDATE sessions SET status = ? WHERE id = ?`, status, id)
	return err
}

// ListHansard returns all hansard entries, newest first.
func (db *DB) ListHansard() ([]*Hansard, error) {
	rows, err := db.conn.Query(
		`SELECT id, minister_id, bill_id, COALESCE(outcome,''), COALESCE(duration_s,0), COALESCE(skills_used,''), COALESCE(quality,0), COALESCE(notes,''), created_at
		 FROM hansard ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*Hansard
	for rows.Next() {
		var h Hansard
		if err := rows.Scan(&h.ID, &h.MinisterID, &h.BillID, &h.Outcome, &h.DurationS, &h.SkillsUsed, &h.Quality, &h.Notes, &h.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, &h)
	}
	return entries, nil
}

// ListHansardByMinister returns hansard entries for a specific minister, newest first.
func (db *DB) ListHansardByMinister(ministerID string) ([]*Hansard, error) {
	rows, err := db.conn.Query(
		`SELECT id, minister_id, bill_id, COALESCE(outcome,''), COALESCE(duration_s,0), COALESCE(skills_used,''), COALESCE(quality,0), COALESCE(notes,''), created_at
		 FROM hansard WHERE minister_id = ? ORDER BY created_at DESC`, ministerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*Hansard
	for rows.Next() {
		var h Hansard
		if err := rows.Scan(&h.ID, &h.MinisterID, &h.BillID, &h.Outcome, &h.DurationS, &h.SkillsUsed, &h.Quality, &h.Notes, &h.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, &h)
	}
	return entries, nil
}

// GetBillsByAssignee returns all bills currently assigned to the given minister.
func (db *DB) GetBillsByAssignee(ministerID string) ([]*Bill, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), created_at, updated_at
		 FROM bills WHERE assignee = ?`, ministerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, &b)
	}
	return bills, nil
}

// ClearBillAssignment removes the assignee from a bill and resets its status to "draft".
func (db *DB) ClearBillAssignment(billID string) error {
	_, err := db.conn.Exec(
		`UPDATE bills SET assignee = NULL, status = 'draft', updated_at = ? WHERE id = ?`,
		time.Now(), billID,
	)
	return err
}

// HansardSuccessRate returns the number of enacted vs total completed bills for a minister.
func (db *DB) HansardSuccessRate(ministerID string) (enacted, total int, err error) {
	rows, err := db.conn.Query(
		`SELECT outcome FROM hansard WHERE minister_id = ?`, ministerID,
	)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var outcome string
		if err := rows.Scan(&outcome); err != nil {
			return 0, 0, err
		}
		total++
		if outcome == "enacted" {
			enacted++
		}
	}
	return enacted, total, nil
}

// ListMinistersWithStatus returns all ministers with the given status.
func (db *DB) ListMinistersWithStatus(status string) ([]*Minister, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, created_at
		 FROM ministers WHERE status = ?`, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.CreatedAt); err != nil {
			return nil, err
		}
		ministers = append(ministers, &m)
	}
	return ministers, nil
}

// ListWorkingMinisters returns all ministers currently in "working" status.
func (db *DB) ListWorkingMinisters() ([]*Minister, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, created_at
		 FROM ministers WHERE status = 'working'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.CreatedAt); err != nil {
			return nil, err
		}
		ministers = append(ministers, &m)
	}
	return ministers, nil
}

// ListIdleMinistersForSkill returns ministers in "idle" status whose skills include the given skill.
// An empty skill matches all idle ministers.
func (db *DB) ListIdleMinistersForSkill(skill string) ([]*Minister, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, created_at
		 FROM ministers WHERE status = 'idle'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.CreatedAt); err != nil {
			return nil, err
		}
		if skill == "" || ministerHasSkill(m.Skills, skill) {
			ministers = append(ministers, &m)
		}
	}
	return ministers, nil
}

// ministerHasSkill checks if the minister's JSON skills list contains the given skill.
func ministerHasSkill(skillsJSON, skill string) bool {
	if skillsJSON == "" {
		return false
	}
	// Quick substring check before JSON parse.
	return strings.Contains(skillsJSON, `"`+skill+`"`)
}

// ListActiveSessions returns all sessions with status "active".
func (db *DB) ListActiveSessions() ([]*Session, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, topology, COALESCE(config,''), status, created_at
		 FROM sessions WHERE status = 'active'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Title, &s.Topology, &s.Config, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}

// ListUnreadGazettes returns gazettes that have not been marked as read.
func (db *DB) ListUnreadGazettes() ([]*Gazette, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(from_minister,''), COALESCE(to_minister,''), COALESCE(bill_id,''), COALESCE(type,''), summary, COALESCE(artifacts,''), created_at, read_at
		 FROM gazettes WHERE read_at IS NULL ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gazettes []*Gazette
	for rows.Next() {
		var g Gazette
		if err := rows.Scan(&g.ID, &g.FromMinister, &g.ToMinister, &g.BillID, &g.Type, &g.Summary, &g.Artifacts, &g.CreatedAt, &g.ReadAt); err != nil {
			return nil, err
		}
		gazettes = append(gazettes, &g)
	}
	return gazettes, nil
}

// MarkGazetteRead sets the read_at timestamp on a gazette.
func (db *DB) MarkGazetteRead(id string) error {
	_, err := db.conn.Exec(`UPDATE gazettes SET read_at = ? WHERE id = ?`, time.Now(), id)
	return err
}

// DB returns the underlying database connection (for migrations, etc.)
func (db *DB) DB() *sql.DB {
	return db.conn
}

// Ping checks database connectivity
func (db *DB) Ping(ctx context.Context) error {
	return db.conn.PingContext(ctx)
}
