package web

import (
	"lilmail/i18n"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const localeCookieName = "lilmail_locale"

// CurrentLocale resolves the locale for a request without coupling mail
// handlers to the browser's Accept-Language parsing details.
func CurrentLocale(c *fiber.Ctx) string {
	if c == nil {
		return i18n.LocaleEnglish
	}
	return i18n.Detect(c.Cookies(localeCookieName), c.Get("Accept-Language"))
}

// RenderStatus returns a JSON error for compatibility with legacy handler
// methods that are no longer mounted. All browser pages are rendered by React.
func RenderStatus(c *fiber.Ctx, status int, _ string, data fiber.Map, _ ...string) error {
	message, _ := data["Error"].(string)
	if message == "" {
		message = fiber.ErrInternalServerError.Message
	}
	return c.Status(status).JSON(fiber.Map{"error": message})
}

// HandleLanguage stores the UI preference and returns the user to the page
// they were viewing. The redirect target is restricted to a local path to
// prevent this preference endpoint from becoming an open redirect.
func HandleLanguage(c *fiber.Ctx) error {
	raw := strings.TrimSpace(c.Query("locale"))
	if raw == "" {
		return c.Status(fiber.StatusBadRequest).SendString("locale is required")
	}

	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "en") && !strings.HasPrefix(lower, "zh") {
		return c.Status(fiber.StatusBadRequest).SendString("unsupported locale")
	}
	locale := i18n.Normalize(raw)

	c.Cookie(&fiber.Cookie{
		Name:     localeCookieName,
		Value:    locale,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		SameSite: "Lax",
	})

	next := c.Query("next")
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/login"
	}
	return c.Redirect(next)
}
