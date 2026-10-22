package web

import (
	"errors"
	"strconv"

	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/session"
)

// Product knowledge belongs to a user, independently of the selected mailbox.
type ProductsHandler struct{ *PriceListHandler }

func NewProductsHandler(store *session.Store, db *mailstore.Store) *ProductsHandler {
	return &ProductsHandler{NewPriceListHandler(store, db)}
}

func (h *ProductsHandler) Register(router fiber.Router) {
	router.Get("/product-categories", h.listCategories)
	router.Post("/product-categories", h.saveCategory)
	router.Put("/product-categories/:id", h.saveCategory)
	router.Delete("/product-categories/:id", h.deleteCategory)
	router.Get("/products", h.search)
	router.Get("/products/search", h.search)
	router.Post("/products", h.save)
	router.Get("/products/:id/read", h.read)
	router.Get("/products/:id", h.get)
	router.Put("/products/:id", h.save)
	router.Delete("/products/:id", h.delete)
	router.Get("/product-images/:id", h.image)
}

func productError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, mailstore.ErrNotFound):
		return fiber.NewError(404, "Product, image or category not found")
	case errors.Is(err, mailstore.ErrProductConflict):
		return fiber.NewError(409, err.Error())
	case errors.Is(err, mailstore.ErrProductInvalid):
		return fiber.NewError(400, err.Error())
	default:
		return fiber.ErrInternalServerError
	}
}

func productInt(c *fiber.Ctx, key string, defaultValue, minValue, maxValue int) (int, error) {
	value := c.Query(key)
	if value == "" {
		if defaultValue < minValue {
			return 0, fiber.NewError(400, "Missing "+key)
		}
		return defaultValue, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < minValue || n > maxValue {
		return 0, fiber.NewError(400, "Invalid "+key)
	}
	return n, nil
}

func (h *ProductsHandler) listCategories(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	items, err := h.mailDB.ListProductCategories(c.UserContext(), owner)
	if err != nil {
		return productError(err)
	}
	return c.JSON(fiber.Map{"categories": items})
}

func (h *ProductsHandler) saveCategory(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var in struct {
		Name string `json:"name"`
	}
	if c.BodyParser(&in) != nil {
		return fiber.NewError(400, "Invalid JSON body")
	}
	item, err := h.mailDB.SaveProductCategory(c.UserContext(), owner, c.Params("id"), in.Name)
	if err != nil {
		return productError(err)
	}
	return c.JSON(fiber.Map{"category": item})
}

func (h *ProductsHandler) deleteCategory(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	if err = h.mailDB.DeleteProductCategory(c.UserContext(), owner, c.Params("id")); err != nil {
		return productError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *ProductsHandler) search(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	limit, err := productInt(c, "limit", 10, 1, 20)
	if err != nil {
		return err
	}
	offset, err := productInt(c, "offset", 0, 0, 100000)
	if err != nil {
		return err
	}
	page, err := h.mailDB.SearchProducts(c.UserContext(), owner, c.Query("q"), c.Query("categoryId"), limit, offset)
	if err != nil {
		return productError(err)
	}
	return c.JSON(page)
}

func (h *ProductsHandler) get(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	p, err := h.mailDB.GetProduct(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return productError(err)
	}
	return c.JSON(fiber.Map{"product": p})
}

func (h *ProductsHandler) read(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	c.Set("Cache-Control", "no-store")
	offset, err := productInt(c, "offset", 0, 0, mailstore.MaxProductSpecificationRunes)
	if err != nil {
		return err
	}
	length, err := productInt(c, "length", 2400, 1, 4000)
	if err != nil {
		return err
	}
	version, err := productInt(c, "version", 0, 0, 1<<31-1)
	if err != nil {
		return err
	}
	result, err := h.mailDB.ReadProduct(c.UserContext(), owner, c.Params("id"), int64(version), offset, length)
	if err != nil {
		return productError(err)
	}
	return c.JSON(result)
}

func (h *ProductsHandler) save(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var in mailstore.ProductInput
	if c.BodyParser(&in) != nil {
		return fiber.NewError(400, "Invalid JSON body")
	}
	p, err := h.mailDB.SaveProduct(c.UserContext(), owner, c.Params("id"), in)
	if err != nil {
		return productError(err)
	}
	if c.Params("id") == "" {
		c.Status(201)
	}
	return c.JSON(fiber.Map{"product": p})
}

func (h *ProductsHandler) delete(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	version, err := productInt(c, "version", 0, 1, 1<<31-1)
	if err != nil {
		return err
	}
	if err = h.mailDB.DeleteProduct(c.UserContext(), owner, c.Params("id"), int64(version)); err != nil {
		return productError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *ProductsHandler) image(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	data, mime, err := h.mailDB.GetProductImage(c.UserContext(), owner, c.Params("id"))
	if err != nil {
		return productError(err)
	}
	c.Set("Content-Type", mime)
	c.Set("Cache-Control", "private, no-store")
	c.Set("X-Content-Type-Options", "nosniff")
	return c.Send(data)
}
