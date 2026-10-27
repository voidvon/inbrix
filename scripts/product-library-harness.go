//go:build ignore

// An isolated, loopback-only backend for check-product-library.mjs.
// Run: go run scripts/product-library-harness.go
// In another terminal: VITE_BACKEND_URL=http://127.0.0.1:3098 npm run dev -- --host 127.0.0.1 --port 3097
// Then: node scripts/check-product-library.mjs
// All records are disposable and deleted when the harness exits.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"inbrix/handlers/web"
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

func main() {
	dir, err := os.MkdirTemp("", "inbrix-products-check-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	db, err := mailstore.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	user, err := db.CreateUser(context.Background(), "product-ui-test", "Product UI Test", "unused")
	if err != nil {
		log.Fatal(err)
	}
	legacy, _, err := db.CreatePriceList(context.Background(), user.ID, "旧阀门价格", "", []mailstore.PriceColumn{
		{Key: "model", Label: "型号", Type: "text", Role: "model"}, {Key: "name", Label: "名称", Type: "text", Role: "name"}, {Key: "connection", Label: "连接方式", Type: "text"}, {Key: "price", Label: "单价", Type: "number", Role: "price"}, {Key: "currency", Label: "币种", Type: "text", Role: "currency"}, {Key: "unit", Label: "单位", Type: "text", Role: "unit"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err = db.SavePriceRows(context.Background(), user.ID, legacy.ID, []mailstore.PriceRow{{Data: map[string]string{"model": "LEGACY-50", "name": "待关联球阀", "connection": "法兰", "price": "88.50", "currency": "CNY", "unit": "台"}}}, nil); err != nil {
		log.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.Use(func(c *fiber.Ctx) error { c.Locals("user_id", user.ID); return c.Next() })
	app.Get("/csrf", func(c *fiber.Ctx) error {
		c.Cookie(&fiber.Cookie{Name: "_csrf", Value: "isolated-product-test"})
		return c.JSON(fiber.Map{"token": "isolated-product-test"})
	})
	app.Get("/api/conversations", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"locale": "zh-CN", "folders": []any{}, "accounts": []any{}, "accountEmail": "test@example.com", "conversations": []any{}, "revision": 1})
	})
	app.Get("/api/capabilities", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"calendar": false, "notifications": false, "webPush": false, "productTestHarness": true})
	})
	app.Get("/events", func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/event-stream")
		return c.SendString(": isolated test\n\n")
	})
	web.NewProductsHandler(nil, db).Register(app.Group("/api"))
	documents := web.NewDocumentsHandler(nil, db)
	app.Get("/api/documents", documents.HandleListDocuments)
	app.Post("/api/documents", documents.HandleSaveDocument)
	app.Post("/api/documents/validate", documents.HandleValidateDocument)
	app.Get("/api/documents/:id", documents.HandleGetDocument)
	app.Get("/api/document-templates", documents.HandleListTemplates)
	app.Get("/api/document-stamps", documents.HandleListStamps)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() { <-ctx.Done(); _ = app.Shutdown() }()
	if err := app.Listen("127.0.0.1:3098"); err != nil {
		log.Print(err)
	}
}
