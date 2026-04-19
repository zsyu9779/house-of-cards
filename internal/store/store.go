// Package store provides SQLite storage layer for House of Cards.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// isDuplicateColumnErr reports whether err is SQLite's "duplicate column" error,
// returned by ALTER TABLE ADD COLUMN when the column already exists.
func isDuplicateColumnErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column")
}

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
	// _pragma=busy_timeout(5000) lets SQLite wait up to 5s on write-lock contention
	// instead of returning SQLITE_BUSY immediately — required for correct behaviour
	// under concurrent writers (Whip tick + CLI commands + API handlers share the DB).
	conn, err := sql.Open("sqlite",
		dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
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
		read_at DATETIME,
		ack_status TEXT DEFAULT '',
		thread_id TEXT DEFAULT '',
		reply_to TEXT DEFAULT '',
		payload TEXT DEFAULT ''
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
	// Idempotent column migrations. Each ALTER is best-effort: SQLite returns
	// "duplicate column" when the column already exists, which is the normal
	// case on every boot after the first. Other errors are logged for visibility
	// but not fatal, since migrate() must stay runnable across upgrade paths.
	migrations := []string{
		`ALTER TABLE bills ADD COLUMN portfolio TEXT DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN project TEXT DEFAULT ''`,
		// Phase 10: multi-project support.
		`ALTER TABLE bills ADD COLUMN project TEXT DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN projects TEXT DEFAULT '[]'`,
		// Phase 3B: Minister hook queue (JSON array of bill IDs).
		`ALTER TABLE ministers ADD COLUMN hook TEXT DEFAULT '[]'`,
		// Phase 2: Gazette ACK protocol + structured payload.
		`ALTER TABLE gazettes ADD COLUMN ack_status TEXT DEFAULT ''`,
		`ALTER TABLE gazettes ADD COLUMN thread_id TEXT DEFAULT ''`,
		`ALTER TABLE gazettes ADD COLUMN reply_to TEXT DEFAULT ''`,
		`ALTER TABLE gazettes ADD COLUMN payload TEXT DEFAULT ''`,
		// Phase 2: Session ACK mode.
		`ALTER TABLE sessions ADD COLUMN ack_mode TEXT DEFAULT 'auto'`,
		// Phase 3: B-3 Bill split — parent bill reference.
		`ALTER TABLE bills ADD COLUMN parent_bill TEXT DEFAULT ''`,
		// Phase 4: B-1.4 Question Time metrics on hansard.
		`ALTER TABLE hansard ADD COLUMN ack_rounds INTEGER DEFAULT 0`,
		`ALTER TABLE hansard ADD COLUMN briefing_time_s INTEGER DEFAULT 0`,
		// v0.3: Whip graduated recovery.
		`ALTER TABLE ministers ADD COLUMN recovery_attempts INTEGER DEFAULT 0`,
	}
	for _, stmt := range migrations {
		if _, err := db.conn.Exec(stmt); err != nil && !isDuplicateColumnErr(err) {
			slog.Warn("migrate: ALTER TABLE failed", "stmt", stmt, "err", err)
		}
	}

	// Indexes for common query patterns.
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_ministers_status ON ministers(status)`,
		`CREATE INDEX IF NOT EXISTS idx_bills_session_id ON bills(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bills_status ON bills(status)`,
		`CREATE INDEX IF NOT EXISTS idx_gazettes_read_at ON gazettes(read_at)`,
		`CREATE INDEX IF NOT EXISTS idx_gazettes_minister_id ON gazettes(to_minister)`,
	}
	for _, idx := range indexes {
		if _, err := db.conn.Exec(idx); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// Event Ledger table (D-1).
	eventSchema := `
	CREATE TABLE IF NOT EXISTS events (
		id TEXT PRIMARY KEY,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		topic TEXT NOT NULL,
		bill_id TEXT,
		minister_id TEXT,
		session_id TEXT,
		source TEXT NOT NULL,
		payload TEXT DEFAULT '{}'
	);

	CREATE INDEX IF NOT EXISTS idx_events_topic ON events(topic);
	CREATE INDEX IF NOT EXISTS idx_events_bill_id ON events(bill_id);
	CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
	`
	if _, err := db.conn.Exec(eventSchema); err != nil {
		return fmt.Errorf("create events table: %w", err)
	}

	return nil
}

// NullString converts a plain string to sql.NullString.
// Empty string is treated as NULL.
func NullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// Minister CRUD.
type Minister struct {
	ID               string
	Title            string
	Runtime          string
	Skills           string
	Status           string
	Pid              int
	Worktree         sql.NullString
	Heartbeat        sql.NullTime
	Hook             string // Phase 3B: JSON array of queued bill IDs
	RecoveryAttempts int    // v0.3: graduated recovery attempt counter
	CreatedAt        time.Time
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
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, COALESCE(hook,'[]'), COALESCE(recovery_attempts,0), created_at
		 FROM ministers WHERE id = ?`, id,
	).Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.Hook, &m.RecoveryAttempts, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (db *DB) ListMinisters() ([]*Minister, error) {
	rows, err := db.conn.Query(`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, COALESCE(hook,'[]'), COALESCE(recovery_attempts,0), created_at FROM ministers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.Hook, &m.RecoveryAttempts, &m.CreatedAt); err != nil {
			return nil, err
		}
		ministers = append(ministers, &m)
	}
	return ministers, nil
}

// ListMinistersWithWorktree returns ministers that have a non-empty worktree path.
func (db *DB) ListMinistersWithWorktree() ([]*Minister, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, COALESCE(hook,'[]'), COALESCE(recovery_attempts,0), created_at
		 FROM ministers WHERE worktree IS NOT NULL AND worktree != ''`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.Hook, &m.RecoveryAttempts, &m.CreatedAt); err != nil {
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

// IncrementRecoveryAttempts increments the recovery attempt counter and returns the new value.
func (db *DB) IncrementRecoveryAttempts(ministerID string) (int, error) {
	_, err := db.conn.Exec(
		`UPDATE ministers SET recovery_attempts = recovery_attempts + 1 WHERE id = ?`,
		ministerID,
	)
	if err != nil {
		return 0, err
	}
	var count int
	err = db.conn.QueryRow(
		`SELECT recovery_attempts FROM ministers WHERE id = ?`, ministerID,
	).Scan(&count)
	return count, err
}

// ResetRecoveryAttempts resets the recovery counter (called on by-election or recovery).
func (db *DB) ResetRecoveryAttempts(ministerID string) error {
	_, err := db.conn.Exec(
		`UPDATE ministers SET recovery_attempts = 0 WHERE id = ?`, ministerID,
	)
	return err
}

// Session CRUD.
type Session struct {
	ID        string
	Title     string
	Topology  string
	Config    sql.NullString
	Project   sql.NullString // Deprecated: use Projects (JSON array)
	Projects  sql.NullString // JSON array of project paths
	AckMode   sql.NullString // "blocking" | "non-blocking" | "auto"
	Status    string
	CreatedAt time.Time
}

// EffectiveAckMode returns the resolved ACK mode for the session.
// "auto" resolves to "blocking" for pipeline topology, "non-blocking" otherwise.
func (s *Session) EffectiveAckMode() string {
	mode := s.AckMode.String
	if mode == "" || mode == AckModeAuto {
		if s.Topology == "pipeline" {
			return AckModeBlocking
		}
		return AckModeNonBlocking
	}
	return mode
}

// GetProjectsSlice returns the session's projects as a string slice.
func (s *Session) GetProjectsSlice() []string {
	projectsStr := s.Projects.String
	if projectsStr == "" {
		// Fallback to legacy Project field.
		if s.Project.Valid && s.Project.String != "" {
			return []string{s.Project.String}
		}
		return nil
	}
	var projects []string
	if err := json.Unmarshal([]byte(projectsStr), &projects); err != nil {
		return nil
	}
	return projects
}

func (db *DB) CreateSession(s *Session) error {
	projects := s.Projects.String
	if projects == "" {
		// Fallback: if Projects is empty but Project has value, migrate.
		if s.Project.Valid && s.Project.String != "" {
			projects = `["` + s.Project.String + `"]`
		} else {
			projects = "[]"
		}
	}
	_, err := db.conn.Exec(
		`INSERT INTO sessions (id, title, topology, config, project, projects, status) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.Title, s.Topology, s.Config, s.Project, projects, s.Status,
	)
	return err
}

func (db *DB) GetSession(id string) (*Session, error) {
	var s Session
	err := db.conn.QueryRow(
		`SELECT id, title, topology, COALESCE(config,''), COALESCE(project,''), COALESCE(projects,'[]'), COALESCE(ack_mode,'auto'), status, created_at FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.Title, &s.Topology, &s.Config, &s.Project, &s.Projects, &s.AckMode, &s.Status, &s.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (db *DB) ListSessions() ([]*Session, error) {
	rows, err := db.conn.Query(`SELECT id, title, topology, COALESCE(config,''), COALESCE(project,''), COALESCE(projects,'[]'), COALESCE(ack_mode,'auto'), status, created_at FROM sessions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Title, &s.Topology, &s.Config, &s.Project, &s.Projects, &s.AckMode, &s.Status, &s.CreatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, &s)
	}
	return sessions, nil
}

// Bill CRUD.
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
	Project     sql.NullString // Optional: specific project for this bill
	ParentBill  string         // Phase 3: B-3 parent bill ID for split bills
	CreatedAt   time.Time
	UpdatedAt   sql.NullTime
}

func (db *DB) CreateBill(b *Bill) error {
	project := b.Project
	if !project.Valid {
		project = sql.NullString{String: "", Valid: false}
	}
	_, err := db.conn.Exec(
		`INSERT INTO bills (id, session_id, title, description, status, assignee, depends_on, branch, portfolio, project, parent_bill)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.SessionID, b.Title, b.Description, b.Status, b.Assignee, b.DependsOn, b.Branch, b.Portfolio, project, b.ParentBill,
	)
	return err
}

func (db *DB) GetBill(id string) (*Bill, error) {
	var b Bill
	err := db.conn.QueryRow(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at
		 FROM bills WHERE id = ?`, id,
	).Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// GetDownstreamBills returns bills that depend on the given bill (i.e., downstream bills).
func (db *DB) GetDownstreamBills(billID string) ([]*Bill, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at
		 FROM bills WHERE depends_on LIKE ?`, "%"+billID+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, &b)
	}
	return bills, nil
}

func (db *DB) ListBills() ([]*Bill, error) {
	rows, err := db.conn.Query(`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at FROM bills`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt); err != nil {
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

// UpdateBillProject sets the project field for a bill.
func (db *DB) UpdateBillProject(billID, project string) error {
	_, err := db.conn.Exec(`UPDATE bills SET project = ?, updated_at = ? WHERE id = ?`, project, time.Now(), billID)
	return err
}

// UpdateSessionProjects sets the projects JSON array for a session.
func (db *DB) UpdateSessionProjects(sessionID, projectsJSON string) error {
	_, err := db.conn.Exec(`UPDATE sessions SET projects = ? WHERE id = ?`, projectsJSON, sessionID)
	return err
}

// Gazette CRUD.
type Gazette struct {
	ID           string
	FromMinister sql.NullString
	ToMinister   sql.NullString
	BillID       sql.NullString
	Type         sql.NullString
	Summary      string
	Artifacts    sql.NullString
	CreatedAt    time.Time
	ReadAt       sql.NullTime
	AckStatus    string // '' | 'delivered' | 'ack' | 'questioned' | 'escalated'
	ThreadID     string // 质询线程 ID
	ReplyTo      string // 回复的 Gazette ID
	Payload      string // JSON: {summary, contracts, artifacts, assumptions}
}

func (db *DB) CreateGazette(g *Gazette) error {
	_, err := db.conn.Exec(
		`INSERT INTO gazettes (id, from_minister, to_minister, bill_id, type, summary, artifacts, ack_status, thread_id, reply_to, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.FromMinister, g.ToMinister, g.BillID, g.Type, g.Summary, g.Artifacts, g.AckStatus, g.ThreadID, g.ReplyTo, g.Payload,
	)
	return err
}

// GetLatestContextHealth returns the most recent ContextHealth payload reported
// by the given minister. Returns (nil, nil) when the minister has not yet sent
// any gazette carrying a context_health payload — callers should treat that as
// "no data, skip" rather than an error.
func (db *DB) GetLatestContextHealth(ministerID string) (*ContextHealth, error) {
	var payload string
	err := db.conn.QueryRow(
		`SELECT COALESCE(payload,'') FROM gazettes
		 WHERE from_minister = ? AND payload LIKE '%context_health%'
		 ORDER BY created_at DESC LIMIT 1`,
		ministerID,
	).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if payload == "" {
		return nil, nil
	}

	var wrapper struct {
		ContextHealth *ContextHealth `json:"context_health"`
	}
	if err := json.Unmarshal([]byte(payload), &wrapper); err != nil {
		return nil, fmt.Errorf("parse context_health payload: %w", err)
	}
	return wrapper.ContextHealth, nil
}

func (db *DB) ListGazettesForMinister(ministerID string) ([]*Gazette, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(from_minister,''), COALESCE(to_minister,''), COALESCE(bill_id,''), COALESCE(type,''), summary, COALESCE(artifacts,''), created_at, read_at, COALESCE(ack_status,''), COALESCE(thread_id,''), COALESCE(reply_to,''), COALESCE(payload,'')
		 FROM gazettes WHERE to_minister = ? OR to_minister IS NULL ORDER BY created_at DESC`, ministerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gazettes []*Gazette
	for rows.Next() {
		var g Gazette
		if err := rows.Scan(&g.ID, &g.FromMinister, &g.ToMinister, &g.BillID, &g.Type, &g.Summary, &g.Artifacts, &g.CreatedAt, &g.ReadAt, &g.AckStatus, &g.ThreadID, &g.ReplyTo, &g.Payload); err != nil {
			return nil, err
		}
		gazettes = append(gazettes, &g)
	}
	return gazettes, nil
}

// Hansard CRUD.
type Hansard struct {
	ID            string
	MinisterID    string
	BillID        string
	Outcome       sql.NullString
	DurationS     int
	SkillsUsed    sql.NullString
	Quality       float64
	Notes         sql.NullString
	AckRounds     int
	BriefingTimeS int
	CreatedAt     time.Time
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
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at
		 FROM bills WHERE session_id = ?`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt); err != nil {
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
		`SELECT id, COALESCE(from_minister,''), COALESCE(to_minister,''), COALESCE(bill_id,''), COALESCE(type,''), summary, COALESCE(artifacts,''), created_at, read_at, COALESCE(ack_status,''), COALESCE(thread_id,''), COALESCE(reply_to,''), COALESCE(payload,'')
		 FROM gazettes WHERE bill_id = ? ORDER BY created_at DESC`, billID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gazettes []*Gazette
	for rows.Next() {
		var g Gazette
		if err := rows.Scan(&g.ID, &g.FromMinister, &g.ToMinister, &g.BillID, &g.Type, &g.Summary, &g.Artifacts, &g.CreatedAt, &g.ReadAt, &g.AckStatus, &g.ThreadID, &g.ReplyTo, &g.Payload); err != nil {
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

func (db *DB) UpdateSessionProject(id, project string) error {
	_, err := db.conn.Exec(`UPDATE sessions SET project = ? WHERE id = ?`, project, id)
	return err
}

// ListBillsWithBranchBySession returns enacted bills with a non-empty branch for a session.
func (db *DB) ListBillsWithBranchBySession(sessionID string) ([]*Bill, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at
		 FROM bills WHERE session_id = ? AND (status = 'enacted' OR status = 'royal_assent') AND branch IS NOT NULL AND branch != ''`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, &b)
	}
	return bills, nil
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
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at
		 FROM bills WHERE assignee = ?`, ministerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt); err != nil {
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
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, COALESCE(hook,'[]'), COALESCE(recovery_attempts,0), created_at
		 FROM ministers WHERE status = ?`, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.Hook, &m.RecoveryAttempts, &m.CreatedAt); err != nil {
			return nil, err
		}
		ministers = append(ministers, &m)
	}
	return ministers, nil
}

// ListWorkingMinisters returns all ministers currently in "working" status.
func (db *DB) ListWorkingMinisters() ([]*Minister, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, COALESCE(hook,'[]'), COALESCE(recovery_attempts,0), created_at
		 FROM ministers WHERE status = 'working'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.Hook, &m.RecoveryAttempts, &m.CreatedAt); err != nil {
			return nil, err
		}
		ministers = append(ministers, &m)
	}
	return ministers, nil
}

// ListOfflineMinisters returns all ministers with status "offline" and non-empty skills.
// These are candidates for the autoscale reserve pool.
func (db *DB) ListOfflineMinisters() ([]*Minister, error) {
	rows, err := db.conn.Query(
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, COALESCE(hook,'[]'), COALESCE(recovery_attempts,0), created_at
		 FROM ministers WHERE status = 'offline' AND skills != '' AND skills != '[]'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.Hook, &m.RecoveryAttempts, &m.CreatedAt); err != nil {
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
		`SELECT id, title, runtime, skills, status, COALESCE(pid,0), COALESCE(worktree,''), heartbeat, COALESCE(hook,'[]'), COALESCE(recovery_attempts,0), created_at
		 FROM ministers WHERE status = 'idle'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ministers []*Minister
	for rows.Next() {
		var m Minister
		if err := rows.Scan(&m.ID, &m.Title, &m.Runtime, &m.Skills, &m.Status, &m.Pid, &m.Worktree, &m.Heartbeat, &m.Hook, &m.RecoveryAttempts, &m.CreatedAt); err != nil {
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
		`SELECT id, title, topology, COALESCE(config,''), COALESCE(project,''), COALESCE(projects,'[]'), COALESCE(ack_mode,'auto'), status, created_at
		 FROM sessions WHERE status = 'active'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.Title, &s.Topology, &s.Config, &s.Project, &s.Projects, &s.AckMode, &s.Status, &s.CreatedAt); err != nil {
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

// WhipStats aggregates statistics for the Whip report.
type WhipStats struct {
	ByElectionCount int
	AvgDurationS    float64
	StuckMinisters  []*Minister
}

// GetWhipStats returns aggregate statistics used in the Whip report.
func (db *DB) GetWhipStats() (*WhipStats, error) {
	stats := &WhipStats{}

	// Count by-elections: Hansard entries written by the Whip (補選触発).
	// Scan failures default to 0, which is the correct zero-value signal.
	if err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM hansard WHERE notes LIKE '%补选触发%'`,
	).Scan(&stats.ByElectionCount); err != nil {
		slog.Warn("GetWhipStats: by-election count scan", "err", err)
	}

	// Average completion time across enacted bills with measured duration.
	if err := db.conn.QueryRow(
		`SELECT COALESCE(AVG(duration_s), 0) FROM hansard WHERE outcome = 'enacted' AND duration_s > 0`,
	).Scan(&stats.AvgDurationS); err != nil {
		slog.Warn("GetWhipStats: avg duration scan", "err", err)
	}

	// Ministers currently stuck.
	stuck, err := db.ListMinistersWithStatus("stuck")
	if err != nil {
		return nil, err
	}
	stats.StuckMinisters = stuck

	return stats, nil
}

// ListRecentHansard returns the most recent N hansard entries, newest first.
func (db *DB) ListRecentHansard(limit int) ([]*Hansard, error) {
	rows, err := db.conn.Query(
		`SELECT id, minister_id, bill_id, COALESCE(outcome,''), COALESCE(duration_s,0), COALESCE(skills_used,''), COALESCE(quality,0), COALESCE(notes,''), created_at
		 FROM hansard ORDER BY created_at DESC LIMIT ?`, limit,
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

// EnactBillFromDone enacts a bill via the done-file protocol.
// It updates bill status to "enacted", records a Hansard entry (outcome: enacted),
// computes a quality score, and creates a completion Gazette.
// Called by the Whip pollDoneFiles loop.
func (db *DB) EnactBillFromDone(billID, ministerID, summary, payloadJSON string) error {
	if err := db.UpdateBillStatus(billID, "enacted"); err != nil {
		return fmt.Errorf("update bill status: %w", err)
	}

	notes := "done 文件检测，由 Whip 自动记录"
	quality := ComputeBillQuality("enacted", notes)

	h := &Hansard{
		ID:         fmt.Sprintf("hansard-%x", time.Now().UnixNano()),
		MinisterID: ministerID,
		BillID:     billID,
		Outcome:    NullString("enacted"),
		Quality:    quality,
		Notes:      NullString(notes),
	}
	if err := db.CreateHansard(h); err != nil {
		return fmt.Errorf("create hansard: %w", err)
	}

	g := &Gazette{
		ID:           fmt.Sprintf("gazette-%x", time.Now().UnixNano()),
		FromMinister: NullString(ministerID),
		BillID:       NullString(billID),
		Type:         NullString(GazetteCompletion),
		Summary:      fmt.Sprintf("完成公报：议案 [%s] 已由部长 [%s] 完成并 enacted。\n\n%s", billID, ministerID, summary),
		Payload:      payloadJSON,
	}
	return db.CreateGazette(g)
}

// ListSubBills returns all bills whose parent_bill matches the given ID.
func (db *DB) ListSubBills(parentID string) ([]*Bill, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at
		 FROM bills WHERE parent_bill = ?`, parentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, &b)
	}
	return bills, nil
}

// ListBillsForCommittee returns all bills currently in committee review stage.
func (db *DB) ListBillsForCommittee() ([]*Bill, error) {
	rows, err := db.conn.Query(
		`SELECT id, COALESCE(session_id,''), title, COALESCE(description,''), status, COALESCE(assignee,''), COALESCE(depends_on,''), COALESCE(branch,''), COALESCE(portfolio,''), COALESCE(project,''), COALESCE(parent_bill,''), created_at, updated_at
		 FROM bills WHERE status = 'committee'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bills []*Bill
	for rows.Next() {
		var b Bill
		if err := rows.Scan(&b.ID, &b.SessionID, &b.Title, &b.Description, &b.Status, &b.Assignee, &b.DependsOn, &b.Branch, &b.Portfolio, &b.Project, &b.ParentBill, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		bills = append(bills, &b)
	}
	return bills, nil
}

// UnassignBill removes the assignee from a bill without changing its status.
// Use this (instead of ClearBillAssignment) when you want to preserve the bill status.
func (db *DB) UnassignBill(billID string) error {
	_, err := db.conn.Exec(
		`UPDATE bills SET assignee = NULL, updated_at = ? WHERE id = ?`,
		time.Now(), billID,
	)
	return err
}

// ListByElectionHansard returns recent hansard entries that were created as part of
// a by-election (补选) process. These are identified by notes containing "补选".
func (db *DB) ListByElectionHansard(limit int) ([]*Hansard, error) {
	rows, err := db.conn.Query(
		`SELECT id, minister_id, bill_id, COALESCE(outcome,''), COALESCE(duration_s,0), COALESCE(skills_used,''), COALESCE(quality,0), COALESCE(notes,''), created_at
		 FROM hansard WHERE notes LIKE '%补选%' ORDER BY created_at DESC LIMIT ?`, limit,
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

// ─── Minister Hook Queue (Phase 3B) ──────────────────────────────────────────

// PushHook appends a bill ID to the minister's hook queue (FIFO).
// Duplicate bill IDs are silently ignored.
func (db *DB) PushHook(ministerID, billID string) error {
	m, err := db.GetMinister(ministerID)
	if err != nil {
		return fmt.Errorf("get minister: %w", err)
	}

	var queue []string
	if m.Hook != "" && m.Hook != "[]" {
		if err := json.Unmarshal([]byte(m.Hook), &queue); err != nil {
			queue = nil
		}
	}

	// Deduplicate.
	for _, id := range queue {
		if id == billID {
			return nil // Already queued.
		}
	}

	queue = append(queue, billID)
	data, err := json.Marshal(queue)
	if err != nil {
		return fmt.Errorf("marshal hook: %w", err)
	}

	_, err = db.conn.Exec(`UPDATE ministers SET hook = ? WHERE id = ?`, string(data), ministerID)
	return err
}

// PopHook removes and returns the oldest bill ID from the minister's hook queue.
// Returns ("", nil) when the queue is empty.
func (db *DB) PopHook(ministerID string) (string, error) {
	m, err := db.GetMinister(ministerID)
	if err != nil {
		return "", fmt.Errorf("get minister: %w", err)
	}

	var queue []string
	if m.Hook != "" && m.Hook != "[]" {
		if err := json.Unmarshal([]byte(m.Hook), &queue); err != nil {
			return "", nil
		}
	}

	if len(queue) == 0 {
		return "", nil
	}

	billID := queue[0]
	queue = queue[1:]

	data, err := json.Marshal(queue)
	if err != nil {
		return "", fmt.Errorf("marshal hook: %w", err)
	}

	_, err = db.conn.Exec(`UPDATE ministers SET hook = ? WHERE id = ?`, string(data), ministerID)
	if err != nil {
		return "", err
	}

	return billID, nil
}

// PeekHook returns all bill IDs in the minister's hook queue without removing them.
func (db *DB) PeekHook(ministerID string) ([]string, error) {
	m, err := db.GetMinister(ministerID)
	if err != nil {
		return nil, fmt.Errorf("get minister: %w", err)
	}

	if m.Hook == "" || m.Hook == "[]" {
		return nil, nil
	}

	var queue []string
	if err := json.Unmarshal([]byte(m.Hook), &queue); err != nil {
		return nil, nil
	}
	return queue, nil
}

// FindLeastLoadedMinister returns the idle minister with the fewest assigned bills
// that matches the given portfolio skill. Phase 3E — 负载均衡分配.
func (db *DB) FindLeastLoadedMinister(skill string) (*Minister, error) {
	candidates, err := db.ListIdleMinistersForSkill(skill)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}

	// Count active bills per minister and pick the one with fewest.
	minLoad := -1
	var best *Minister
	for _, m := range candidates {
		bills, err := db.GetBillsByAssignee(m.ID)
		if err != nil {
			continue
		}
		// Count non-terminal bills.
		load := 0
		for _, b := range bills {
			if b.Status != "enacted" && b.Status != "royal_assent" && b.Status != "failed" {
				load++
			}
		}
		if minLoad < 0 || load < minLoad {
			minLoad = load
			best = m
		}
	}
	return best, nil
}

// ─── Phase 4A: Hansard Quality Scoring ───────────────────────────────────────

// UpdateHansardQuality sets the quality score on a hansard entry.
func (db *DB) UpdateHansardQuality(id string, quality float64) error {
	_, err := db.conn.Exec(`UPDATE hansard SET quality = ? WHERE id = ?`, quality, id)
	return err
}

// GetMinisterAvgQuality returns the average quality score for a minister across
// all hansard entries with quality > 0. Returns 0.5 (neutral) if no data.
func (db *DB) GetMinisterAvgQuality(ministerID string) (float64, error) {
	var avg sql.NullFloat64
	err := db.conn.QueryRow(
		`SELECT AVG(quality) FROM hansard WHERE minister_id = ? AND quality > 0`, ministerID,
	).Scan(&avg)
	if err != nil {
		return 0.5, err
	}
	if !avg.Valid || avg.Float64 == 0 {
		return 0.5, nil // No data — assume neutral.
	}
	return avg.Float64, nil
}

// GetMinisterAvgQualityForSkill returns the avg quality for a minister filtered
// by bills with a specific portfolio skill (via joined bills table).
// Falls back to overall avg if no skill-specific data exists.
func (db *DB) GetMinisterAvgQualityForSkill(ministerID, skill string) (float64, error) {
	if skill == "" {
		return db.GetMinisterAvgQuality(ministerID)
	}
	var avg sql.NullFloat64
	err := db.conn.QueryRow(
		`SELECT AVG(h.quality)
		 FROM hansard h
		 JOIN bills b ON h.bill_id = b.id
		 WHERE h.minister_id = ? AND b.portfolio = ? AND h.quality > 0`,
		ministerID, skill,
	).Scan(&avg)
	if err != nil || !avg.Valid || avg.Float64 == 0 {
		// Fallback to overall avg.
		return db.GetMinisterAvgQuality(ministerID)
	}
	return avg.Float64, nil
}

// ComputeBillQuality calculates a quality score [0.0, 1.0] for a completed bill.
//
// Formula:
//   - outcome: enacted=0.8, partial=0.4, failed=0.0 (base)
//   - committee_pass: +0.15 bonus if notes contain "PASS"
//   - no by-election: +0.05 bonus if notes do NOT contain "补选"
//
// The result is clamped to [0.0, 1.0].
func ComputeBillQuality(outcome, notes string) float64 {
	var score float64
	switch outcome {
	case "enacted":
		score = 0.80
	case "partial":
		score = 0.40
	default:
		score = 0.0
	}
	// Bonus: committee pass.
	if strings.Contains(strings.ToUpper(notes), "PASS") {
		score += 0.15
	}
	// Bonus: no by-election disruption.
	if !strings.Contains(notes, "补选") {
		score += 0.05
	}
	if score > 1.0 {
		score = 1.0
	}
	return score
}

// FindBestMinisterForSkill returns the idle minister with the best combined score
// of historical quality × (1 / load+1) for the given skill.
// Falls back to FindLeastLoadedMinister behaviour when quality data is absent.
func (db *DB) FindBestMinisterForSkill(skill string) (*Minister, error) {
	candidates, err := db.ListIdleMinistersForSkill(skill)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	type scored struct {
		m     *Minister
		score float64
	}
	var ranked []scored
	for _, m := range candidates {
		quality, _ := db.GetMinisterAvgQualityForSkill(m.ID, skill)
		bills, _ := db.GetBillsByAssignee(m.ID)
		load := 0
		for _, b := range bills {
			if b.Status != "enacted" && b.Status != "royal_assent" && b.Status != "failed" {
				load++
			}
		}
		// Score = quality × (1 + firstACKRate × 0.2) / (load + 1).
		// Ministers with higher first-ACK rate get up to 20% bonus.
		firstACKRate, _ := db.GetMinisterFirstACKRate(m.ID)
		score := quality * (1.0 + firstACKRate*0.2) / float64(load+1)
		ranked = append(ranked, scored{m, score})
	}

	best := ranked[0]
	for _, s := range ranked[1:] {
		if s.score > best.score {
			best = s
		}
	}
	return best.m, nil
}

// ─── Phase 5A: Session Statistics ────────────────────────────────────────────

// MinisterLoad holds per-minister statistics within a session.
type MinisterLoad struct {
	ID      string
	Title   string
	Bills   int     // total bills assigned in this session
	Enacted int     // enacted bills in this session
	AvgQ    float64 // average quality for bills in this session
}

// SessionStats holds comprehensive statistics for a single session.
type SessionStats struct {
	SessionID   string
	Title       string
	Topology    string
	Status      string
	TotalBills  int
	ByStatus    map[string]int // draft/reading/committee/enacted/royal_assent/failed
	EnactedRate float64        // enacted / total
	AvgQuality  float64        // average quality score across enacted bills
	TotalDurS   int            // sum of hansard.duration_s
	Ministers   []MinisterLoad // per-minister stats in this session
}

// GetSessionStats returns comprehensive statistics for a single session.
func (db *DB) GetSessionStats(sessionID string) (*SessionStats, error) {
	s, err := db.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	stats := &SessionStats{
		SessionID: s.ID,
		Title:     s.Title,
		Topology:  s.Topology,
		Status:    s.Status,
		ByStatus:  make(map[string]int),
	}

	bills, err := db.ListBillsBySession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("list bills: %w", err)
	}
	stats.TotalBills = len(bills)
	for _, b := range bills {
		stats.ByStatus[b.Status]++
	}
	if stats.TotalBills > 0 {
		enacted := stats.ByStatus["enacted"] + stats.ByStatus["royal_assent"]
		stats.EnactedRate = float64(enacted) / float64(stats.TotalBills)
	}

	// Aggregate hansard quality + duration for this session's bills.
	if len(bills) > 0 {
		billIDs := make([]interface{}, len(bills))
		for i, b := range bills {
			billIDs[i] = b.ID
		}
		ph := strings.Repeat("?,", len(billIDs))
		ph = ph[:len(ph)-1]

		var avgQuality sql.NullFloat64
		var totalDur int
		row := db.conn.QueryRow(
			`SELECT AVG(quality), COALESCE(SUM(duration_s),0) FROM hansard WHERE bill_id IN (`+ph+`) AND quality > 0`,
			billIDs...,
		)
		if err := row.Scan(&avgQuality, &totalDur); err == nil {
			if avgQuality.Valid {
				stats.AvgQuality = avgQuality.Float64
			}
			stats.TotalDurS = totalDur
		}
	}

	// Per-minister stats within this session.
	ministerBills := make(map[string][]*Bill)
	for _, b := range bills {
		if b.Assignee.String == "" {
			continue
		}
		ministerBills[b.Assignee.String] = append(ministerBills[b.Assignee.String], b)
	}

	for mID, mBills := range ministerBills {
		load := MinisterLoad{
			ID:    mID,
			Bills: len(mBills),
		}
		// Fetch minister title (best-effort).
		if m, err := db.GetMinister(mID); err == nil {
			load.Title = m.Title
		} else {
			load.Title = mID
		}

		var billIDsForM []interface{}
		for _, b := range mBills {
			if b.Status == "enacted" || b.Status == "royal_assent" {
				load.Enacted++
			}
			billIDsForM = append(billIDsForM, b.ID)
		}

		// Avg quality for this minister in this session.
		if len(billIDsForM) > 0 {
			ph := strings.Repeat("?,", len(billIDsForM))
			ph = ph[:len(ph)-1]
			args := append([]interface{}{mID}, billIDsForM...)
			var avgQ sql.NullFloat64
			row := db.conn.QueryRow(
				`SELECT AVG(quality) FROM hansard WHERE minister_id = ? AND bill_id IN (`+ph+`) AND quality > 0`,
				args...,
			)
			if err := row.Scan(&avgQ); err == nil && avgQ.Valid {
				load.AvgQ = avgQ.Float64
			}
		}

		stats.Ministers = append(stats.Ministers, load)
	}

	return stats, nil
}

// GetAllSessionStats returns statistics for all sessions.
func (db *DB) GetAllSessionStats() ([]*SessionStats, error) {
	sessions, err := db.ListSessions()
	if err != nil {
		return nil, err
	}

	var result []*SessionStats
	for _, s := range sessions {
		stats, err := db.GetSessionStats(s.ID)
		if err != nil {
			continue
		}
		result = append(result, stats)
	}
	return result, nil
}

// DB returns the underlying database connection (for migrations, etc.)
func (db *DB) DB() *sql.DB {
	return db.conn
}

// Ping checks database connectivity.
func (db *DB) Ping(ctx context.Context) error {
	return db.conn.PingContext(ctx)
}

// ─── D-1: Event Ledger ───────────────────────────────────────────────────────

// Event represents a structured event in the Event Ledger.
type Event struct {
	ID         string
	Timestamp  time.Time
	Topic      string
	BillID     sql.NullString
	MinisterID sql.NullString
	SessionID  sql.NullString
	Source     string
	Payload    string
}

// RecordEvent writes a structured event to the events table.
func (db *DB) RecordEvent(topic, source, billID, ministerID, sessionID, payload string) error {
	id := newEventID()
	_, err := db.conn.Exec(
		`INSERT INTO events (id, topic, bill_id, minister_id, session_id, source, payload) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, topic, NullString(billID), NullString(ministerID), NullString(sessionID), source, payload,
	)
	return err
}

// newEventID returns a unique event ID combining nanosecond timestamp with random
// bytes — nanosecond precision alone collides under concurrent callers.
func newEventID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("evt-%x-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// ListEvents returns events matching optional filters, newest first.
func (db *DB) ListEvents(topic, billID, ministerID string, since time.Duration) ([]*Event, error) {
	query := `SELECT id, timestamp, topic, COALESCE(bill_id,''), COALESCE(minister_id,''), COALESCE(session_id,''), source, COALESCE(payload,'{}') FROM events WHERE 1=1`
	var args []interface{}

	if topic != "" {
		query += ` AND topic = ?`
		args = append(args, topic)
	}
	if billID != "" {
		query += ` AND bill_id = ?`
		args = append(args, billID)
	}
	if ministerID != "" {
		query += ` AND minister_id = ?`
		args = append(args, ministerID)
	}
	if since > 0 {
		query += ` AND timestamp > ?`
		args = append(args, time.Now().Add(-since))
	}

	query += ` ORDER BY timestamp DESC`

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Topic, &e.BillID, &e.MinisterID, &e.SessionID, &e.Source, &e.Payload); err != nil {
			return nil, err
		}
		events = append(events, &e)
	}
	return events, nil
}

// ListEventsBySession returns events for a specific session, oldest first (for timeline).
func (db *DB) ListEventsBySession(sessionID string) ([]*Event, error) {
	rows, err := db.conn.Query(
		`SELECT id, timestamp, topic, COALESCE(bill_id,''), COALESCE(minister_id,''), COALESCE(session_id,''), source, COALESCE(payload,'{}') FROM events WHERE session_id = ? ORDER BY timestamp ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Topic, &e.BillID, &e.MinisterID, &e.SessionID, &e.Source, &e.Payload); err != nil {
			return nil, err
		}
		events = append(events, &e)
	}
	return events, nil
}

// ─── Phase 4: C-3 Hansard Timeline ──────────────────────────────────────────

// ListHansardBySession returns hansard entries for bills in a session, oldest first.
func (db *DB) ListHansardBySession(sessionID string) ([]*Hansard, error) {
	rows, err := db.conn.Query(
		`SELECT h.id, h.minister_id, h.bill_id, COALESCE(h.outcome,''), COALESCE(h.duration_s,0), COALESCE(h.skills_used,''), COALESCE(h.quality,0), COALESCE(h.notes,''), h.created_at
		 FROM hansard h JOIN bills b ON h.bill_id = b.id
		 WHERE b.session_id = ? ORDER BY h.created_at ASC`, sessionID,
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

// ─── Phase 4: B-1.4 Question Time Metrics ──────────────────────────────────

// CountACKRoundsForBill counts the number of question-type gazettes for a bill.
func (db *DB) CountACKRoundsForBill(billID string) (int, error) {
	var count int
	err := db.conn.QueryRow(
		`SELECT COUNT(*) FROM gazettes WHERE bill_id = ? AND type = 'question'`, billID,
	).Scan(&count)
	return count, err
}

// FirstACKRate returns the ratio of bills in a session that had zero question rounds.
func (db *DB) FirstACKRate(sessionID string) (float64, error) {
	rows, err := db.conn.Query(
		`SELECT b.id FROM bills b WHERE b.session_id = ? AND (b.status = 'enacted' OR b.status = 'royal_assent')`,
		sessionID,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	zeroRounds := 0
	for rows.Next() {
		var billID string
		if err := rows.Scan(&billID); err != nil {
			return 0, err
		}
		total++
		count, _ := db.CountACKRoundsForBill(billID)
		if count == 0 {
			zeroRounds++
		}
	}
	if total == 0 {
		return 0, nil
	}
	return float64(zeroRounds) / float64(total), nil
}

// GetMinisterFirstACKRate returns the first-ACK rate (zero question rounds) for a minister.
func (db *DB) GetMinisterFirstACKRate(ministerID string) (float64, error) {
	rows, err := db.conn.Query(
		`SELECT h.bill_id FROM hansard h WHERE h.minister_id = ? AND h.outcome = 'enacted'`,
		ministerID,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	total := 0
	zeroRounds := 0
	for rows.Next() {
		var billID string
		if err := rows.Scan(&billID); err != nil {
			return 0, err
		}
		total++
		count, _ := db.CountACKRoundsForBill(billID)
		if count == 0 {
			zeroRounds++
		}
	}
	if total == 0 {
		return 0, nil
	}
	return float64(zeroRounds) / float64(total), nil
}

// UpdateHansardMetrics updates ACK rounds and briefing time on a hansard record.
func (db *DB) UpdateHansardMetrics(hansardID string, ackRounds, briefingTimeS int) error {
	_, err := db.conn.Exec(
		`UPDATE hansard SET ack_rounds = ?, briefing_time_s = ? WHERE id = ?`,
		ackRounds, briefingTimeS, hansardID,
	)
	return err
}

// ClearMinisterWorktree clears the worktree field for a minister.
func (db *DB) ClearMinisterWorktree(id string) error {
	_, err := db.conn.Exec(`UPDATE ministers SET worktree = '' WHERE id = ?`, id)
	return err
}
