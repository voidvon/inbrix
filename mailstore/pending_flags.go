package mailstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const seenFlag = `\Seen`

// MessageFlagKey identifies one mirrored message without trusting a UID outside
// its mailbox scope.
type MessageFlagKey struct {
	FolderName string
	UID        string
}

// PendingFlagUpdate is a durable, idempotent request to reconcile one local
// flag with the upstream IMAP mailbox.
type PendingFlagUpdate struct {
	AccountID   string
	FolderName  string
	UID         string
	Flag        string
	Add         bool
	Version     int64
	Attempts    int
	NextAttempt time.Time
}

func flagsWithState(flags []string, flag string, add bool) ([]string, bool) {
	next := make([]string, 0, len(flags)+1)
	found := false
	for _, current := range flags {
		if strings.EqualFold(current, flag) {
			found = true
			if !add {
				continue
			}
			current = flag
		}
		next = append(next, current)
	}
	if add && !found {
		next = append(next, flag)
	}
	return next, found != add
}

// QueueSeenUpdates makes the local mirror authoritative immediately and
// persists the matching IMAP work in the same transaction. Repeated toggles
// coalesce to the latest desired state while incrementing version so an older
// in-flight delivery cannot acknowledge a newer user action.
func (s *Store) QueueSeenUpdates(ctx context.Context, accountID string, keys []MessageFlagKey, seen bool) (int, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return 0, fmt.Errorf("mailstore: account is required to queue seen updates")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("mailstore: begin seen update: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Unix()
	updated := 0
	folders := make(map[string]struct{})
	deduped := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		folder := strings.TrimSpace(key.FolderName)
		uid, parseErr := parseUIDString(key.UID)
		if parseErr != nil || folder == "" {
			return 0, fmt.Errorf("mailstore: invalid message flag key %q/%q", folder, key.UID)
		}
		dedupeKey := folder + "\x00" + key.UID
		if _, exists := deduped[dedupeKey]; exists {
			continue
		}
		deduped[dedupeKey] = struct{}{}

		var rawFlags string
		if err := tx.QueryRowContext(ctx, `SELECT flags_json FROM messages WHERE account_id = ? AND folder_name = ? AND uid = ?`, accountID, folder, uid).Scan(&rawFlags); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, ErrNotFound
			}
			return 0, fmt.Errorf("mailstore: read flags for %s/%s: %w", folder, key.UID, err)
		}
		var flags []string
		if err := json.Unmarshal([]byte(rawFlags), &flags); err != nil {
			flags = nil
		}
		next, changed := flagsWithState(flags, seenFlag, seen)
		if !changed {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET flags_json = ?, updated_at = ? WHERE account_id = ? AND folder_name = ? AND uid = ?`, marshalJSON(next, "[]"), now, accountID, folder, uid); err != nil {
			return 0, fmt.Errorf("mailstore: update local flags for %s/%s: %w", folder, key.UID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pending_flag_updates(account_id, folder_name, uid, flag, add_flag, version, attempt_count, next_attempt_at, last_error, created_at, updated_at)
			VALUES(?, ?, ?, ?, ?, 1, 0, 0, '', ?, ?)
			ON CONFLICT(account_id, folder_name, uid, flag) DO UPDATE SET
				add_flag=excluded.add_flag, version=pending_flag_updates.version + 1,
				attempt_count=0, next_attempt_at=0, last_error='', updated_at=excluded.updated_at`,
			accountID, folder, uid, seenFlag, boolInt(seen), now, now); err != nil {
			return 0, fmt.Errorf("mailstore: queue flag update for %s/%s: %w", folder, key.UID, err)
		}
		folders[folder] = struct{}{}
		updated++
	}

	for folder := range folders {
		if _, err := tx.ExecContext(ctx, `
			UPDATE folders SET unread_count = (
				SELECT COALESCE(SUM(CASE WHEN instr(flags_json, ?) = 0 THEN 1 ELSE 0 END), 0)
				FROM messages WHERE account_id = ? AND folder_name = ?
			) WHERE account_id = ? AND name = ?`, seenFlag, accountID, folder, accountID, folder); err != nil {
			return 0, fmt.Errorf("mailstore: update local unread count for %s: %w", folder, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("mailstore: commit seen update: %w", err)
	}
	return updated, nil
}

func (s *Store) ListPendingFlagUpdates(ctx context.Context, accountID string, due time.Time, limit int) ([]PendingFlagUpdate, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT account_id, folder_name, uid, flag, add_flag, version, attempt_count, next_attempt_at
		FROM pending_flag_updates
		WHERE account_id = ? AND next_attempt_at <= ?
		ORDER BY updated_at, folder_name, uid LIMIT ?`, accountID, due.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list pending flag updates: %w", err)
	}
	defer rows.Close()
	var updates []PendingFlagUpdate
	for rows.Next() {
		var update PendingFlagUpdate
		var uid int64
		var add int
		var nextAttempt int64
		if err := rows.Scan(&update.AccountID, &update.FolderName, &uid, &update.Flag, &add, &update.Version, &update.Attempts, &nextAttempt); err != nil {
			return nil, fmt.Errorf("mailstore: scan pending flag update: %w", err)
		}
		update.UID = fmt.Sprintf("%d", uid)
		update.Add = intBool(add)
		update.NextAttempt = timeFromUnix(nextAttempt)
		updates = append(updates, update)
	}
	return updates, rows.Err()
}

func (s *Store) CompletePendingFlagUpdate(ctx context.Context, update PendingFlagUpdate) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_flag_updates WHERE account_id = ? AND folder_name = ? AND uid = ? AND flag = ? AND version = ?`, update.AccountID, update.FolderName, update.UID, update.Flag, update.Version)
	if err != nil {
		return fmt.Errorf("mailstore: complete pending flag update: %w", err)
	}
	return nil
}

func (s *Store) FailPendingFlagUpdate(ctx context.Context, update PendingFlagUpdate, nextAttempt time.Time, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE pending_flag_updates
		SET attempt_count = attempt_count + 1, next_attempt_at = ?, last_error = ?, updated_at = ?
		WHERE account_id = ? AND folder_name = ? AND uid = ? AND flag = ? AND version = ?`,
		nextAttempt.Unix(), message, time.Now().Unix(), update.AccountID, update.FolderName, update.UID, update.Flag, update.Version)
	if err != nil {
		return fmt.Errorf("mailstore: defer pending flag update: %w", err)
	}
	return nil
}
