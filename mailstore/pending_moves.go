package mailstore

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TargetFolderPermanentDelete is the sentinel target_folder value used in
// pending_message_moves to indicate a permanent expunge/deletion rather than a move.
const TargetFolderPermanentDelete = "<delete>"

// MessageMoveKey identifies one mirrored message to be moved.
type MessageMoveKey struct {
	FolderName string
	UID        string
}

// PendingMessageMove is a durable, idempotent request to move one message
// to a target mailbox (such as Trash) on the upstream IMAP server.
type PendingMessageMove struct {
	AccountID    string
	FolderName   string
	UID          string
	TargetFolder string
	Version      int64
	Attempts     int
	NextAttempt  time.Time
	LastError    string
}

// QueueMessageMoves deletes the messages locally from the mirror immediately,
// updates folder message and unread counts, and records durable pending move
// records so the upstream IMAP move can be executed asynchronously.
func (s *Store) QueueMessageMoves(ctx context.Context, accountID string, keys []MessageMoveKey, targetFolder string) (int, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return 0, fmt.Errorf("mailstore: account is required to queue message moves")
	}
	if len(keys) == 0 {
		return 0, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("mailstore: begin message move: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().Unix()
	targetFolder = strings.TrimSpace(targetFolder)
	folders := make(map[string]struct{})
	deduped := make(map[string]struct{}, len(keys))
	queued := 0

	for _, key := range keys {
		folder := strings.TrimSpace(key.FolderName)
		uid, parseErr := parseUIDString(key.UID)
		if parseErr != nil || folder == "" {
			return 0, fmt.Errorf("mailstore: invalid message move key %q/%q", folder, key.UID)
		}
		if targetFolder != "" && targetFolder != TargetFolderPermanentDelete && strings.EqualFold(folder, targetFolder) {
			continue
		}
		dedupeKey := folder + "\x00" + key.UID
		if _, exists := deduped[dedupeKey]; exists {
			continue
		}
		deduped[dedupeKey] = struct{}{}

		if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE account_id = ? AND folder_name = ? AND uid = ?`, accountID, folder, uid); err != nil {
			return 0, fmt.Errorf("mailstore: delete local message %s/%s: %w", folder, key.UID, err)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO pending_message_moves(account_id, folder_name, uid, target_folder, version, attempt_count, next_attempt_at, last_error, created_at, updated_at)
			VALUES(?, ?, ?, ?, 1, 0, 0, '', ?, ?)
			ON CONFLICT(account_id, folder_name, uid) DO UPDATE SET
				target_folder=excluded.target_folder, version=pending_message_moves.version + 1,
				attempt_count=0, next_attempt_at=0, last_error='', updated_at=excluded.updated_at`,
			accountID, folder, uid, targetFolder, now, now); err != nil {
			return 0, fmt.Errorf("mailstore: queue message move for %s/%s: %w", folder, key.UID, err)
		}
		folders[folder] = struct{}{}
		queued++
	}

	for folder := range folders {
		var messageCount, unreadCount int
		err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*), COALESCE(SUM(CASE WHEN instr(flags_json, ?) = 0 THEN 1 ELSE 0 END), 0)
			FROM messages WHERE account_id = ? AND folder_name = ?`, seenFlag, accountID, folder).Scan(&messageCount, &unreadCount)
		if err != nil {
			return 0, fmt.Errorf("mailstore: folder stats for %s: %w", folder, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE folders SET message_count = ?, unread_count = ? WHERE account_id = ? AND name = ?`, messageCount, unreadCount, accountID, folder); err != nil {
			return 0, fmt.Errorf("mailstore: update folder stats for %s: %w", folder, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("mailstore: commit message move: %w", err)
	}
	return queued, nil
}

// ListPendingMessageMoves returns message moves ready for upstream execution.
func (s *Store) ListPendingMessageMoves(ctx context.Context, accountID string, due time.Time, limit int) ([]PendingMessageMove, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT account_id, folder_name, uid, target_folder, version, attempt_count, next_attempt_at, last_error
		FROM pending_message_moves
		WHERE account_id = ? AND next_attempt_at <= ?
		ORDER BY updated_at, folder_name, uid LIMIT ?`, accountID, due.Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list pending message moves: %w", err)
	}
	defer rows.Close()

	var moves []PendingMessageMove
	for rows.Next() {
		var move PendingMessageMove
		var uid int64
		var nextAttempt int64
		if err := rows.Scan(&move.AccountID, &move.FolderName, &uid, &move.TargetFolder, &move.Version, &move.Attempts, &nextAttempt, &move.LastError); err != nil {
			return nil, fmt.Errorf("mailstore: scan pending message move: %w", err)
		}
		move.UID = fmt.Sprintf("%d", uid)
		move.NextAttempt = timeFromUnix(nextAttempt)
		moves = append(moves, move)
	}
	return moves, rows.Err()
}

// CompletePendingMessageMove cleans up a successfully delivered message move.
func (s *Store) CompletePendingMessageMove(ctx context.Context, move PendingMessageMove) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending_message_moves WHERE account_id = ? AND folder_name = ? AND uid = ? AND version = ?`, move.AccountID, move.FolderName, move.UID, move.Version)
	if err != nil {
		return fmt.Errorf("mailstore: complete pending message move: %w", err)
	}
	return nil
}

// FailPendingMessageMove marks a delivery attempt failed and defers the next retry.
func (s *Store) FailPendingMessageMove(ctx context.Context, move PendingMessageMove, nextAttempt time.Time, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE pending_message_moves
		SET attempt_count = attempt_count + 1, next_attempt_at = ?, last_error = ?, updated_at = ?
		WHERE account_id = ? AND folder_name = ? AND uid = ? AND version = ?`,
		nextAttempt.Unix(), message, time.Now().Unix(), move.AccountID, move.FolderName, move.UID, move.Version)
	if err != nil {
		return fmt.Errorf("mailstore: defer pending message move: %w", err)
	}
	return nil
}

// ResolveTrashFolder inspects the mirrored folders for an account to identify
// the canonical Trash folder without contacting IMAP.
func (s *Store) ResolveTrashFolder(ctx context.Context, accountID string) (string, error) {
	folders, err := s.ListFolders(ctx, accountID)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		for _, attr := range f.Attributes {
			if strings.EqualFold(attr, `\Trash`) {
				return f.Name, nil
			}
		}
	}
	for _, f := range folders {
		lc := strings.ToLower(strings.TrimSpace(f.Name))
		if lc == "trash" || lc == "deleted" || lc == "deleted items" || lc == "bin" || lc == "已删除" || strings.HasSuffix(lc, "/trash") {
			return f.Name, nil
		}
	}
	return "", nil
}

// ResolveJunkFolder inspects the mirrored folders for an account to identify
// the canonical Junk/Spam folder without contacting IMAP.
func (s *Store) ResolveJunkFolder(ctx context.Context, accountID string) (string, error) {
	folders, err := s.ListFolders(ctx, accountID)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		for _, attr := range f.Attributes {
			if strings.EqualFold(attr, `\Junk`) {
				return f.Name, nil
			}
		}
	}
	for _, f := range folders {
		lc := strings.ToLower(strings.TrimSpace(f.Name))
		if lc == "junk" || lc == "spam" || lc == "junk mail" || lc == "junk email" || lc == "junk e-mail" || lc == "bulk mail" || lc == "垃圾邮件" || strings.HasSuffix(lc, "/junk") || strings.HasSuffix(lc, "/spam") {
			return f.Name, nil
		}
	}
	return "", nil
}

// IsTrashFolder returns true if the specified folder represents the Trash / Deleted mailbox.
func (s *Store) IsTrashFolder(ctx context.Context, accountID, folderName string) (bool, error) {
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		return false, nil
	}
	folders, err := s.ListFolders(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, f := range folders {
		if strings.EqualFold(f.Name, folderName) {
			for _, attr := range f.Attributes {
				if strings.EqualFold(attr, `\Trash`) {
					return true, nil
				}
			}
		}
	}
	trash, err := s.ResolveTrashFolder(ctx, accountID)
	if err != nil {
		return false, err
	}
	if trash != "" && strings.EqualFold(folderName, trash) {
		return true, nil
	}
	lc := strings.ToLower(folderName)
	return lc == "trash" || lc == "deleted" || lc == "deleted items" || lc == "bin" || lc == "已删除" || strings.HasSuffix(lc, "/trash"), nil
}

// IsJunkFolder returns true if the specified folder represents the Junk / Spam mailbox.
func (s *Store) IsJunkFolder(ctx context.Context, accountID, folderName string) (bool, error) {
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		return false, nil
	}
	folders, err := s.ListFolders(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, f := range folders {
		if strings.EqualFold(f.Name, folderName) {
			for _, attr := range f.Attributes {
				if strings.EqualFold(attr, `\Junk`) {
					return true, nil
				}
			}
		}
	}
	junk, err := s.ResolveJunkFolder(ctx, accountID)
	if err != nil {
		return false, err
	}
	if junk != "" && strings.EqualFold(folderName, junk) {
		return true, nil
	}
	lc := strings.ToLower(folderName)
	return lc == "junk" || lc == "spam" || lc == "junk mail" || lc == "junk email" || lc == "junk e-mail" || lc == "bulk mail" || lc == "垃圾邮件" || strings.HasSuffix(lc, "/junk") || strings.HasSuffix(lc, "/spam"), nil
}

// QueueMessageDeletions queues permanent deletion for the given messages, deleting them
// locally from the mirror immediately and recording durable pending delete records.
func (s *Store) QueueMessageDeletions(ctx context.Context, accountID string, keys []MessageMoveKey) (int, error) {
	return s.QueueMessageMoves(ctx, accountID, keys, TargetFolderPermanentDelete)
}
