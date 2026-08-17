package web

import (
	"encoding/json"
	"errors"
	"lilmail/mailstore"
	"lilmail/models"
	"strings"
	"testing"
	"time"
)

type junkMutationClient struct {
	junk      string
	discover  error
	moveErr   error
	deleteErr error
	moved     [3]string
	deleted   [2]string
}

func (c *junkMutationClient) DiscoverJunkFolder() (string, error) { return c.junk, c.discover }
func (c *junkMutationClient) MoveMessage(src, uid, dest string) error {
	c.moved = [3]string{src, uid, dest}
	return c.moveErr
}
func (c *junkMutationClient) DeleteMessage(folder, uid string) error {
	c.deleted = [2]string{folder, uid}
	return c.deleteErr
}

func TestApplyRemoteJunkMessageMutation(t *testing.T) {
	t.Run("restore to inbox", func(t *testing.T) {
		client := &junkMutationClient{junk: "Junk"}
		if err := applyRemoteJunkMessageMutation(client, "junk", "42", false, nil); err != nil {
			t.Fatal(err)
		}
		if client.moved != [3]string{"junk", "42", "INBOX"} || client.deleted != [2]string{} {
			t.Fatalf("unexpected remote operations: moved=%q deleted=%q", client.moved, client.deleted)
		}
	})

	t.Run("permanent delete", func(t *testing.T) {
		client := &junkMutationClient{junk: "Junk"}
		if err := applyRemoteJunkMessageMutation(client, "Junk", "43", true, nil); err != nil {
			t.Fatal(err)
		}
		if client.deleted != [2]string{"Junk", "43"} || client.moved != [3]string{} {
			t.Fatalf("unexpected remote operations: moved=%q deleted=%q", client.moved, client.deleted)
		}
	})

	t.Run("reject outside junk", func(t *testing.T) {
		client := &junkMutationClient{junk: "Junk"}
		err := applyRemoteJunkMessageMutation(client, "INBOX", "44", true, nil)
		if !errors.Is(err, errMessageOutsideSpam) || client.deleted != [2]string{} || client.moved != [3]string{} {
			t.Fatalf("outside-junk mutation was not rejected: err=%v moved=%q deleted=%q", err, client.moved, client.deleted)
		}
	})

	t.Run("propagate remote failure", func(t *testing.T) {
		client := &junkMutationClient{junk: "Junk", deleteErr: errors.New("expunge failed")}
		if err := applyRemoteJunkMessageMutation(client, "Junk", "45", true, nil); err == nil || !strings.Contains(err.Error(), "expunge failed") {
			t.Fatalf("remote failure: %v", err)
		}
	})

	t.Run("cache cleanup failure blocks deletion", func(t *testing.T) {
		client := &junkMutationClient{junk: "Junk"}
		err := applyRemoteJunkMessageMutation(client, "Junk", "46", true, func() error { return errors.New("storage unavailable") })
		if !errors.Is(err, errAttachmentCacheCleanup) || client.deleted != [2]string{} || client.moved != [3]string{} {
			t.Fatalf("mutation continued after cache failure: err=%v moved=%q deleted=%q", err, client.moved, client.deleted)
		}
	})
}

func TestMessageAttachmentCacheKeysAreAccountScoped(t *testing.T) {
	message := models.Email{Attachments: []models.Attachment{
		{ID: "opaque-one", PartID: "2.1"},
		{ID: "opaque-two", PartID: "3"},
		{ID: "opaque-one", PartID: "2.1"},
		{PartID: "4"},
	}}
	got := messageAttachmentCacheKeys("account-7", message)
	want := map[string]bool{
		"attachments/account-7/opaque-one": true,
		"attachments/account-7/opaque-two": true,
	}
	if len(got) != len(want) {
		t.Fatalf("cache keys = %q", got)
	}
	for _, key := range got {
		if !want[key] {
			t.Fatalf("unexpected cache key %q", key)
		}
	}
}

func TestFolderMessageSummaryJSONOmitsBodiesAndAttachments(t *testing.T) {
	message := models.Email{
		ID: "17", Folder: "Junk", From: "sender@example.com", FromName: "Sender",
		To: "me@example.com", Subject: "A &amp; B", Preview: "Hello &amp; goodbye",
		Body: "large plain body", HTML: "<p>large HTML body</p>",
		Date:        time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC),
		Attachments: []models.Attachment{{ID: "attachment-token", Filename: "report.pdf"}},
	}
	account := mailstore.Account{Email: "me@example.com", Label: "Personal", Color: "#123456"}

	raw, err := json.Marshal(folderMessageSummaryJSON(message, account))
	if err != nil {
		t.Fatalf("marshal folder summary: %v", err)
	}
	jsonText := string(raw)
	for _, forbidden := range []string{"large plain body", "large HTML body", "report.pdf", "attachments"} {
		if strings.Contains(jsonText, forbidden) {
			t.Fatalf("folder summary contains %q: %s", forbidden, jsonText)
		}
	}
	var decoded struct {
		Subject      string `json:"subject"`
		AccountEmail string `json:"accountEmail"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode folder summary: %v", err)
	}
	if decoded.Subject != "A & B" || decoded.AccountEmail != "me@example.com" {
		t.Fatalf("folder summary lost display metadata: %s", jsonText)
	}
}

func TestFolderMessageDetailJSONPreparesHTMLAndAttachments(t *testing.T) {
	message := models.Email{
		ID: "18", Folder: "Projects", From: "sender@example.com", To: "me@example.com",
		Subject: "Status &amp; plan", Body: "Ready &amp; waiting",
		HTML: `<p>Ready<img src="cid:logo@example.com"></p>`,
		Attachments: []models.Attachment{
			{ID: "inline-token", Filename: "logo.png", ContentType: "image/png", ContentID: "logo@example.com"},
			{ID: "file-token", Filename: "plan.pdf", ContentType: "application/pdf"},
		},
	}
	account := mailstore.Account{Email: "me@example.com", Label: "Personal"}

	got := folderMessageDetailJSON(message, account, "zh-CN")
	if got.Subject != "Status & plan" || got.Body != "Ready & waiting" {
		t.Fatalf("detail text was not decoded: subject=%q body=%q", got.Subject, got.Body)
	}
	if !strings.HasPrefix(got.HTML, "<!DOCTYPE html>") || strings.Contains(got.HTML, "cid:logo@example.com") || !strings.Contains(got.HTML, "/api/attachment/inline-token") {
		t.Fatalf("detail HTML was not prepared: %s", got.HTML)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != "file-token" || !got.HasAttachments {
		t.Fatalf("regular attachments are incorrect: %+v", got.Attachments)
	}
	if got.AccountEmail != account.Email || got.AccountLabel != account.Label {
		t.Fatalf("account metadata was not attached: %+v", got)
	}
}

func TestFolderMessageDetailJSONKeepsFlatEmailShapeWithSummary(t *testing.T) {
	response := FolderMessageDetailJSON{
		Email:       models.Email{ID: "42", Subject: "RFQ"},
		MailSummary: &MailSummaryJSON{Text: "已保存的总结", Status: "ready", Stale: true},
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	jsonText := string(raw)
	if !strings.Contains(jsonText, `"id":"42"`) || !strings.Contains(jsonText, `"subject":"RFQ"`) || !strings.Contains(jsonText, `"mailSummary":{"text":"已保存的总结","status":"ready","stale":true`) {
		t.Fatalf("unexpected detail shape: %s", jsonText)
	}
}
