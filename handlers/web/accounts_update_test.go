package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"inbrix/config"

	"github.com/gofiber/fiber/v2"
)

func TestHandleUpdateAccountPreservesPasswordForMetadataEdit(t *testing.T) {
	accountStore, _ := openTestAccountStore(t)
	const owner = "owner@example.com"
	const email = "mailbox@example.com"
	original := AccountEntry{
		Email:             email,
		Label:             "Old label",
		Color:             "#111111",
		IMAPServer:        "imap.example.com",
		IMAPPort:          993,
		SMTPServer:        "smtp.example.com",
		SMTPPort:          587,
		EncryptedPassword: "encrypted-password",
	}
	if err := accountStore.Save(owner, original); err != nil {
		t.Fatal(err)
	}
	handler := NewAccountsHandler(nil, &config.Config{}, nil, accountStore)
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("username", owner)
		return c.Next()
	})
	app.Put("/api/accounts/:email", handler.HandleUpdateAccount)
	body := `{"password":"","label":"New label","color":"#abcdef","imap_server":"imap.example.com","imap_port":993,"smtp_server":"smtp.example.com","smtp_port":587}`
	request := httptest.NewRequest(http.MethodPut, "/api/accounts/mailbox%40example.com", strings.NewReader(body))
	request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	updated, err := accountStore.Get(owner, email)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Label != "New label" || updated.Color != "#abcdef" {
		t.Fatalf("metadata was not updated: %+v", updated)
	}
	if updated.EncryptedPassword != original.EncryptedPassword {
		t.Fatal("empty password replaced the stored credential")
	}
}
