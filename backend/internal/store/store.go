// Package store persists users, sessions, documents, notes and questions in
// SQLite (local) or PostgreSQL. The schema matches the earlier Python version,
// so an existing atlas.db keeps working.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")
var ErrConflict = errors.New("conflict")

type Store struct {
	db       *sql.DB
	postgres bool
}

type User struct {
	ID           string  `json:"id"`
	Login        string  `json:"login"`
	Name         string  `json:"name"`
	PasswordHash string  `json:"-"`
	Language     string  `json:"language"`
	Private      bool    `json:"private"`
	Avatar       *string `json:"avatar"`
}

type Document struct {
	ID        string
	UserID    string
	Title     string
	Author    string
	Year      *int
	Genre     string
	CreatedAt string
	Content   string
	Analysis  string
	// Background analysis state: processing, ready or error.
	Status   string
	Progress int
	Stage    string
	Note     string
	Error    string
	Depth    string
	Source   string // txt, pdf, epub or text (pasted)
	Subgenre string
	AIModel  string // local model requested for refinement, empty when off
}

type Note struct {
	ID         string  `json:"id"`
	DocumentID string  `json:"-"`
	EntityID   *string `json:"entity_id"`
	Fragment   *string `json:"fragment"`
	Text       string  `json:"text"`
	CreatedAt  string  `json:"created_at"`
}

type Question struct {
	ID         string `json:"id"`
	DocumentID string `json:"-"`
	Question   string `json:"question"`
	Answer     string `json:"answer"`
	Sources    string `json:"-"`
	Mode       string `json:"mode"`
	CreatedAt  string `json:"created_at"`
}

// Now returns an ISO-8601 UTC timestamp like Python's datetime.isoformat().
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000-07:00") }

// Open connects using DATABASE_URL semantics: sqlite:///path or postgresql://...
func Open(databaseURL, dataDir string) (*Store, error) {
	if databaseURL == "" {
		databaseURL = "sqlite:///" + filepath.Join(dataDir, "atlas.db")
	}
	var (
		db       *sql.DB
		err      error
		postgres bool
	)
	switch {
	case strings.HasPrefix(databaseURL, "sqlite:///"):
		file := strings.TrimPrefix(databaseURL, "sqlite:///")
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return nil, err
		}
		db, err = sql.Open("sqlite", "file:"+file+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	case strings.HasPrefix(databaseURL, "postgres"):
		// Accept SQLAlchemy-style driver suffixes such as postgresql+psycopg://.
		if i := strings.Index(databaseURL, "://"); i > 0 {
			databaseURL = "postgres" + databaseURL[i:]
		}
		db, err = sql.Open("pgx", databaseURL)
		postgres = true
	default:
		return nil, fmt.Errorf("unsupported DATABASE_URL: use sqlite:///path or postgresql://")
	}
	if err != nil {
		return nil, err
	}
	if !postgres {
		db.SetMaxOpenConns(1) // SQLite writes are serialised; one connection avoids lock errors.
	}
	s := &Store{db: db, postgres: postgres}
	return s, s.migrate()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error {
	var one int
	return s.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
}

// q rewrites ? placeholders to $n for PostgreSQL.
func (s *Store) q(query string) string {
	if !s.postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (s *Store) migrate() error {
	jsonType := "TEXT"
	if s.postgres {
		jsonType = "JSON"
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (id VARCHAR(36) PRIMARY KEY, login VARCHAR(80) NOT NULL UNIQUE, name VARCHAR(100) NOT NULL,
			password_hash TEXT NOT NULL, language VARCHAR(5) NOT NULL DEFAULT 'ru', private BOOLEAN NOT NULL DEFAULT TRUE, avatar TEXT)`,
		`CREATE TABLE IF NOT EXISTS sessions (id VARCHAR(36) PRIMARY KEY, user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS ix_sessions_user_id ON sessions (user_id)`,
		`CREATE TABLE IF NOT EXISTS documents (id VARCHAR(36) PRIMARY KEY, user_id VARCHAR(36) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title VARCHAR(250) NOT NULL, author VARCHAR(180) NOT NULL DEFAULT '', year INTEGER, genre VARCHAR(80) NOT NULL DEFAULT 'Художественный',
			created_at VARCHAR(40) NOT NULL, content TEXT NOT NULL, analysis ` + jsonType + ` NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS ix_documents_user_id ON documents (user_id)`,
		`CREATE TABLE IF NOT EXISTS notes (id VARCHAR(36) PRIMARY KEY, document_id VARCHAR(36) NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			entity_id VARCHAR(50), fragment TEXT, text TEXT NOT NULL, created_at VARCHAR(40) NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS ix_notes_document_id ON notes (document_id)`,
		`CREATE TABLE IF NOT EXISTS questions (id VARCHAR(36) PRIMARY KEY, document_id VARCHAR(36) NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			question TEXT NOT NULL, answer TEXT NOT NULL, sources ` + jsonType + ` NOT NULL, mode VARCHAR(40) NOT NULL DEFAULT 'extractive', created_at VARCHAR(40) NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS ix_questions_document_id ON questions (document_id)`,
	}
	// Columns added after the first release; older databases get them with defaults.
	added := []string{
		`status VARCHAR(20) NOT NULL DEFAULT 'ready'`,
		`progress INTEGER NOT NULL DEFAULT 100`,
		`stage VARCHAR(20) NOT NULL DEFAULT 'done'`,
		`note TEXT NOT NULL DEFAULT ''`,
		`error TEXT NOT NULL DEFAULT ''`,
		`depth VARCHAR(10) NOT NULL DEFAULT 'full'`,
		`source VARCHAR(10) NOT NULL DEFAULT 'text'`,
		`subgenre VARCHAR(80) NOT NULL DEFAULT ''`,
		`ai_model VARCHAR(80) NOT NULL DEFAULT ''`,
	}
	for _, st := range statements {
		if _, err := s.db.Exec(st); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	for _, column := range added {
		st := "ALTER TABLE documents ADD COLUMN " + column
		if s.postgres {
			st = "ALTER TABLE documents ADD COLUMN IF NOT EXISTS " + column
		}
		if _, err := s.db.Exec(st); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func isUnique(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

func (s *Store) CreateUser(ctx context.Context, u *User) error {
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO users (id, login, name, password_hash, language, private, avatar) VALUES (?, ?, ?, ?, ?, ?, ?)`),
		u.ID, u.Login, u.Name, u.PasswordHash, u.Language, u.Private, u.Avatar)
	if err != nil && isUnique(err) {
		return ErrConflict
	}
	return err
}

func (s *Store) userBy(ctx context.Context, column, value string) (*User, error) {
	u := &User{}
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, login, name, password_hash, language, private, avatar FROM users WHERE `+column+` = ?`), value).
		Scan(&u.ID, &u.Login, &u.Name, &u.PasswordHash, &u.Language, &u.Private, &u.Avatar)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByID(ctx context.Context, id string) (*User, error) {
	return s.userBy(ctx, "id", id)
}
func (s *Store) UserByLogin(ctx context.Context, login string) (*User, error) {
	return s.userBy(ctx, "login", login)
}

func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE users SET name = ?, language = ?, private = ?, avatar = ? WHERE id = ?`),
		u.Name, u.Language, u.Private, u.Avatar, u.ID)
	return err
}

func (s *Store) CreateSession(ctx context.Context, id, userID string, expires int64) error {
	if _, err := s.db.ExecContext(ctx, s.q(`DELETE FROM sessions WHERE expires_at < ?`), time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)`), id, userID, expires)
	return err
}

func (s *Store) Session(ctx context.Context, id string) (userID string, expires int64, err error) {
	err = s.db.QueryRowContext(ctx, s.q(`SELECT user_id, expires_at FROM sessions WHERE id = ?`), id).Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM sessions WHERE id = ?`), id)
	return err
}

const documentColumns = `id, user_id, title, author, year, genre, created_at, content, analysis, status, progress, stage, note, error, depth, source, subgenre, ai_model`

func scanDocument(row interface{ Scan(...any) error }) (*Document, error) {
	d := &Document{}
	var year sql.NullInt64
	err := row.Scan(&d.ID, &d.UserID, &d.Title, &d.Author, &year, &d.Genre, &d.CreatedAt, &d.Content, &d.Analysis,
		&d.Status, &d.Progress, &d.Stage, &d.Note, &d.Error, &d.Depth, &d.Source, &d.Subgenre, &d.AIModel)
	if year.Valid {
		y := int(year.Int64)
		d.Year = &y
	}
	return d, err
}

func (s *Store) CreateDocument(ctx context.Context, d *Document) error {
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO documents (`+documentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		d.ID, d.UserID, d.Title, d.Author, d.Year, d.Genre, d.CreatedAt, d.Content, d.Analysis,
		d.Status, d.Progress, d.Stage, d.Note, d.Error, d.Depth, d.Source, d.Subgenre, d.AIModel)
	return err
}

func (s *Store) Documents(ctx context.Context, userID string) ([]*Document, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT `+documentColumns+` FROM documents WHERE user_id = ?`), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Document
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Document(ctx context.Context, id string) (*Document, error) {
	d, err := scanDocument(s.db.QueryRowContext(ctx, s.q(`SELECT `+documentColumns+` FROM documents WHERE id = ?`), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *Store) UpdateDocument(ctx context.Context, d *Document) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE documents SET title = ?, author = ?, year = ?, genre = ?, subgenre = ? WHERE id = ?`),
		d.Title, d.Author, d.Year, d.Genre, d.Subgenre, d.ID)
	return err
}

func (s *Store) DeleteDocument(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, st := range []string{`DELETE FROM notes WHERE document_id = ?`, `DELETE FROM questions WHERE document_id = ?`, `DELETE FROM documents WHERE id = ?`} {
		if _, err := tx.ExecContext(ctx, s.q(st), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Notes(ctx context.Context, documentID string) ([]*Note, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, document_id, entity_id, fragment, text, created_at FROM notes WHERE document_id = ? ORDER BY created_at DESC`), documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Note{}
	for rows.Next() {
		n := &Note{}
		if err := rows.Scan(&n.ID, &n.DocumentID, &n.EntityID, &n.Fragment, &n.Text, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) Note(ctx context.Context, id string) (*Note, error) {
	n := &Note{}
	err := s.db.QueryRowContext(ctx, s.q(`SELECT id, document_id, entity_id, fragment, text, created_at FROM notes WHERE id = ?`), id).
		Scan(&n.ID, &n.DocumentID, &n.EntityID, &n.Fragment, &n.Text, &n.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) CreateNote(ctx context.Context, n *Note) error {
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO notes (id, document_id, entity_id, fragment, text, created_at) VALUES (?, ?, ?, ?, ?, ?)`),
		n.ID, n.DocumentID, n.EntityID, n.Fragment, n.Text, n.CreatedAt)
	return err
}

func (s *Store) DeleteNote(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.q(`DELETE FROM notes WHERE id = ?`), id)
	return err
}

func (s *Store) Questions(ctx context.Context, documentID string) ([]*Question, error) {
	rows, err := s.db.QueryContext(ctx, s.q(`SELECT id, document_id, question, answer, sources, mode, created_at FROM questions WHERE document_id = ? ORDER BY created_at DESC`), documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Question{}
	for rows.Next() {
		q := &Question{}
		if err := rows.Scan(&q.ID, &q.DocumentID, &q.Question, &q.Answer, &q.Sources, &q.Mode, &q.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) CreateQuestion(ctx context.Context, q *Question) error {
	_, err := s.db.ExecContext(ctx, s.q(`INSERT INTO questions (id, document_id, question, answer, sources, mode, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`),
		q.ID, q.DocumentID, q.Question, q.Answer, q.Sources, q.Mode, q.CreatedAt)
	return err
}

// SetProgress records the current analysis stage of a document.
func (s *Store) SetProgress(ctx context.Context, id string, progress int, stage, note string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE documents SET status = 'processing', progress = ?, stage = ?, note = ?, error = '' WHERE id = ?`),
		progress, stage, note, id)
	return err
}

// StartAnalysis puts a document back into the processing state.
func (s *Store) StartAnalysis(ctx context.Context, id, depth, aiModel string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE documents SET status = 'processing', progress = 5, stage = 'extract', note = '', error = '', depth = ?, ai_model = ? WHERE id = ?`), depth, aiModel, id)
	return err
}

// FinishAnalysis stores the result and marks the document ready.
func (s *Store) FinishAnalysis(ctx context.Context, id, analysis, note string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE documents SET analysis = ?, status = 'ready', progress = 100, stage = 'done', note = ?, error = '' WHERE id = ?`),
		analysis, note, id)
	return err
}

// FailAnalysis marks the document as failed with a message for the user.
func (s *Store) FailAnalysis(ctx context.Context, id, message string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE documents SET status = 'error', error = ? WHERE id = ?`), message, id)
	return err
}

// ProcessingIDs lists documents whose analysis was interrupted, e.g. by a restart.
func (s *Store) ProcessingIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM documents WHERE status = 'processing'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PendingAI lists ready documents that were analysed while the language model was unavailable.
func (s *Store) PendingAI(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM documents WHERE status = 'ready' AND ai_model = 'pending'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// NoteCount tells whether a document has notes (they may point at entity ids).
func (s *Store) NoteCount(ctx context.Context, documentID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.q(`SELECT COUNT(*) FROM notes WHERE document_id = ?`), documentID).Scan(&n)
	return n, err
}

// SetAIModel changes the requested model without touching the analysis.
func (s *Store) SetAIModel(ctx context.Context, id, model string) error {
	_, err := s.db.ExecContext(ctx, s.q(`UPDATE documents SET ai_model = ? WHERE id = ?`), model, id)
	return err
}
