package mailstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"inbrix/models"
)

type MessageSearchPage struct {
	Messages   []models.Email
	NextCursor string
	HasMore    bool
}

type messageSearchCursor struct {
	Query  string `json:"q"`
	Date   int64  `json:"d"`
	Folder string `json:"f"`
	UID    int64  `json:"u"`
}

// SearchMessagePage reads only one page of bodies. A non-nil keys slice limits
// the search to a server-resolved conversation; an empty slice matches nothing.
// Keyset pagination avoids skipping older hits when new mail arrives.
func (s *Store) SearchMessagePage(ctx context.Context, accountID, scopeID, query, cursor string, limit int, keys []MessageKey) (MessageSearchPage, error) {
	page := MessageSearchPage{Messages: []models.Email{}}
	if keys != nil && len(keys) == 0 {
		return page, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	parsed := parseSearchQuery(query)
	clauses := []string{"account_id = ?"}
	args := []any{accountID}
	if parsed.folder != "" {
		clauses = append(clauses, "folder_name = ?")
		args = append(args, parsed.folder)
	}
	clauses = append(clauses, parsed.clauses...)
	args = append(args, parsed.args...)
	if keys != nil {
		values := make([]string, 0, len(keys))
		for _, key := range keys {
			uid, err := parseUIDString(key.UID)
			if err != nil {
				continue
			}
			values = append(values, "(?, ?)")
			args = append(args, key.FolderName, uid)
		}
		if len(values) == 0 {
			return page, nil
		}
		clauses = append(clauses, "(folder_name, uid) IN (VALUES "+strings.Join(values, ",")+")")
	}
	identity := sha256.Sum256([]byte(accountID + "\x00" + scopeID + "\x00" + query))
	fingerprint := hex.EncodeToString(identity[:])
	if cursor != "" {
		var boundary messageSearchCursor
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &boundary) != nil || boundary.Query != fingerprint || boundary.UID <= 0 || boundary.Folder == "" {
			return page, fmt.Errorf("invalid mail search cursor; restart the search")
		}
		clauses = append(clauses, "(date_unix < ? OR (date_unix = ? AND (folder_name > ? OR (folder_name = ? AND uid < ?))))")
		args = append(args, boundary.Date, boundary.Date, boundary.Folder, boundary.Folder, boundary.UID)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, "SELECT "+messageColumns+" FROM messages WHERE "+strings.Join(clauses, " AND ")+" ORDER BY date_unix DESC, folder_name ASC, uid DESC LIMIT ?", args...)
	if err != nil {
		return page, fmt.Errorf("mailstore: search page: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		email, err := scanMessage(rows)
		if err != nil {
			return page, err
		}
		page.Messages = append(page.Messages, email)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Messages) > limit {
		page.Messages = page.Messages[:limit]
		page.HasMore = true
		last := page.Messages[limit-1]
		uid, _ := strconv.ParseInt(last.ID, 10, 64)
		raw, _ := json.Marshal(messageSearchCursor{Query: fingerprint, Date: unixOrZero(last.Date), Folder: last.Folder, UID: uid})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
