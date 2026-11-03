package mailstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// NormalizeConversationTags keeps tag names bounded and canonical at the API
// and storage boundaries. An empty list clears a conversation's tags.
func NormalizeConversationTags(tags []string) ([]string, error) {
	if len(tags) > 10 {
		return nil, fmt.Errorf("a conversation can have at most 10 tags")
	}
	normalized := make([]string, 0, len(tags))
	seen := make(map[string]bool)
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > 32 || strings.ContainsFunc(tag, unicode.IsControl) {
			return nil, fmt.Errorf("tag names must contain 1–32 characters without control characters")
		}
		if !seen[tag] {
			seen[tag] = true
			normalized = append(normalized, tag)
		}
	}
	sort.Strings(normalized)
	return normalized, nil
}

func (s *Store) ListConversationTags(ctx context.Context, accountID string) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT conversation_id, tag FROM conversation_tags WHERE account_id = ? ORDER BY tag`, accountID)
	if err != nil {
		return nil, fmt.Errorf("mailstore: list conversation tags: %w", err)
	}
	defer rows.Close()
	tags := make(map[string][]string)
	for rows.Next() {
		var conversationID, tag string
		if err := rows.Scan(&conversationID, &tag); err != nil {
			return nil, err
		}
		tags[conversationID] = append(tags[conversationID], tag)
	}
	return tags, rows.Err()
}

func (s *Store) SetConversationTags(ctx context.Context, accountID, conversationID string, tags []string) error {
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("mailstore: conversation tag account and conversation are required")
	}
	tags, err := NormalizeConversationTags(tags)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_tags WHERE account_id = ? AND conversation_id = ?`, accountID, conversationID); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_tags(account_id, conversation_id, tag) VALUES(?, ?, ?)`, accountID, conversationID, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}
