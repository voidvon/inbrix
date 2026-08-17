// Package mailstore owns the durable local mail mirror.
//
// IMAP remains the source of truth. This package stores enough account,
// folder, message-header and optional body state for web requests to avoid a
// network round-trip in the normal path, while retaining the UID information
// required for incremental reconciliation.
package mailstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrNotFound = errors.New("mailstore: not found")
	ErrNotReady = errors.New("mailstore: account has not completed its first sync")
)

type Store struct {
	db   *sql.DB
	path string
	mu   sync.Mutex
}

type User struct {
	ID           string
	Login        string
	DisplayName  string
	PasswordHash string
	CreatedAt    time.Time
}

type Account struct {
	ID                string
	OwnerID           string
	Email             string
	Username          string
	Label             string
	Color             string
	IMAPServer        string
	IMAPPort          int
	IMAPTLS           bool
	SMTPServer        string
	SMTPPort          int
	SMTPStartTLS      bool
	EncryptedPassword string
	AuthType          string
	IsDefault         bool
	LastSyncAt        time.Time
	LastError         string
}

type Folder struct {
	AccountID    string
	Name         string
	Delimiter    string
	Attributes   []string
	UnreadCount  int
	MessageCount int
	SyncComplete bool
	LastSyncAt   time.Time
	LastError    string
}

type SyncState struct {
	AccountID    string
	FolderName   string
	SyncComplete bool
	LastSyncAt   time.Time
	LastError    string
}

// WebhookSettings controls per-user Feishu notifications emitted by the
// background mail synchronizer. The URL is retained while disabled so users
// can turn notifications back on without pasting the bot URL again.
type WebhookSettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

type AIModelRecord struct {
	ID              string
	OwnerID         string
	Provider        string
	BaseURL         string
	Model           string
	ReasoningEffort string
	EncryptedAPIKey string
	IsDefault       bool
	CreatedAt       time.Time
}

type AIAgentRecord struct {
	ID        string
	OwnerID   string
	Name      string
	Prompt    string
	Purpose   string
	CreatedAt time.Time
}

type MessageSummaryKey struct {
	AccountID  string
	FolderName string
	UID        string
}

type MessageSummaryRecord struct {
	MessageSummaryKey
	SummaryType     string
	Summary         string
	Status          string
	SourceHash      string
	ConfigHash      string
	ModelID         string
	ModelName       string
	AgentID         string
	PipelineVersion int
	GenerationToken string
	LeaseUntil      time.Time
	ErrorMessage    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Open creates the database directory, enables WAL mode, and applies the
// schema. A single SQLite file is deliberately used for all users/accounts so
// the process can share one writer and the web handlers can use one pool.
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("mailstore: database path is empty")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("mailstore: create directory: %w", err)
		}
	}

	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("mailstore: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	s := &Store{db: db, path: path}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string { return s.path }

func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			login TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS mail_accounts (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			email TEXT NOT NULL,
			username TEXT NOT NULL,
			label TEXT NOT NULL DEFAULT '',
			color TEXT NOT NULL DEFAULT '',
			imap_server TEXT NOT NULL,
			imap_port INTEGER NOT NULL DEFAULT 993,
			imap_tls INTEGER NOT NULL DEFAULT 1,
			smtp_server TEXT NOT NULL,
			smtp_port INTEGER NOT NULL DEFAULT 587,
			smtp_starttls INTEGER NOT NULL DEFAULT 1,
			encrypted_password TEXT NOT NULL,
			auth_type TEXT NOT NULL DEFAULT 'password',
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			last_sync_at INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			UNIQUE(owner_id, email)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mail_accounts_owner ON mail_accounts(owner_id, is_default DESC, email)`,
		`CREATE TABLE IF NOT EXISTS webhook_settings (
			owner_id TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			url TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ai_model_settings (
			owner_id TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			base_url TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			encrypted_api_key TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS ai_models (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT 'openai',
			base_url TEXT NOT NULL,
			model TEXT NOT NULL,
			reasoning_effort TEXT NOT NULL DEFAULT 'medium',
			encrypted_api_key TEXT NOT NULL,
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(owner_id, provider, base_url, model)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_models_owner ON ai_models(owner_id, is_default DESC, created_at)`,
		`CREATE TABLE IF NOT EXISTS ai_agents (
			id TEXT PRIMARY KEY,
			owner_id TEXT NOT NULL,
			name TEXT NOT NULL,
			prompt TEXT NOT NULL,
			purpose TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(owner_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ai_agents_owner ON ai_agents(owner_id, created_at, name)`,
		`INSERT OR IGNORE INTO ai_models(id, owner_id, provider, base_url, model, encrypted_api_key, is_default, created_at, updated_at)
			SELECT 'legacy_' || owner_id, owner_id, 'openai', base_url, model, encrypted_api_key, 1, updated_at, updated_at
			FROM ai_model_settings WHERE model <> '' AND encrypted_api_key <> ''`,
		`DELETE FROM ai_model_settings WHERE model <> '' AND encrypted_api_key <> ''`,
		`CREATE TABLE IF NOT EXISTS folders (
			account_id TEXT NOT NULL,
			name TEXT NOT NULL,
			delimiter TEXT NOT NULL DEFAULT '',
			attributes_json TEXT NOT NULL DEFAULT '[]',
			unread_count INTEGER NOT NULL DEFAULT 0,
			message_count INTEGER NOT NULL DEFAULT 0,
			sync_complete INTEGER NOT NULL DEFAULT 0,
			last_sync_at INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(account_id, name),
			FOREIGN KEY(account_id) REFERENCES mail_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			account_id TEXT NOT NULL,
			folder_name TEXT NOT NULL,
			uid INTEGER NOT NULL,
			date_unix INTEGER NOT NULL DEFAULT 0,
			from_addr TEXT NOT NULL DEFAULT '',
			from_name TEXT NOT NULL DEFAULT '',
			to_addrs TEXT NOT NULL DEFAULT '',
			to_names_json TEXT NOT NULL DEFAULT '[]',
			cc TEXT NOT NULL DEFAULT '',
			subject TEXT NOT NULL DEFAULT '',
			preview TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			html TEXT NOT NULL DEFAULT '',
			flags_json TEXT NOT NULL DEFAULT '[]',
			attachments_json TEXT NOT NULL DEFAULT '[]',
			message_id TEXT NOT NULL DEFAULT '',
			in_reply_to TEXT NOT NULL DEFAULT '',
			references_json TEXT NOT NULL DEFAULT '[]',
			has_attachments INTEGER NOT NULL DEFAULT 0,
			body_cached INTEGER NOT NULL DEFAULT 0,
			attachment_metadata_cached INTEGER NOT NULL DEFAULT 0,
			auth_json TEXT NOT NULL DEFAULT '',
			unsubscribe_json TEXT NOT NULL DEFAULT '',
			invite_json TEXT NOT NULL DEFAULT '',
			brand_json TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(account_id, folder_name, uid),
			FOREIGN KEY(account_id) REFERENCES mail_accounts(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_list ON messages(account_id, folder_name, date_unix DESC, uid DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_subject ON messages(account_id, folder_name, subject)`,
		`CREATE TABLE IF NOT EXISTS message_summaries (
			account_id TEXT NOT NULL,
			folder_name TEXT NOT NULL,
			uid INTEGER NOT NULL,
			summary_type TEXT NOT NULL DEFAULT 'mail_summary',
			summary_text TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'generating',
			source_hash TEXT NOT NULL DEFAULT '',
			config_hash TEXT NOT NULL DEFAULT '',
			model_id TEXT NOT NULL DEFAULT '',
			model_name TEXT NOT NULL DEFAULT '',
			agent_id TEXT NOT NULL DEFAULT '',
			pipeline_version INTEGER NOT NULL DEFAULT 1,
			generation_token TEXT NOT NULL DEFAULT '',
			lease_until INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(account_id, folder_name, uid, summary_type),
			FOREIGN KEY(account_id, folder_name, uid) REFERENCES messages(account_id, folder_name, uid) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_message_summaries_account ON message_summaries(account_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS conversation_notes (
			account_id TEXT NOT NULL,
			conversation_id TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(account_id, conversation_id),
			FOREIGN KEY(account_id) REFERENCES mail_accounts(id) ON DELETE CASCADE
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("mailstore: migrate: %w", err)
		}
	}
	if err := s.ensureAIAgentPurposeColumn(ctx); err != nil {
		return err
	}
	// The messages table predates attachment_metadata_cached. CREATE TABLE IF
	// NOT EXISTS cannot alter that existing table, so add the column lazily for
	// users upgrading an existing mirror database.
	if err := s.ensureMessageAttachmentMetadataColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureAIModelReasoningEffortColumn(ctx); err != nil {
		return err
	}
	return nil
}

func (s *Store) ListConversationNotes(ctx context.Context, accountID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT conversation_id, note FROM conversation_notes WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list conversation notes: %w", err)
	}
	defer rows.Close()
	notes := make(map[string]string)
	for rows.Next() {
		var conversationID, note string
		if err := rows.Scan(&conversationID, &note); err != nil {
			return nil, fmt.Errorf("mailstore: scan conversation note: %w", err)
		}
		notes[conversationID] = note
	}
	return notes, rows.Err()
}

func (s *Store) SetConversationNote(ctx context.Context, accountID, conversationID, note string) error {
	accountID = strings.TrimSpace(accountID)
	conversationID = strings.TrimSpace(conversationID)
	if accountID == "" || conversationID == "" {
		return fmt.Errorf("mailstore: conversation note account and conversation are required")
	}
	note = strings.TrimSpace(note)
	if note == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM conversation_notes WHERE account_id = ? AND conversation_id = ?`, accountID, conversationID)
		if err != nil {
			return fmt.Errorf("mailstore: delete conversation note: %w", err)
		}
		return nil
	}
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO conversation_notes(account_id, conversation_id, note, created_at, updated_at)
		VALUES(?, ?, ?, ?, ?)
		ON CONFLICT(account_id, conversation_id) DO UPDATE SET note=excluded.note, updated_at=excluded.updated_at`,
		accountID, conversationID, note, now, now)
	if err != nil {
		return fmt.Errorf("mailstore: set conversation note: %w", err)
	}
	return nil
}

func (s *Store) ensureMessageAttachmentMetadataColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(messages)`)
	if err != nil {
		return fmt.Errorf("mailstore: inspect messages schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("mailstore: scan messages schema: %w", err)
		}
		if name == "attachment_metadata_cached" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mailstore: inspect messages schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE messages ADD COLUMN attachment_metadata_cached INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("mailstore: add attachment metadata column: %w", err)
	}
	return nil
}

func (s *Store) ensureAIModelReasoningEffortColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(ai_models)`)
	if err != nil {
		return fmt.Errorf("mailstore: inspect AI models schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("mailstore: scan AI models schema: %w", err)
		}
		if name == "reasoning_effort" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mailstore: inspect AI models schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE ai_models ADD COLUMN reasoning_effort TEXT NOT NULL DEFAULT 'medium'`); err != nil {
		return fmt.Errorf("mailstore: add AI model reasoning effort column: %w", err)
	}
	return nil
}

func (s *Store) ensureAIAgentPurposeColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(ai_agents)`)
	if err != nil {
		return fmt.Errorf("mailstore: inspect AI agents schema: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("mailstore: scan AI agents schema: %w", err)
		}
		if name == "purpose" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("mailstore: inspect AI agents schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE ai_agents ADD COLUMN purpose TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("mailstore: add AI agent purpose column: %w", err)
	}
	return nil
}

func newID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("mailstore: generate id: %w", err)
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func timeFromUnix(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(v, 0)
}

func marshalJSON(v any, empty string) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return empty
	}
	return string(raw)
}

func unmarshalJSON(raw, fallback string, dst any) {
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	if err := json.Unmarshal([]byte(raw), dst); err != nil {
		_ = json.Unmarshal([]byte(fallback), dst)
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func intBool(v int) bool { return v != 0 }

// CreateUser stores an application user. Password hashing is intentionally
// kept outside this package so the storage layer never decides a password
// policy; callers pass a bcrypt/argon hash rather than plaintext.
func (s *Store) CreateUser(ctx context.Context, login, displayName, passwordHash string) (User, error) {
	login = strings.TrimSpace(strings.ToLower(login))
	if login == "" || passwordHash == "" {
		return User{}, fmt.Errorf("mailstore: login and password hash are required")
	}
	id, err := newID("usr")
	if err != nil {
		return User{}, err
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(id, login, display_name, password_hash, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?)`, id, login, displayName, passwordHash, now, now)
	if err != nil {
		return User{}, fmt.Errorf("mailstore: create user: %w", err)
	}
	return User{ID: id, Login: login, DisplayName: displayName, PasswordHash: passwordHash, CreatedAt: timeFromUnix(now)}, nil
}

func (s *Store) GetUserByLogin(ctx context.Context, login string) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, login, display_name, password_hash, created_at FROM users WHERE login = ?`, strings.ToLower(strings.TrimSpace(login))).Scan(&u.ID, &u.Login, &u.DisplayName, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("mailstore: get user: %w", err)
	}
	u.CreatedAt = timeFromUnix(created)
	return u, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	var u User
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT id, login, display_name, password_hash, created_at FROM users WHERE id = ?`, id).Scan(&u.ID, &u.Login, &u.DisplayName, &u.PasswordHash, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("mailstore: get user: %w", err)
	}
	u.CreatedAt = timeFromUnix(created)
	return u, nil
}

// EnsureLegacyUser lets existing direct-mailbox sessions participate in the
// mirror before the application-user registration flow is enabled. Such a
// user has no local password until the user explicitly sets one.
func (s *Store) EnsureLegacyUser(ctx context.Context, login string) (User, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if login == "" {
		return User{}, fmt.Errorf("mailstore: legacy login is empty")
	}
	if u, err := s.GetUserByLogin(ctx, login); err == nil {
		return u, nil
	} else if !errors.Is(err, ErrNotFound) {
		return User{}, err
	}
	id, err := newID("usr")
	if err != nil {
		return User{}, err
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO users(id, login, display_name, password_hash, created_at, updated_at) VALUES(?, ?, ?, '', ?, ?)`, id, login, login, now, now)
	if err != nil {
		return User{}, fmt.Errorf("mailstore: create legacy user: %w", err)
	}
	return s.GetUserByLogin(ctx, login)
}

func (s *Store) SetUserPassword(ctx context.Context, userID, passwordHash string) error {
	if passwordHash == "" {
		return fmt.Errorf("mailstore: password hash is empty")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, passwordHash, time.Now().Unix(), userID)
	if err != nil {
		return fmt.Errorf("mailstore: set user password: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpsertAccount(ctx context.Context, account Account) (Account, error) {
	if account.OwnerID == "" || account.Email == "" || account.Username == "" {
		return Account{}, fmt.Errorf("mailstore: account owner, email and username are required")
	}
	if account.ID == "" {
		var existing string
		err := s.db.QueryRowContext(ctx, `SELECT id FROM mail_accounts WHERE owner_id = ? AND email = ?`, account.OwnerID, strings.ToLower(account.Email)).Scan(&existing)
		if err == nil {
			account.ID = existing
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Account{}, fmt.Errorf("mailstore: find account: %w", err)
		}
	}
	if account.ID == "" {
		var err error
		account.ID, err = newID("acct")
		if err != nil {
			return Account{}, err
		}
	}
	if account.Label == "" {
		account.Label = account.Email
	}
	if account.AuthType == "" {
		account.AuthType = "password"
	}
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mail_accounts(id, owner_id, email, username, label, color, imap_server, imap_port, imap_tls, smtp_server, smtp_port, smtp_starttls, encrypted_password, auth_type, is_default, created_at, updated_at, last_sync_at, last_error)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(owner_id, email) DO UPDATE SET
			username=excluded.username, label=excluded.label, color=excluded.color,
			imap_server=excluded.imap_server, imap_port=excluded.imap_port, imap_tls=excluded.imap_tls,
			smtp_server=excluded.smtp_server, smtp_port=excluded.smtp_port, smtp_starttls=excluded.smtp_starttls,
			encrypted_password=excluded.encrypted_password, auth_type=excluded.auth_type,
			updated_at=excluded.updated_at`,
		account.ID, account.OwnerID, strings.ToLower(account.Email), account.Username, account.Label, account.Color,
		account.IMAPServer, account.IMAPPort, boolInt(account.IMAPTLS), account.SMTPServer, account.SMTPPort,
		boolInt(account.SMTPStartTLS), account.EncryptedPassword, account.AuthType, boolInt(account.IsDefault), now, now,
		unixOrZero(account.LastSyncAt), account.LastError)
	if err != nil {
		return Account{}, fmt.Errorf("mailstore: upsert account: %w", err)
	}
	if err := s.setDefaultAccount(ctx, account.OwnerID, account.ID, account.IsDefault); err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, account.ID)
}

func (s *Store) setDefaultAccount(ctx context.Context, ownerID, accountID string, makeDefault bool) error {
	if !makeDefault {
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mail_accounts WHERE owner_id = ? AND is_default = 1`, ownerID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END WHERE owner_id = ?`, accountID, ownerID); err != nil {
		return fmt.Errorf("mailstore: set default account: %w", err)
	}
	return nil
}

func scanAccount(scanner interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var imapTLS, smtpStartTLS, isDefault int
	var lastSync int64
	err := scanner.Scan(&a.ID, &a.OwnerID, &a.Email, &a.Username, &a.Label, &a.Color, &a.IMAPServer, &a.IMAPPort, &imapTLS, &a.SMTPServer, &a.SMTPPort, &smtpStartTLS, &a.EncryptedPassword, &a.AuthType, &isDefault, &lastSync, &a.LastError)
	if err != nil {
		return Account{}, err
	}
	a.IMAPTLS = intBool(imapTLS)
	a.SMTPStartTLS = intBool(smtpStartTLS)
	a.IsDefault = intBool(isDefault)
	a.LastSyncAt = timeFromUnix(lastSync)
	return a, nil
}

const accountColumns = `id, owner_id, email, username, label, color, imap_server, imap_port, imap_tls, smtp_server, smtp_port, smtp_starttls, encrypted_password, auth_type, is_default, last_sync_at, last_error`

func (s *Store) GetAccount(ctx context.Context, id string) (Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM mail_accounts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("mailstore: get account: %w", err)
	}
	return a, nil
}

func (s *Store) GetAccountByEmail(ctx context.Context, ownerID, email string) (Account, error) {
	a, err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM mail_accounts WHERE owner_id = ? AND email = ?`, ownerID, strings.ToLower(strings.TrimSpace(email))))
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("mailstore: get account by email: %w", err)
	}
	return a, nil
}

func (s *Store) ListAccounts(ctx context.Context, ownerID string) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM mail_accounts WHERE owner_id = ? ORDER BY is_default DESC, email`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list accounts: %w", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan account: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListAllAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM mail_accounts ORDER BY owner_id, is_default DESC, email`)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list all accounts: %w", err)
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, fmt.Errorf("mailstore: scan account: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAccount(ctx context.Context, ownerID, accountID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM mail_accounts WHERE owner_id = ? AND id = ?`, ownerID, accountID)
	if err != nil {
		return fmt.Errorf("mailstore: delete account: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// Keep one deterministic default account after deleting the current
	// default. This makes a subsequent application login recoverable without
	// requiring the user to repair the database manually.
	var defaultID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM mail_accounts WHERE owner_id = ? ORDER BY is_default DESC, email LIMIT 1`, ownerID).Scan(&defaultID); err == nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END WHERE owner_id = ?`, defaultID, ownerID); err != nil {
			return fmt.Errorf("mailstore: restore default account: %w", err)
		}
	}
	return nil
}

// SetDefaultAccount makes accountID the account selected for the next local
// application login. It deliberately does not change the active HTTP session;
// callers can switch the session and persist the preference together.
func (s *Store) SetDefaultAccount(ctx context.Context, ownerID, accountID string) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mail_accounts WHERE owner_id = ? AND id = ?`, ownerID, accountID).Scan(&exists); err != nil {
		return fmt.Errorf("mailstore: check default account: %w", err)
	}
	if exists == 0 {
		return ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET is_default = CASE WHEN id = ? THEN 1 ELSE 0 END WHERE owner_id = ?`, accountID, ownerID)
	if err != nil {
		return fmt.Errorf("mailstore: set default account: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetAccountError(ctx context.Context, accountID, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET last_error = ?, last_sync_at = ? WHERE id = ?`, message, time.Now().Unix(), accountID)
	return err
}

func (s *Store) MarkAccountSyncedAt(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE mail_accounts SET last_error = '', last_sync_at = ? WHERE id = ?`, unixOrZero(at), id)
	return err
}

func (s *Store) UpsertFolder(ctx context.Context, folder Folder) error {
	if folder.AccountID == "" || folder.Name == "" {
		return fmt.Errorf("mailstore: folder account and name are required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO folders(account_id, name, delimiter, attributes_json, unread_count, message_count, sync_complete, last_sync_at, last_error)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, name) DO UPDATE SET
			delimiter=excluded.delimiter, attributes_json=excluded.attributes_json,
			unread_count=excluded.unread_count, message_count=excluded.message_count`,
		folder.AccountID, folder.Name, folder.Delimiter, marshalJSON(folder.Attributes, "[]"), folder.UnreadCount,
		folder.MessageCount, boolInt(folder.SyncComplete), unixOrZero(folder.LastSyncAt), folder.LastError)
	return err
}

func (s *Store) ListFolders(ctx context.Context, accountID string) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id, name, delimiter, attributes_json, unread_count, message_count, sync_complete, last_sync_at, last_error FROM folders WHERE account_id = ? ORDER BY name`, accountID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list folders: %w", err)
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		var attrs string
		var complete int
		var lastSync int64
		if err := rows.Scan(&f.AccountID, &f.Name, &f.Delimiter, &attrs, &f.UnreadCount, &f.MessageCount, &complete, &lastSync, &f.LastError); err != nil {
			return nil, fmt.Errorf("mailstore: scan folder: %w", err)
		}
		unmarshalJSON(attrs, "[]", &f.Attributes)
		f.SyncComplete = intBool(complete)
		f.LastSyncAt = timeFromUnix(lastSync)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) GetSyncState(ctx context.Context, accountID, folderName string) (SyncState, error) {
	var state SyncState
	var complete, lastSync int
	err := s.db.QueryRowContext(ctx, `SELECT account_id, name, sync_complete, last_sync_at, last_error FROM folders WHERE account_id = ? AND name = ?`, accountID, folderName).Scan(&state.AccountID, &state.FolderName, &complete, &lastSync, &state.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncState{AccountID: accountID, FolderName: folderName}, ErrNotFound
	}
	if err != nil {
		return SyncState{}, err
	}
	state.SyncComplete = intBool(complete)
	state.LastSyncAt = timeFromUnix(int64(lastSync))
	return state, nil
}

func (s *Store) MarkFolderSync(ctx context.Context, accountID, folderName string, complete bool, errValue error) error {
	message := ""
	if errValue != nil {
		message = errValue.Error()
	}
	_, err := s.db.ExecContext(ctx, `UPDATE folders SET sync_complete = ?, last_sync_at = ?, last_error = ? WHERE account_id = ? AND name = ?`, boolInt(complete), time.Now().Unix(), message, accountID, folderName)
	return err
}

func (s *Store) ClearFolder(ctx context.Context, accountID, folderName string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM messages WHERE account_id = ? AND folder_name = ?`, accountID, folderName)
	return err
}

// PruneFolders removes local mailboxes that were absent from a successful
// remote LIST. Message rows do not reference folders directly, so both tables
// are cleaned in one transaction to prevent renamed/deleted folders lingering
// in local navigation or search results.
func (s *Store) PruneFolders(ctx context.Context, accountID string, seen []string) error {
	if strings.TrimSpace(accountID) == "" {
		return fmt.Errorf("mailstore: account is required to prune folders")
	}
	seenSet := make(map[string]struct{}, len(seen))
	for _, name := range seen {
		if strings.TrimSpace(name) != "" {
			seenSet[name] = struct{}{}
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mailstore: begin folder prune: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	rows, err := tx.QueryContext(ctx, `SELECT name FROM folders WHERE account_id = ?`, accountID)
	if err != nil {
		return fmt.Errorf("mailstore: list folders for prune: %w", err)
	}
	var stale []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("mailstore: scan folder for prune: %w", err)
		}
		if _, ok := seenSet[name]; !ok {
			stale = append(stale, name)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("mailstore: iterate folders for prune: %w", err)
	}
	rows.Close()

	for _, name := range stale {
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE account_id = ? AND folder_name = ?`, accountID, name); err != nil {
			return fmt.Errorf("mailstore: prune messages for folder %q: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM folders WHERE account_id = ? AND name = ?`, accountID, name); err != nil {
			return fmt.Errorf("mailstore: prune folder %q: %w", name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("mailstore: commit folder prune: %w", err)
	}
	return nil
}

func parseUIDString(id string) (int64, error) {
	uid, err := strconv.ParseUint(strings.TrimSpace(id), 10, 32)
	if err != nil || uid == 0 {
		return 0, fmt.Errorf("invalid message UID %q", id)
	}
	return int64(uid), nil
}
