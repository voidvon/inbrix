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

// Render attaches all request-scoped localization data before delegating to
// Fiber. Every full page and HTMX partial goes through this helper so a page
// fragment cannot silently fall back to English.
func Render(c *fiber.Ctx, view string, data fiber.Map, layout ...string) error {
	if data == nil {
		data = fiber.Map{}
	}

	locale := CurrentLocale(c)
	translations := i18n.Dictionary(locale)
	data["Locale"] = locale
	data["Translations"] = translations
	data["CurrentPath"] = c.OriginalURL()

	// Common handler errors and titles use the English source text as their key.
	// Translating them here keeps error paths localized without duplicating
	// locale plumbing in every handler.
	for _, field := range []string{"Error", "Success", "Title"} {
		if value, ok := data[field].(string); ok && value != "" {
			data[field] = i18n.Translate(locale, value)
		}
	}

	return c.Render(view, data, layout...)
}

func RenderStatus(c *fiber.Ctx, status int, view string, data fiber.Map, layout ...string) error {
	if strings.Contains(c.Get(fiber.HeaderAccept), fiber.MIMEApplicationJSON) {
		message, _ := data["Error"].(string)
		if message == "" {
			message = fiber.ErrInternalServerError.Message
		}
		return c.Status(status).JSON(fiber.Map{"error": message})
	}
	c.Status(status)
	return Render(c, view, data, layout...)
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
