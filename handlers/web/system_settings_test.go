package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"lilmail/mailstore"

	"github.com/gofiber/fiber/v2"
)

func TestSystemSettingsRequireSuperAdminAndManageRoles(t *testing.T) {
	db, err := mailstore.Open(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	admin, err := db.CreateUser(ctx, "admin", "Administrator", "hash")
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := db.CreateUser(ctx, "member", "Member", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole(ctx, admin.ID, mailstore.RoleSuperAdmin); err != nil {
		t.Fatal(err)
	}

	actorID := ordinary.ID
	handler := NewSystemSettingsHandler(db, "test-version")
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", actorID)
		return c.Next()
	})
	app.Get("/api/system/settings", handler.RequireSuperAdmin, handler.HandleGet)
	app.Get("/api/public/settings", handler.HandleRegistrationStatus)
	app.Patch("/api/system/settings/registration", handler.RequireSuperAdmin, handler.HandleUpdateRegistration)
	app.Patch("/api/system/users/:id/role", handler.RequireSuperAdmin, handler.HandleUpdateUserRole)

	response, err := app.Test(httptest.NewRequest("GET", "/api/system/settings", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("ordinary user status = %d, want 403", response.StatusCode)
	}

	actorID = admin.ID
	response, err = app.Test(httptest.NewRequest("GET", "/api/system/settings", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("super admin status = %d, want 200", response.StatusCode)
	}
	var settings struct {
		Version          string           `json:"version"`
		CurrentUserID    string           `json:"currentUserId"`
		RegistrationOpen bool             `json:"registrationOpen"`
		Users            []map[string]any `json:"users"`
	}
	if err := json.NewDecoder(response.Body).Decode(&settings); err != nil {
		t.Fatal(err)
	}
	if settings.Version != "test-version" || settings.CurrentUserID != admin.ID || !settings.RegistrationOpen || len(settings.Users) != 2 {
		t.Fatalf("unexpected system settings: %+v", settings)
	}
	for _, user := range settings.Users {
		if _, leaked := user["passwordHash"]; leaked {
			t.Fatal("system settings leaked a password hash")
		}
	}

	request := httptest.NewRequest("PATCH", "/api/system/settings/registration", strings.NewReader(`{"registrationOpen":false}`))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err = app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("registration setting status = %d, want 200", response.StatusCode)
	}
	registrationOpen, err := db.RegistrationOpen(ctx)
	if err != nil || registrationOpen {
		t.Fatalf("registration setting = %t, err=%v", registrationOpen, err)
	}

	authHandler := NewAuthHandler(nil, nil)
	authHandler.SetMailMirror(db, nil)
	registrationApp := fiber.New()
	registrationApp.Post("/register", authHandler.HandleRegister)
	response, err = registrationApp.Test(httptest.NewRequest("POST", "/register", nil))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusForbidden {
		t.Fatalf("closed registration status = %d, want 403", response.StatusCode)
	}

	request = httptest.NewRequest("PATCH", "/api/system/users/"+ordinary.ID+"/role", strings.NewReader(`{"role":"super_admin"}`))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err = app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK {
		t.Fatalf("promote status = %d, want 200", response.StatusCode)
	}
	updated, err := db.GetUser(ctx, ordinary.ID)
	if err != nil || updated.Role != mailstore.RoleSuperAdmin {
		t.Fatalf("user was not promoted: user=%+v err=%v", updated, err)
	}

	request = httptest.NewRequest("PATCH", "/api/system/users/"+admin.ID+"/role", strings.NewReader(`{"role":"user"}`))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err = app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("self-demotion status = %d, want 400", response.StatusCode)
	}
	unchanged, err := db.GetUser(ctx, admin.ID)
	if err != nil || unchanged.Role != mailstore.RoleSuperAdmin {
		t.Fatalf("self-demotion changed role: user=%+v err=%v", unchanged, err)
	}
}
