package web_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"inbrix/config"
	"inbrix/handlers/api"
	"inbrix/handlers/jsonapi"
	"inbrix/handlers/web"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/csrf"
	"github.com/gofiber/fiber/v2/middleware/session"
)

type apiKeyFixture struct {
	app              *fiber.App
	db               *mailstore.Store
	user, other      mailstore.User
	account, foreign mailstore.Account
	key, otherKey    mailstore.APIKeyWithSecret
}

func newAPIKeyFixture(t *testing.T) apiKeyFixture {
	t.Helper()
	f := apiKeyFixture{}
	var err error
	f.db, err = mailstore.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	ctx := context.Background()
	f.user, err = f.db.CreateUser(ctx, "api-alice", "Alice", "hash")
	if err != nil {
		t.Fatal(err)
	}
	f.other, err = f.db.CreateUser(ctx, "api-bob", "Bob", "hash")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		user mailstore.User
		out  *mailstore.Account
	}{{f.user, &f.account}, {f.other, &f.foreign}} {
		*target.out, err = f.db.UpsertAccount(ctx, mailstore.Account{OwnerID: target.user.ID, Email: target.user.Login + "@example.com", Username: target.user.Login, EncryptedPassword: "encrypted", IMAPServer: "imap.example.com", IMAPPort: 993, SMTPServer: "smtp.example.com", SMTPPort: 465, IsDefault: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	f.key, err = f.db.CreateAPIKey(ctx, f.user.ID, "Alice automation", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	f.otherKey, err = f.db.CreateAPIKey(ctx, f.other.ID, "Bob automation", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	store := session.New()
	cfg := &config.Config{}
	cfg.JWT.Secret = "api-key-test-jwt-secret"
	auth := web.NewAuthHandler(store, cfg)
	auth.SetMailMirror(f.db, nil)
	f.app = fiber.New()
	f.app.Get("/test-login", func(c *fiber.Ctx) error {
		sess, err := store.Get(c)
		if err != nil {
			return err
		}
		sess.Set("authenticated", true)
		sess.Set("user_id", f.other.ID)
		sess.Set("username", f.other.Login)
		sess.Set("email", f.foreign.Email)
		sess.Set("account_id", f.foreign.ID)
		return sess.Save()
	})
	csrfGuard := csrf.New(csrf.Config{KeyLookup: "header:X-CSRF-Token", CookieName: "_csrf", Next: func(c *fiber.Ctx) bool { return api.IsAPIKeyRequest(c) || jsonapi.IsBrokerRequest(c) }})
	jsonapi.New(store, cfg, auth).RegisterWithMiddleware(f.app, auth.APIKeyMiddleware(), csrfGuard)
	g := f.app.Group("/api", auth.APIKeyMiddleware(), api.SessionMiddleware(store), csrfGuard)
	web.NewAPIKeysHandler(f.db).Register(g)
	web.NewAISettingsHandler(store, cfg, f.db).RegisterSkillRoutes(g.Group("/settings/ai"))
	g.Get("/settings/ai/mail-tasks/runs", web.NewAISettingsHandler(store, cfg, f.db).HandleListMailTaskRuns)
	web.NewProductsHandler(store, f.db).Register(g)
	reference, err := os.ReadFile("../../docs/API.md")
	if err != nil {
		t.Fatal(err)
	}
	api.NewAutomationHandler(store, string(reference)).Register(g)
	system := web.NewSystemSettingsHandler(f.db, "test")
	g.Get("/system/settings", system.RequireSuperAdmin, system.HandleGet)
	g.Get("/whoami", func(c *fiber.Ctx) error {
		sess, err := api.RequestSession(c, store)
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"user": sess.Get("user_id"), "email": sess.Get("email"), "account": sess.Get("account_id")})
	})
	g.Post("/save-session", func(c *fiber.Ctx) error {
		sess, err := api.RequestSession(c, store)
		if err != nil {
			return err
		}
		sess.Set("marker", "request-only")
		if err := sess.Save(); err != nil {
			return err
		}
		// Reads after Save must see changes within this request.
		sess, err = api.RequestSession(c, store)
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"marker": sess.Get("marker")})
	})
	webAccounts := web.NewAccountsHandler(store, cfg, auth, nil)
	webAccounts.SetMailMirror(f.db, nil)
	g.Get("/accounts", webAccounts.HandleListAccounts)
	email := web.NewEmailHandler(store, cfg, auth)
	email.SetMailMirror(f.db)
	g.Get("/mail/messages/:uid", email.HandleLocalFolderMessageJSON)
	g.Patch("/mail/messages/:uid/read", email.HandleLocalFolderMessageReadJSON)
	documents := web.NewDocumentsHandler(store, f.db)
	g.Get("/documents", documents.HandleListDocuments)
	g.Post("/documents", documents.HandleSaveDocument)
	g.Get("/documents/:id", documents.HandleGetDocument)
	return f
}

func keyRequest(t *testing.T, f apiKeyFixture, method, path, token, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Accept", "application/json")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-API-Key", token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	res, err := f.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res, string(raw)
}

func TestAPIKeyAuthenticationAndLivePermissions(t *testing.T) {
	f := newAPIKeyFixture(t)
	ctx := context.Background()
	res, _ := keyRequest(t, f, "GET", "/api/accounts", "", "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("unauthenticated request accepted")
	}
	for _, headers := range []map[string]string{{"X-API-Key": f.key.Key}, {"Authorization": "Bearer " + f.key.Key}, {"Authorization": "bEaReR " + f.key.Key}} {
		res, body := keyRequest(t, f, "GET", "/api/accounts", "", "", headers)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, f.account.Email) || strings.Contains(body, f.foreign.Email) {
			t.Fatalf("owner-scoped account access failed: %d %s", res.StatusCode, body)
		}
	}
	res, body := keyRequest(t, f, "GET", "/v1/me", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, f.account.Email) || !strings.Contains(body, f.user.Login) {
		t.Fatalf("/v1 did not inherit key identity: %d %s", res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, "GET", "/api/system/settings", f.key.Key, "", nil)
	if res.StatusCode != http.StatusForbidden {
		t.Fatal("ordinary user's key gained super-admin permissions")
	}
	if err := f.db.SetUserRole(ctx, f.user.ID, mailstore.RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}
	res, body = keyRequest(t, f, "GET", "/api/system/settings", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, f.user.ID) {
		t.Fatalf("key did not inherit promotion: %d %s", res.StatusCode, body)
	}
	if err := f.db.SetUserRole(ctx, f.user.ID, mailstore.RoleUser); err != nil {
		t.Fatal(err)
	}
	res, _ = keyRequest(t, f, "GET", "/api/system/settings", f.key.Key, "", nil)
	if res.StatusCode != http.StatusForbidden {
		t.Fatal("key retained super-admin permissions after demotion")
	}
	res, _ = keyRequest(t, f, "GET", "/api/whoami", f.key.Key, "", map[string]string{"X-Account-ID": f.foreign.ID})
	if res.StatusCode != http.StatusNotFound {
		t.Fatal("foreign mailbox selection was accepted")
	}
	res, body = keyRequest(t, f, "GET", "/api/whoami", f.key.Key, "", map[string]string{"X-Account-ID": f.account.ID})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, f.account.ID) {
		t.Fatalf("owned mailbox selection failed: %d %s", res.StatusCode, body)
	}
}

func TestAPIKeyLifecycleThroughHTTP(t *testing.T) {
	f := newAPIKeyFixture(t)
	res, body := keyRequest(t, f, "POST", "/api/settings/api-keys", f.key.Key, `{"name":"automation","ownerId":"`+f.other.ID+`","role":"super_admin"}`, nil)
	if res.StatusCode != http.StatusCreated || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("create failed: %d %s", res.StatusCode, body)
	}
	var created mailstore.APIKeyWithSecret
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}
	res, body = keyRequest(t, f, "GET", "/api/whoami", created.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, f.user.ID) || strings.Contains(body, f.other.ID) {
		t.Fatal("caller-controlled owner changed the new key's identity")
	}
	res, body = keyRequest(t, f, "GET", "/api/settings/api-keys", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || strings.Contains(body, created.Key) || strings.Contains(body, f.otherKey.Prefix) || !strings.Contains(body, created.Prefix) {
		t.Fatal("key list leaked secrets or another owner's keys")
	}
	for _, operation := range []struct{ method, suffix string }{{"POST", "/rotate"}, {"POST", "/revoke"}, {"GET", "/events"}} {
		res, _ = keyRequest(t, f, operation.method, "/api/settings/api-keys/"+created.ID+operation.suffix, f.otherKey.Key, "", nil)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("cross-owner key operation %s: %d", operation.suffix, res.StatusCode)
		}
	}
	res, body = keyRequest(t, f, "POST", "/api/settings/api-keys/"+created.ID+"/rotate", f.key.Key, "", nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("rotate failed: %d %s", res.StatusCode, body)
	}
	var rotated mailstore.APIKeyWithSecret
	if err := json.Unmarshal([]byte(body), &rotated); err != nil {
		t.Fatal(err)
	}
	res, _ = keyRequest(t, f, "GET", "/api/whoami", created.Key, "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("old key accepted after rotation")
	}
	res, _ = keyRequest(t, f, "GET", "/api/whoami", rotated.Key, "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatal("new key rejected")
	}
	res, _ = keyRequest(t, f, "POST", "/api/settings/api-keys/"+rotated.ID+"/revoke", rotated.Key, `{"reason":"done"}`, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatal("key cannot exercise the owner's revoke permission")
	}
	res, _ = keyRequest(t, f, "GET", "/api/whoami", rotated.Key, "", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("revoked key accepted")
	}
	res, body = keyRequest(t, f, "GET", "/api/settings/api-keys/"+rotated.ID+"/events", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"revoked"`) || !strings.Contains(body, `"reason":"done"`) {
		t.Fatalf("audit missing: %d %s", res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, "POST", "/api/settings/api-keys", f.key.Key, `{"name":"bad expiration","expiresAt":"yesterday"}`, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatal("invalid expiration accepted")
	}
}

func TestAPIKeysDoNotCreateOrOverwriteBrowserSessionsAndKeepCSRF(t *testing.T) {
	f := newAPIKeyFixture(t)
	res, body := keyRequest(t, f, "POST", "/api/save-session", f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"marker":"request-only"`) || len(res.Cookies()) != 0 || res.Header.Get("X-Inbrix-Request-Session") != "" {
		t.Fatalf("key session leaked or Save broke request-local data: %d %s", res.StatusCode, body)
	}
	login, _ := keyRequest(t, f, "GET", "/test-login", "", "", nil)
	var cookie string
	for _, c := range login.Cookies() {
		cookie += c.Name + "=" + c.Value + "; "
	}
	if cookie == "" {
		t.Fatal("browser fixture did not establish a session")
	}
	res, body = keyRequest(t, f, "GET", "/api/whoami", f.key.Key, "", map[string]string{"Cookie": cookie})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, f.user.ID) || strings.Contains(body, f.other.ID) {
		t.Fatal("API key did not take precedence over a different browser identity")
	}
	res, body = keyRequest(t, f, "GET", "/api/whoami", "", "", map[string]string{"Cookie": cookie})
	if res.StatusCode != http.StatusOK || !strings.Contains(body, f.other.ID) {
		t.Fatal("key request overwrote the browser's session")
	}
	var csrfToken string
	for _, c := range res.Cookies() {
		if c.Name == "_csrf" {
			csrfToken = c.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("browser GET did not issue a CSRF token")
	}
	res, body = keyRequest(t, f, "POST", "/api/settings/api-keys", "", `{"name":"browser-created"}`, map[string]string{"Cookie": cookie + "_csrf=" + csrfToken, "X-CSRF-Token": csrfToken})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("browser creation with a valid CSRF token failed: %d %s", res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, "GET", "/api/whoami", "invalid-key", "", map[string]string{"Cookie": cookie})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("explicit invalid key fell back to a valid cookie")
	}
	for _, path := range []string{"/api/settings/api-keys", "/v1/settings/signatures"} {
		method := "POST"
		if strings.HasPrefix(path, "/v1/") {
			method = "PUT"
		}
		res, _ = keyRequest(t, f, method, path, "", `{"name":"forged"}`, map[string]string{"Cookie": cookie})
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("cookie mutation bypassed CSRF at %s: %d", path, res.StatusCode)
		}
	}
	if err := f.db.RevokeAPIKey(context.Background(), f.user.ID, f.key.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	res, _ = keyRequest(t, f, "GET", "/api/whoami", f.key.Key, "", map[string]string{"Cookie": cookie})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("revoked key fell back to an unrelated browser cookie")
	}
}

func TestAPIKeyCanReadAndWriteExistingOwnerScopedData(t *testing.T) {
	f := newAPIKeyFixture(t)
	payload, _ := json.Marshal(map[string]string{"type": "quotation", "name": "API-created document", "html": commercialAPITestCanvas()})
	res, body := keyRequest(t, f, "POST", "/api/documents", f.key.Key, string(payload), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("existing write API failed with a key: %d %s", res.StatusCode, body)
	}
	var saved struct {
		Document mailstore.DocumentRecord `json:"document"`
	}
	if err := json.Unmarshal([]byte(body), &saved); err != nil || saved.Document.ID == "" {
		t.Fatalf("saved document missing: %v", err)
	}
	res, body = keyRequest(t, f, "GET", "/api/documents/"+saved.Document.ID, f.key.Key, "", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "API-created document") {
		t.Fatalf("existing read API failed with a key: %d %s", res.StatusCode, body)
	}
	res, _ = keyRequest(t, f, "GET", "/api/documents/"+saved.Document.ID, f.otherKey.Key, "", nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatal("key bypassed an existing document ownership check")
	}
	res, body = keyRequest(t, f, "GET", "/api/documents", f.otherKey.Key, "", nil)
	if res.StatusCode != http.StatusOK || strings.Contains(body, saved.Document.ID) {
		t.Fatal("key accessed another user's document list")
	}
}
