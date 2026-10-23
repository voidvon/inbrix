package web

import (
	"inbrix/mailstore"

	"github.com/gofiber/fiber/v2"
)

func (h *ProductsHandler) queryPrices(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var q mailstore.ProductPriceQuery
	if err = c.BodyParser(&q); err != nil {
		return fiber.NewError(400, "Invalid JSON body")
	}
	result, err := h.mailDB.QueryProductPrices(c.UserContext(), owner, q)
	if err != nil {
		return productError(err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(result)
}
func (h *ProductsHandler) pendingPrices(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	offset, err := productInt(c, "offset", 0, 0, 100000)
	if err != nil {
		return err
	}
	limit, err := productInt(c, "limit", 30, 1, 100)
	if err != nil {
		return err
	}
	rows, total, err := h.mailDB.PendingProductPrices(c.UserContext(), owner, offset, limit)
	if err != nil {
		return productError(err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"rows": rows, "total": total, "hasMore": offset+len(rows) < total, "nextOffset": offset + len(rows)})
}
func (h *ProductsHandler) importPrice(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input struct {
		ProductID string                  `json:"productId"`
		Version   int64                   `json:"version"`
		Price     *mailstore.ProductPrice `json:"price"`
	}
	if c.BodyParser(&input) != nil {
		return fiber.NewError(400, "Invalid JSON body")
	}
	p, err := h.mailDB.ImportProductPrice(c.UserContext(), owner, c.Params("id"), input.ProductID, input.Version, input.Price)
	if err != nil {
		return productError(err)
	}
	return c.JSON(fiber.Map{"product": p})
}

func (h *ProductsHandler) listDimensions(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	rows, err := h.mailDB.ListProductDimensions(c.UserContext(), owner)
	if err != nil {
		return productError(err)
	}
	c.Set("Cache-Control", "no-store")
	return c.JSON(fiber.Map{"dimensions": rows})
}
func (h *ProductsHandler) saveDimension(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	var input mailstore.ProductDimensionRecord
	if c.BodyParser(&input) != nil {
		return fiber.NewError(400, "Invalid JSON body")
	}
	row, err := h.mailDB.SaveProductDimension(c.UserContext(), owner, c.Params("key"), input)
	if err != nil {
		return productError(err)
	}
	if c.Params("key") == "" {
		c.Status(201)
	}
	return c.JSON(fiber.Map{"dimension": row})
}
func (h *ProductsHandler) deleteDimension(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	version, err := productInt(c, "version", 0, 1, 1<<31-1)
	if err != nil {
		return err
	}
	if err = h.mailDB.DeleteProductDimension(c.UserContext(), owner, c.Params("key"), int64(version)); err != nil {
		return productError(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}
