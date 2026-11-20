package mailstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrConversationIndexStale = errors.New("mailstore: conversation index changed during read")

type ConversationIndexRow struct {
	ID          string
	Date        time.Time
	Status      string
	UnreadCount int
	Summary     []byte
	Messages    []byte
}

type ConversationIndexPage struct {
	Summaries   [][]byte
	NextDate    time.Time
	NextID      string
	Tags        []string
	Total       int
	UnreadCount int
	HasMore     bool
}

func (s *Store) migrateConversationIndex(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS conversation_index_state (
			account_id TEXT PRIMARY KEY REFERENCES mail_accounts(id) ON DELETE CASCADE,
			revision INTEGER NOT NULL DEFAULT 0,
			indexed_revision INTEGER NOT NULL DEFAULT -1
		)`,
		`CREATE TABLE IF NOT EXISTS conversation_index_rows (
			account_id TEXT NOT NULL REFERENCES conversation_index_state(account_id) ON DELETE CASCADE,
			conversation_id TEXT NOT NULL,
			sort_seconds INTEGER NOT NULL,
			sort_nanos INTEGER NOT NULL,
			status TEXT NOT NULL,
			unread_count INTEGER NOT NULL,
			summary TEXT NOT NULL,
			messages TEXT NOT NULL,
			PRIMARY KEY(account_id, conversation_id)
		) WITHOUT ROWID`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_index_page ON conversation_index_rows(account_id, sort_seconds, sort_nanos, conversation_id)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_index_counts ON conversation_index_rows(account_id, status, unread_count)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("mailstore: migrate conversation index: %w", err)
		}
	}

	// Keep invalidation in the source transaction. Polls that only advance
	// sync timestamps or recalculate folder counts leave the index reusable.
	for _, source := range []struct {
		table   string
		columns string
	}{
		{"messages", "folder_name uid date_unix from_addr from_name to_addrs to_names_json subject preview flags_json attachments_json message_id body"},
		{"folders", "name attributes_json"},
		{"outgoing_messages", "status conversation_id message_id message_json"},
		{"drafts", "owner_id account_email conversation_id to_addrs subject body_text updated_at"},
		{"conversation_notes", "conversation_id note"},
		{"conversation_tags", "conversation_id tag"},
		{"conversation_statuses", "conversation_id status message_key"},
		{"mail_accounts", "owner_id email label color smtp_server"},
	} {
		columns := strings.Fields(source.columns)
		if source.table != "drafts" && source.table != "mail_accounts" {
			columns = append(columns, "account_id")
		}
		changed := make([]string, 0, len(columns))
		for _, column := range columns {
			if source.table == "messages" && column == "body" {
				changed = append(changed, "(trim(new.preview) = '' AND substr(old.body, 1, 1024) IS NOT substr(new.body, 1, 1024))")
				continue
			}
			changed = append(changed, "old."+column+" IS NOT new."+column)
		}
		for _, action := range []string{"INSERT", "UPDATE", "DELETE"} {
			if source.table == "mail_accounts" && action != "UPDATE" {
				continue
			}
			event, condition := action, ""
			target := conversationIndexTriggerTarget(source.table, "new")
			switch action {
			case "DELETE":
				target = conversationIndexTriggerTarget(source.table, "old")
			case "UPDATE":
				event += " OF " + strings.Join(columns, ", ")
				condition = " WHEN " + strings.Join(changed, " OR ")
				target = "(" + target + ") OR (" + conversationIndexTriggerTarget(source.table, "old") + ")"
			}
			statement := `CREATE TRIGGER IF NOT EXISTS conversation_index_` + source.table + "_" + strings.ToLower(action) +
				` AFTER ` + event + ` ON ` + source.table + condition + ` BEGIN
					INSERT INTO conversation_index_state(account_id, revision)
					SELECT id, 1 FROM mail_accounts WHERE ` + target + `
					ON CONFLICT(account_id) DO UPDATE SET revision = revision + 1;
				END`
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("mailstore: migrate %s conversation invalidation: %w", source.table, err)
			}
		}
	}
	return tx.Commit()
}

func conversationIndexTriggerTarget(table, row string) string {
	switch table {
	case "mail_accounts":
		return "id = " + row + ".id"
	case "drafts":
		return row + ".conversation_id <> '' AND owner_id = " + row + ".owner_id AND lower(email) = lower(" + row + ".account_email)"
	default:
		return "id = " + row + ".account_id"
	}
}

func (s *Store) ConversationIndexRevision(ctx context.Context, accountID string) (revision int64, current bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT revision, revision = indexed_revision
		FROM conversation_index_state WHERE account_id = ?`, accountID).Scan(&revision, &current)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return revision, current, err
}

// ReplaceConversationIndex commits the derived data only if every source read
// belongs to the same revision. Reading the source and encoding rows uses no
// write lock, and an intervening source update leaves the old index untouched.
func (s *Store) ReplaceConversationIndex(ctx context.Context, accountID string, revision int64, rows []ConversationIndexRow) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_index_state(account_id) VALUES(?)
		ON CONFLICT(account_id) DO NOTHING`, accountID); err != nil {
		return false, err
	}
	var stored, indexed int64
	if err := tx.QueryRowContext(ctx, `SELECT revision, indexed_revision
		FROM conversation_index_state WHERE account_id = ?`, accountID).Scan(&stored, &indexed); err != nil {
		return false, err
	}
	if stored != revision || indexed == revision {
		return stored == indexed, nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_index_rows WHERE account_id = ?`, accountID); err != nil {
		return false, err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO conversation_index_rows
		(account_id, conversation_id, sort_seconds, sort_nanos, status, unread_count, summary, messages)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, accountID, row.ID, -row.Date.Unix(), -row.Date.Nanosecond(),
			row.Status, row.UnreadCount, string(row.Summary), string(row.Messages)); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversation_index_state SET indexed_revision = ? WHERE account_id = ?`, revision, accountID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ReadConversationIndexPage reads the counts, tags and page from the same
// SQLite snapshot. Only summaries on the requested page are decoded by Go.
func (s *Store) ReadConversationIndexPage(ctx context.Context, accountIDs []string, open bool, tag string, before time.Time, beforeID string, limit int) (ConversationIndexPage, error) {
	page := ConversationIndexPage{Tags: []string{}}
	if len(accountIDs) == 0 {
		return page, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(accountIDs)), ",")
	args := make([]any, 0, len(accountIDs))
	for _, id := range accountIDs {
		args = append(args, id)
	}
	var indexed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM conversation_index_state
		WHERE revision = indexed_revision AND account_id IN (`+placeholders+`)`, args...).Scan(&indexed); err != nil {
		return page, err
	}
	if indexed != len(accountIDs) {
		return page, ErrConversationIndexStale
	}
	scope := ` FROM conversation_index_rows AS r WHERE r.account_id IN (` + placeholders + `)`
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sum(r.unread_count), 0)`+scope, args...).Scan(&page.UnreadCount); err != nil {
		return page, err
	}
	tags, err := tx.QueryContext(ctx, `SELECT DISTINCT t.tag FROM conversation_tags AS t
		JOIN conversation_index_rows AS r USING(account_id, conversation_id)
		WHERE t.account_id IN (`+placeholders+`) ORDER BY t.tag`, args...)
	if err != nil {
		return page, err
	}
	for tags.Next() {
		var value string
		if err := tags.Scan(&value); err != nil {
			tags.Close()
			return page, err
		}
		page.Tags = append(page.Tags, value)
	}
	tags.Close()
	if err := tags.Err(); err != nil {
		return page, err
	}
	if open {
		scope += ` AND r.status IN ('unanswered','draft')`
	}
	if tag != "" {
		scope += ` AND EXISTS (SELECT 1 FROM conversation_tags AS t
			WHERE t.account_id = r.account_id AND t.conversation_id = r.conversation_id AND t.tag = ?)`
		args = append(args, tag)
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*)`+scope, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	if limit > 0 && beforeID != "" {
		scope += ` AND (r.sort_seconds, r.sort_nanos, r.conversation_id) > (?, ?, ?)`
		args = append(args, -before.Unix(), -before.Nanosecond(), beforeID)
	}
	query := `SELECT r.summary, r.sort_seconds, r.sort_nanos, r.conversation_id` + scope +
		` ORDER BY r.sort_seconds, r.sort_nanos, r.conversation_id`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit+1)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		if limit > 0 && len(page.Summaries) == limit {
			page.HasMore = true
			break
		}
		var summary []byte
		var seconds, nanos int64
		if err := rows.Scan(&summary, &seconds, &nanos, &page.NextID); err != nil {
			return page, err
		}
		page.NextDate = time.Unix(-seconds, -nanos)
		page.Summaries = append(page.Summaries, summary)
	}
	return page, rows.Err()
}

func (s *Store) ReadConversationIndexDetail(ctx context.Context, accountID, conversationID string) (summary, messages []byte, revision int64, err error) {
	var current bool
	err = s.db.QueryRowContext(ctx, `SELECT state.revision, state.revision = state.indexed_revision, r.summary, r.messages
		FROM conversation_index_state AS state LEFT JOIN conversation_index_rows AS r
		ON r.account_id = state.account_id AND r.conversation_id = ?
		WHERE state.account_id = ?`, conversationID, accountID).Scan(&revision, &current, &summary, &messages)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !current) {
		return nil, nil, 0, ErrConversationIndexStale
	}
	if err == nil && summary == nil {
		return nil, nil, revision, ErrNotFound
	}
	return summary, messages, revision, err
}
