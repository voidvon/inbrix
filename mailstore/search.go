package mailstore

import (
	"context"
	"fmt"
	"inbrix/models"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var searchOperator = regexp.MustCompile(`(?i)^(from|to|cc|subject|in|is|has|before|after):(.+)$`)

// searchTextClause matches one free-text term against the header and body
// fields a user expects a mail search to cover. The last alternative reaches
// into the raw HTML part for messages that carry no text/plain body, so
// HTML-only mail (marketing mail, notifications, many web clients) is
// searchable too. Matches that only hit HTML markup are filtered out by the
// caller, which compares the term against the message's plain-text projection.
const searchTextClause = `(lower(subject) LIKE lower(?) OR lower(from_addr) LIKE lower(?) OR lower(to_addrs) LIKE lower(?) OR lower(cc) LIKE lower(?) OR lower(preview) LIKE lower(?) OR lower(body) LIKE lower(?) OR (trim(body) = '' AND lower(html) LIKE lower(?)))`

// searchTextArgCount is the number of bound parameters searchTextClause needs.
const searchTextArgCount = 7

// parsedSearch is the normalized form of a user search string: the SQL clauses
// that constrain the mirror, the folder override requested by an `in:`
// operator, and the positive free-text terms. Terms are kept separately so
// callers can verify and highlight matches without re-implementing the query
// grammar.
type parsedSearch struct {
	clauses []string
	args    []any
	folder  string
	terms   []string
}

func negatedClause(clause string, negated bool) string {
	if negated {
		return "NOT (" + clause + ")"
	}
	return clause
}

// addTextTerm records one free-text term and its SQL constraint. Negated terms
// constrain the query but are not reported back as terms to highlight.
func (p *parsedSearch) addTextTerm(term string, negated bool) {
	if term == "" {
		return
	}
	p.clauses = append(p.clauses, negatedClause(searchTextClause, negated))
	pattern := "%" + term + "%"
	for i := 0; i < searchTextArgCount; i++ {
		p.args = append(p.args, pattern)
	}
	if !negated {
		p.terms = append(p.terms, term)
	}
}

// parseSearchQuery normalizes a search string into SQL clauses and terms. It
// supports the operators most useful to the web UI; anything it does not
// recognise is treated as a free-text term.
func parseSearchQuery(query string) parsedSearch {
	var parsed parsedSearch
	for _, raw := range strings.Fields(strings.TrimSpace(query)) {
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
				parsed.folder = value
				continue
			}
			switch key {
			case "from", "to", "cc", "subject":
				column := map[string]string{"from": "from_addr", "to": "to_addrs", "cc": "cc", "subject": "subject"}[key]
				parsed.clauses = append(parsed.clauses, negatedClause(fmt.Sprintf("lower(%s) LIKE lower(?)", column), negated))
				parsed.args = append(parsed.args, "%"+value+"%")
				if !negated && value != "" {
					parsed.terms = append(parsed.terms, value)
				}
			case "is":
				switch strings.ToLower(value) {
				case "unread":
					parsed.clauses = append(parsed.clauses, negatedClause("instr(flags_json, ?) = 0", negated))
					parsed.args = append(parsed.args, `\Seen`)
				case "read":
					parsed.clauses = append(parsed.clauses, negatedClause("instr(flags_json, ?) > 0", negated))
					parsed.args = append(parsed.args, `\Seen`)
				default:
					parsed.addTextTerm(term, negated)
				}
			case "has":
				if strings.EqualFold(value, "attachment") {
					parsed.clauses = append(parsed.clauses, negatedClause("has_attachments = 1", negated))
				} else {
					parsed.addTextTerm(term, negated)
				}
			case "before", "after":
				when, err := parseSearchDate(value)
				if err != nil {
					parsed.addTextTerm(term, negated)
					continue
				}
				op := "<"
				if key == "after" {
					op = ">="
				}
				parsed.clauses = append(parsed.clauses, negatedClause("date_unix "+op+" ?", negated))
				parsed.args = append(parsed.args, when.Unix())
			default:
				parsed.addTextTerm(term, negated)
			}
			continue
		}
		parsed.addTextTerm(term, negated)
	}
	return parsed
}

// SearchTextTerms returns the positive free-text terms of a search query with
// operators, quoting, and negations removed. Callers use it to verify and
// highlight the matches SearchMessages reports.
func SearchTextTerms(query string) []string {
	return parseSearchQuery(query).terms
}

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

	parsed := parseSearchQuery(query)
	if parsed.folder != "" {
		folderName = parsed.folder
	}
	clauses := parsed.clauses
	args := []any{accountID}
	if folderName != "" {
		clauses = append([]string{"folder_name = ?"}, clauses...)
		args = append(args, folderName)
	}
	if len(clauses) == 0 {
		return nil, folderName, fmt.Errorf("mailstore: search query has no searchable terms")
	}
	args = append(args, parsed.args...)

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
