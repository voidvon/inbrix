package mailstore

import (
	"context"
	"fmt"
	"lilmail/models"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var searchOperator = regexp.MustCompile(`(?i)^(from|to|cc|subject|in|is|has|before|after):(.+)$`)

// SearchMessages performs a local search over the synchronized mirror. It
// supports the operators most useful to the web UI and treats unrecognised
// text as a case-insensitive match across common header/preview fields.
func (s *Store) SearchMessages(ctx context.Context, accountID, folderName, query string, limit int) ([]models.Email, string, error) {
	if limit <= 0 {
		limit = 50
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, folderName, fmt.Errorf("mailstore: search query is empty")
	}

	var clauses []string
	args := []any{accountID}
	folderArgIndex := -1
	if folderName != "" {
		clauses = append(clauses, "folder_name = ?")
		args = append(args, folderName)
		folderArgIndex = len(args) - 1
	}
	var textTerms []string
	for _, raw := range strings.Fields(query) {
		term := strings.Trim(raw, `"'`)
		if term == "" {
			continue
		}
		negated := strings.HasPrefix(term, "-")
		if negated {
			term = strings.TrimPrefix(term, "-")
		}
		if match := searchOperator.FindStringSubmatch(term); match != nil {
			key, value := strings.ToLower(match[1]), strings.TrimSpace(match[2])
			if key == "in" && value != "" {
				folderName = value
				if folderArgIndex >= 0 {
					args[folderArgIndex] = value
				} else {
					clauses = append(clauses, "folder_name = ?")
					args = append(args, value)
					folderArgIndex = len(args) - 1
				}
				continue
			}
			switch key {
			case "from", "to", "cc", "subject":
				column := map[string]string{"from": "from_addr", "to": "to_addrs", "cc": "cc", "subject": "subject"}[key]
				clause := fmt.Sprintf("lower(%s) LIKE lower(?)", column)
				if negated {
					clause = "NOT (" + clause + ")"
				}
				clauses = append(clauses, clause)
				args = append(args, "%"+value+"%")
			case "is":
				switch strings.ToLower(value) {
				case "unread":
					clause := "instr(flags_json, ?) = 0"
					if negated {
						clause = "NOT (" + clause + ")"
					}
					clauses = append(clauses, clause)
					args = append(args, `\Seen`)
				case "read":
					clause := "instr(flags_json, ?) > 0"
					if negated {
						clause = "NOT (" + clause + ")"
					}
					clauses = append(clauses, clause)
					args = append(args, `\Seen`)
				default:
					textTerms = append(textTerms, raw)
				}
			case "has":
				if strings.EqualFold(value, "attachment") {
					clause := "has_attachments = 1"
					if negated {
						clause = "NOT (" + clause + ")"
					}
					clauses = append(clauses, clause)
				} else {
					textTerms = append(textTerms, raw)
				}
			case "before", "after":
				when, err := parseSearchDate(value)
				if err != nil {
					textTerms = append(textTerms, raw)
					continue
				}
				op := "<"
				if key == "after" {
					op = ">="
				}
				clause := "date_unix " + op + " ?"
				if negated {
					clause = "NOT (" + clause + ")"
				}
				clauses = append(clauses, clause)
				args = append(args, when.Unix())
			default:
				textTerms = append(textTerms, raw)
			}
			continue
		}
		textTerms = append(textTerms, term)
	}

	for _, term := range textTerms {
		negated := strings.HasPrefix(term, "-")
		term = strings.TrimPrefix(term, "-")
		if term == "" {
			continue
		}
		pattern := "%" + term + "%"
		clause := `(lower(subject) LIKE lower(?) OR lower(from_addr) LIKE lower(?) OR lower(to_addrs) LIKE lower(?) OR lower(cc) LIKE lower(?) OR lower(preview) LIKE lower(?) OR lower(body) LIKE lower(?))`
		if negated {
			clause = "NOT " + clause
		}
		clauses = append(clauses, clause)
		for i := 0; i < 6; i++ {
			args = append(args, pattern)
		}
	}
	if len(clauses) == 0 {
		return nil, folderName, fmt.Errorf("mailstore: search query has no searchable terms")
	}

	sqlText := `SELECT ` + messageColumns + ` FROM messages WHERE account_id = ? AND ` + strings.Join(clauses, " AND ") + ` ORDER BY date_unix DESC, uid DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, folderName, fmt.Errorf("mailstore: search: %w", err)
	}
	defer rows.Close()
	var out []models.Email
	for rows.Next() {
		email, err := scanMessage(rows)
		if err != nil {
			return nil, folderName, err
		}
		out = append(out, email)
	}
	return out, folderName, rows.Err()
}

func parseSearchDate(raw string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	if days, err := strconv.Atoi(strings.TrimSuffix(raw, "d")); err == nil && strings.HasSuffix(raw, "d") {
		return time.Now().Add(-time.Duration(days) * 24 * time.Hour), nil
	}
	return time.Time{}, fmt.Errorf("invalid search date %q", raw)
}
