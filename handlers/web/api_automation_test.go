package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"inbrix/mailstore"
	"inbrix/models"
)

func TestAPIKeyAutomationDiscovery(t *testing.T) {
	f := newAPIKeyFixture(t)
	for _, path := range []string{"/api/automation", "/api/automation/reference"} {
		res, _ := keyRequest(t, f, "GET", path, "", "", nil)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("discovery exposed without authentication: %s %d", path, res.StatusCode)
		}
	}
	res, body := keyRequest(t, f, "GET", "/api/automation?path=/v1/messages/:uid&method=GET", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") || strings.Contains(body, f.key.Key) {
		t.Fatalf("catalog failed or leaked key: %d", res.StatusCode)
	}
	var catalog struct {
		Total      int  `json:"total"`
		NextOffset *int `json:"nextOffset"`
		Mailboxes  struct {
			SelectedAccountID string `json:"selectedAccountId"`
		} `json:"mailboxes"`
		Operations []struct {
			Method, Path   string
			PathParameters []string                                        `json:"pathParameters"`
			Reference      struct{ Section, TableHeader, TableRow string } `json:"reference"`
		} `json:"operations"`
	}
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Total != 1 || len(catalog.Operations) != 1 || catalog.Mailboxes.SelectedAccountID != f.account.ID || catalog.NextOffset != nil {
		t.Fatalf("incorrect filtered discovery: %s", body)
	}
	op := catalog.Operations[0]
	if op.Method != "GET" || op.Path != "/v1/messages/:uid" || len(op.PathParameters) != 1 || op.PathParameters[0] != "uid" || !strings.Contains(op.Reference.TableHeader, "Query") || !strings.Contains(op.Reference.TableRow, "folder") {
		t.Fatalf("missing route parameters or documentation: %+v", op)
	}
	res, body = keyRequest(t, f, "GET", "/api/automation/reference?section="+url.QueryEscape(op.Reference.Section), f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/markdown") || !strings.Contains(body, "DELETE /v1/messages/:uid") || strings.Contains(body, "## Demo mode") {
		t.Fatalf("section reference failed: %d", res.StatusCode)
	}
	// Fenced shell comments must not be mistaken for Markdown headings.
	res, body = keyRequest(t, f, "GET", "/api/automation?path=/api/settings/api-keys&method=POST", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"section":"Authentication"`) {
		t.Fatalf("shell comments broke section association: %s", body)
	}
	res, body = keyRequest(t, f, "GET", "/api/automation?q=calendar", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"total":0`) {
		t.Fatalf("discovery listed disabled routes: %s", body)
	}
	for _, path := range []string{"/api/automation?limit=101", "/api/automation?offset=-1", "/api/automation?limit=invalid"} {
		res, _ := keyRequest(t, f, "GET", path, f.key.Key, "", nil)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid pagination accepted: %s", path)
		}
	}
	res, body = keyRequest(t, f, "GET", "/api/automation?limit=1", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &catalog) != nil || len(catalog.Operations) != 1 || catalog.NextOffset == nil || *catalog.NextOffset != 1 || catalog.Total < 20 {
		t.Fatalf("catalog pagination failed: %s", body)
	}
	res, body = keyRequest(t, f, "GET", "/api/automation?offset=999999999", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"operations":[]`) {
		t.Fatalf("empty catalog page failed: %s", body)
	}
	res, _ = keyRequest(t, f, "GET", "/api/automation/reference?section=unknown", f.key.Key, "", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatal("unknown reference section accepted")
	}
	// Discovery remains available to users without any attached mailbox.
	if err := f.db.DeleteAccount(context.Background(), f.user.ID, f.account.ID); err != nil {
		t.Fatal(err)
	}
	res, body = keyRequest(t, f, "GET", "/api/automation", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"listEndpoint":"/api/accounts"`) {
		t.Fatalf("discovery required a mailbox: %d %s", res.StatusCode, body)
	}
}

func TestAPIKeyConcurrentMailboxReadsAndScopedWrites(t *testing.T) {
	f := newAPIKeyFixture(t)
	ctx := context.Background()
	second, err := f.db.UpsertAccount(ctx, mailstore.Account{OwnerID: f.user.ID, Email: "second@example.com", Username: "second", EncryptedPassword: "encrypted", IMAPServer: "imap.second.example.com", IMAPPort: 993})
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range []mailstore.Account{f.account, second, f.foreign} {
		if err := f.db.UpsertFolder(ctx, mailstore.Folder{AccountID: account.ID, Name: "INBOX"}); err != nil {
			t.Fatal(err)
		}
		if err := f.db.UpsertMessages(ctx, account.ID, "INBOX", []models.Email{{ID: "100", Folder: "INBOX", From: "sender@example.com", To: account.Email, Subject: account.Email, Body: "Private mail for " + account.Email, BodyCached: true, Date: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	res, body := keyRequest(t, f, "GET", "/api/accounts", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, second.ID) || strings.Contains(body, f.foreign.ID) {
		t.Fatalf("application mailbox discovery failed: %d %s", res.StatusCode, body)
	}
	// Reuse the same API key concurrently. Identical folder/UID pairs must
	// resolve to different messages according to each request's mailbox header.
	results := make(chan error, 12)
	for i := 0; i < cap(results); i++ {
		account := f.account
		if i%2 == 1 {
			account = second
		}
		go func(account mailstore.Account) {
			req := httptest.NewRequest("GET", "/api/mail/messages/100?folder=INBOX", nil)
			req.Header.Set("X-API-Key", f.key.Key)
			req.Header.Set("X-Account-ID", account.ID)
			res, err := f.app.Test(req)
			if err != nil {
				results <- err
				return
			}
			defer res.Body.Close()
			body, err := io.ReadAll(res.Body)
			if err != nil {
				results <- err
				return
			}
			var message models.Email
			if err := json.Unmarshal(body, &message); err != nil {
				results <- err
				return
			}
			if res.StatusCode != http.StatusOK || message.Body != "Private mail for "+account.Email {
				results <- fmt.Errorf("mailbox selection crossed requests: %s %d %s", account.ID, res.StatusCode, body)
				return
			}
			results <- nil
		}(account)
	}
	for i := 0; i < cap(results); i++ {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
	res, body = keyRequest(t, f, "PATCH", "/api/mail/messages/100/read?folder=INBOX", f.key.Key, "", map[string]string{"X-Account-ID": second.ID})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("selected mailbox write failed: %d %s", res.StatusCode, body)
	}
	for _, account := range []mailstore.Account{f.account, second, f.foreign} {
		message, err := f.db.GetMessage(ctx, account.ID, "INBOX", "100")
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for _, flag := range message.Flags {
			if flag == `\Seen` {
				seen = true
			}
		}
		if seen != (account.ID == second.ID) {
			t.Fatalf("write affected the wrong mailbox: %s flags=%v", account.ID, message.Flags)
		}
	}
	res, body = keyRequest(t, f, "GET", "/api/mail/messages/100?folder=INBOX", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "Private mail for "+f.account.Email) {
		t.Fatal("explicit selection changed default mailbox for later requests")
	}
	res, _ = keyRequest(t, f, "PATCH", "/api/mail/messages/100/read?folder=INBOX", f.key.Key, "", map[string]string{"X-Account-ID": f.foreign.ID})
	if res.StatusCode != http.StatusNotFound {
		t.Fatal("foreign mailbox write accepted")
	}
}
