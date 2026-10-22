package mailstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const APIKeyPrefix = "inbrix_live_"

var (
	ErrInvalidAPIKeyInput = errors.New("mailstore: invalid API key input")
	ErrAPIKeyInactive     = errors.New("mailstore: API key is revoked or expired")
)

type APIKeyRecord struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	RevokedAt  *time.Time `json:"revokedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	LastUsedIP string     `json:"lastUsedIP"`
}

type APIKeyWithSecret struct {
	APIKeyRecord
	Key string `json:"key"`
}

type APIKeyEvent struct {
	ID           int64     `json:"id"`
	Type         string    `json:"type"`
	ClientIP     string    `json:"clientIP"`
	Reason       string    `json:"reason,omitempty"`
	RelatedKeyID string    `json:"relatedKeyId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

const apiKeyColumns = `id, name, key_prefix, created_at, expires_at, revoked_at, last_used_at, last_used_ip`

func scanAPIKey(row interface{ Scan(...any) error }) (APIKeyRecord, error) {
	var key APIKeyRecord
	var created, expires, revoked, used int64
	err := row.Scan(&key.ID, &key.Name, &key.Prefix, &created, &expires, &revoked, &used, &key.LastUsedIP)
	if errors.Is(err, sql.ErrNoRows) {
		return key, ErrNotFound
	}
	if err != nil {
		return key, err
	}
	key.CreatedAt = timeFromUnix(created)
	key.ExpiresAt, key.RevokedAt, key.LastUsedAt = apiKeyTime(expires), apiKeyTime(revoked), apiKeyTime(used)
	key.Status = "active"
	if revoked != 0 {
		key.Status = "revoked"
	} else if expires != 0 && expires <= time.Now().Unix() {
		key.Status = "expired"
	}
	return key, nil
}

func apiKeyTime(value int64) *time.Time {
	if value == 0 {
		return nil
	}
	t := timeFromUnix(value)
	return &t
}

func hashAPIKey(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func newAPIKey(name string, expiresAt *time.Time) (APIKeyWithSecret, string, error) {
	key := APIKeyWithSecret{}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return key, "", fmt.Errorf("%w: 名称需要 1–120 个字符", ErrInvalidAPIKeyInput)
	}
	if expiresAt != nil && expiresAt.Unix() <= time.Now().Unix() {
		return key, "", fmt.Errorf("%w: 过期时间必须晚于当前时间", ErrInvalidAPIKeyInput)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return key, "", err
	}
	id, err := newID("key")
	if err != nil {
		return key, "", err
	}
	key.Key = APIKeyPrefix + base64.RawURLEncoding.EncodeToString(secret)
	key.APIKeyRecord = APIKeyRecord{ID: id, Name: name, Prefix: key.Key[:len(APIKeyPrefix)+12], Status: "active", CreatedAt: timeFromUnix(time.Now().Unix()), ExpiresAt: expiresAt}
	return key, hashAPIKey(key.Key), nil
}

func insertAPIKey(ctx context.Context, tx *sql.Tx, owner string, key APIKeyWithSecret, hash string) error {
	expires := int64(0)
	if key.ExpiresAt != nil {
		expires = key.ExpiresAt.Unix()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO user_api_keys(id, owner_id, name, key_prefix, key_hash, created_at, expires_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, key.ID, owner, key.Name, key.Prefix, hash, key.CreatedAt.Unix(), expires)
	return err
}

func recordAPIKeyEvent(ctx context.Context, tx *sql.Tx, id, kind, ip, reason, related string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO user_api_key_events(api_key_id, event_type, client_ip, reason, related_key_id, created_at) VALUES(?, ?, ?, ?, ?, ?)`, id, kind, ip, reason, related, time.Now().Unix())
	return err
}

func (s *Store) CreateAPIKey(ctx context.Context, owner, name string, expires *time.Time, ip string) (APIKeyWithSecret, error) {
	key, hash, err := newAPIKey(name, expires)
	if err != nil {
		return key, err
	}
	if _, err := s.GetUser(ctx, owner); err != nil {
		return APIKeyWithSecret{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return APIKeyWithSecret{}, err
	}
	defer tx.Rollback()
	if err := insertAPIKey(ctx, tx, owner, key, hash); err != nil {
		return APIKeyWithSecret{}, err
	}
	if err := recordAPIKeyEvent(ctx, tx, key.ID, "created", ip, "", ""); err != nil {
		return APIKeyWithSecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return APIKeyWithSecret{}, err
	}
	return key, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, owner string) ([]APIKeyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+apiKeyColumns+` FROM user_api_keys WHERE owner_id = ? ORDER BY created_at DESC, id DESC`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]APIKeyRecord, 0)
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// AuthenticateAPIKey resolves the current user on every request, so role
// changes apply to existing keys without copying or caching permissions.
func (s *Store) AuthenticateAPIKey(ctx context.Context, token, ip string) (User, string, error) {
	if len(token) != len(APIKeyPrefix)+43 || !strings.HasPrefix(token, APIKeyPrefix) {
		return User{}, "", ErrNotFound
	}
	var userID, keyID string
	now := time.Now().Unix()
	err := s.db.QueryRowContext(ctx, `SELECT k.id, k.owner_id FROM user_api_keys k JOIN users u ON u.id = k.owner_id WHERE k.key_hash = ? AND k.revoked_at = 0 AND (k.expires_at = 0 OR k.expires_at > ?)`, hashAPIKey(token), now).Scan(&keyID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", err
	}
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return User{}, "", err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE user_api_keys SET last_used_at = ?, last_used_ip = ? WHERE id = ? AND (last_used_at = 0 OR last_used_at <= ?)`, now, ip, keyID, now-300)
	if err != nil {
		return User{}, "", err
	}
	return user, keyID, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, owner, id, ip, reason string) error {
	if utf8.RuneCountInString(reason) > 500 {
		return fmt.Errorf("%w: 撤销原因不能超过 500 个字符", ErrInvalidAPIKeyInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	key, err := scanAPIKey(tx.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM user_api_keys WHERE owner_id = ? AND id = ?`, owner, id))
	if err != nil {
		return err
	}
	if key.RevokedAt != nil {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_api_keys SET revoked_at = ? WHERE owner_id = ? AND id = ?`, time.Now().Unix(), owner, id); err != nil {
		return err
	}
	if err := recordAPIKeyEvent(ctx, tx, id, "revoked", ip, strings.TrimSpace(reason), ""); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RotateAPIKey(ctx context.Context, owner, id, ip string) (APIKeyWithSecret, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return APIKeyWithSecret{}, err
	}
	defer tx.Rollback()
	old, err := scanAPIKey(tx.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM user_api_keys WHERE owner_id = ? AND id = ?`, owner, id))
	if err != nil {
		return APIKeyWithSecret{}, err
	}
	if old.Status != "active" {
		return APIKeyWithSecret{}, ErrAPIKeyInactive
	}
	key, hash, err := newAPIKey(old.Name, old.ExpiresAt)
	if err != nil {
		return APIKeyWithSecret{}, err
	}
	if err := insertAPIKey(ctx, tx, owner, key, hash); err != nil {
		return APIKeyWithSecret{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_api_keys SET revoked_at = ? WHERE id = ? AND owner_id = ?`, time.Now().Unix(), id, owner); err != nil {
		return APIKeyWithSecret{}, err
	}
	if err := recordAPIKeyEvent(ctx, tx, id, "rotated", ip, "", key.ID); err != nil {
		return APIKeyWithSecret{}, err
	}
	if err := recordAPIKeyEvent(ctx, tx, key.ID, "created", ip, "", id); err != nil {
		return APIKeyWithSecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return APIKeyWithSecret{}, err
	}
	return key, nil
}

func (s *Store) ListAPIKeyEvents(ctx context.Context, owner, id string) ([]APIKeyEvent, error) {
	if _, err := scanAPIKey(s.db.QueryRowContext(ctx, `SELECT `+apiKeyColumns+` FROM user_api_keys WHERE owner_id = ? AND id = ?`, owner, id)); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, event_type, client_ip, reason, related_key_id, created_at FROM user_api_key_events WHERE api_key_id = ? ORDER BY id DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]APIKeyEvent, 0)
	for rows.Next() {
		var event APIKeyEvent
		var created int64
		if err := rows.Scan(&event.ID, &event.Type, &event.ClientIP, &event.Reason, &event.RelatedKeyID, &created); err != nil {
			return nil, err
		}
		event.CreatedAt = timeFromUnix(created)
		events = append(events, event)
	}
	return events, rows.Err()
}
