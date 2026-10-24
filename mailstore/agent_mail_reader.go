package mailstore

import (
	"context"
	"fmt"
	"strings"
	"time"

	mailapi "inbrix/handlers/api"
	"inbrix/models"
)

// AgentMailReader is the common read-only evidence boundary for interactive
// agents and background skills. Account identity always comes from the caller,
// never from model-generated tool arguments. A zero AsOf allows live chat.
type AgentMailReader struct {
	Store     *Store
	AccountID string
	AsOf      time.Time
	Anchor    MessageKey
}

func (r AgentMailReader) Search(ctx context.Context, scope, query, cursor string, limit int, keys []MessageKey) (MessageSearchPage, error) {
	return r.Store.searchMessagePage(ctx, r.AccountID, scope, query, cursor, limit, keys, r.AsOf, r.Anchor)
}

func (r AgentMailReader) Read(ctx context.Context, folder, uid string) (models.Email, error) {
	mail, err := r.Store.GetMessage(ctx, r.AccountID, folder, uid)
	if err != nil {
		return models.Email{}, err
	}
	if !r.AsOf.IsZero() && mail.Date.After(r.AsOf) {
		return models.Email{}, fmt.Errorf("mail is newer than the analysis boundary")
	}
	if folder == r.Anchor.FolderName {
		anchor, anchorErr := parseUIDString(r.Anchor.UID)
		actual, _ := parseUIDString(uid)
		if anchorErr == nil && actual > anchor {
			return models.Email{}, fmt.Errorf("mail arrived after the triggering message")
		}
	}
	return mail, nil
}

// AgentMailText retains quoted/forwarded content. Its role is evidence
// projection; deciding which parts are newly authored belongs to the skill.
func AgentMailText(mail models.Email) string {
	if markup := strings.TrimSpace(mail.HTML); markup != "" {
		if text := strings.TrimSpace(mailapi.PlainTextFromHTML(markup)); text != "" {
			return text
		}
	}
	if body := strings.TrimSpace(mail.Body); body != "" {
		return body
	}
	return strings.TrimSpace(mail.Preview)
}
