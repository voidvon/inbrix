package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v2"
	_ "golang.org/x/image/webp"
	"inbrix/mailstore"
)

func (h *AISettingsHandler) attachmentAccount(c *fiber.Ctx, owner string) (mailstore.Account, error) {
	email := strings.TrimSpace(c.Query("accountEmail"))
	if c.Method() == "POST" {
		email = strings.TrimSpace(c.FormValue("accountEmail"))
	}
	if email == "" {
		return mailstore.Account{}, fiber.NewError(400, "accountEmail is required")
	}
	account, err := h.mailDB.GetAccountByEmail(c.UserContext(), owner, email)
	if errors.Is(err, mailstore.ErrNotFound) {
		return account, fiber.NewError(404, "mail account not found")
	}
	if err != nil {
		return account, fiber.ErrInternalServerError
	}
	return account, nil
}

// The server inspects the actual bytes; client filenames and MIME headers are not trusted.
func validateAIImage(data []byte) (string, error) {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return "", fiber.NewError(400, "仅支持 PNG、JPEG、WebP 图片 / Unsupported image type")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return "", fiber.NewError(400, "图片无效或超过 4000 万像素 / Invalid or oversized image")
	}
	if contentType == "image/webp" && len(data) > 20 && string(data[12:16]) == "VP8X" && data[20]&2 != 0 {
		return "", fiber.NewError(400, "请上传静态 WebP 图片 / Animated WebP is not supported")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return "", fiber.NewError(400, "图片文件损坏 / Corrupt image file")
	}
	return contentType, nil
}

func (h *AISettingsHandler) HandleUploadAIChatAttachment(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	account, err := h.attachmentAccount(c, owner)
	if err != nil {
		return err
	}
	file, err := c.FormFile("file")
	if err != nil {
		return fiber.NewError(400, "multipart file field is required")
	}
	if file.Size <= 0 || file.Size > mailstore.MaxAIChatAttachmentBytes {
		return fiber.NewError(413, "每个附件最大 10 MB / Attachment exceeds 10 MB")
	}
	src, err := file.Open()
	if err != nil {
		return fiber.NewError(400, "could not read attachment")
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, mailstore.MaxAIChatAttachmentBytes+1))
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if len(data) > mailstore.MaxAIChatAttachmentBytes {
		return fiber.NewError(413, "每个附件最大 10 MB")
	}
	contentType, err := validateAIAttachment(file.Filename, data)
	if err != nil {
		return err
	}
	filename := filepath.Base(strings.ReplaceAll(file.Filename, "\\", "/"))
	if filename == "" || len(filename) > 240 {
		extension := strings.ToLower(filepath.Ext(filename))
		if len(extension) > 10 {
			extension = ""
		}
		filename = "attachment" + extension
	}
	attachment, err := h.mailDB.CreateAIChatAttachment(c.UserContext(), owner, account.ID, filename, contentType, data)
	if err != nil {
		return fiber.NewError(400, err.Error())
	}
	return c.Status(201).JSON(attachment)
}

func (h *AISettingsHandler) HandleAIChatAttachment(c *fiber.Ctx) error {
	owner, err := h.ready(c)
	if err != nil {
		return err
	}
	account, err := h.attachmentAccount(c, owner)
	if err != nil {
		return err
	}
	if c.Method() == "DELETE" {
		err = h.mailDB.DeleteAIChatAttachment(c.UserContext(), owner, account.ID, c.Params("id"))
		if errors.Is(err, mailstore.ErrNotFound) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return chatStoreError(err)
		}
		h.pdfCache.forget(owner, account.ID, c.Params("id"))
		return c.SendStatus(204)
	}
	attachment, err := h.mailDB.GetAIChatAttachment(c.UserContext(), owner, account.ID, c.Params("id"))
	if errors.Is(err, mailstore.ErrNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	c.Set("Content-Type", attachment.ContentType)
	disposition := "attachment"
	if strings.HasPrefix(attachment.ContentType, "image/") {
		disposition = "inline"
	}
	c.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": attachment.Filename}))
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Cache-Control", "private, no-store")
	return c.Send(attachment.Data)
}

func (h *AISettingsHandler) resolveAgentAttachments(ctx context.Context, owner, account string, messages []AIAgentChatMessage) ([]mailstore.AIInputAttachment, error) {
	var images []mailstore.AIInputAttachment
	total := 0
	for index, message := range messages {
		if len(message.Attachments) > 6 || (message.Role != "user" && len(message.Attachments) > 0) {
			return nil, fiber.NewError(400, "仅用户消息可附附件，每条消息最多 6 个 / Up to 6 attachments per user message")
		}
		for _, ref := range message.Attachments {
			if len(images) >= 24 {
				return nil, fiber.NewError(413, "对话附件过多，请新建对话 / Start a new chat for more attachments")
			}
			attachment, err := h.mailDB.GetAIChatAttachment(ctx, owner, account, ref.ID)
			if errors.Is(err, mailstore.ErrNotFound) {
				return nil, fiber.NewError(400, "附件不存在或不属于当前邮箱，请重新上传 / Attachment unavailable for this mailbox")
			}
			if err != nil {
				return nil, fiber.ErrInternalServerError
			}
			total += attachment.Size
			if total > 32<<20 {
				return nil, fiber.NewError(413, "对话附件总量超过 32 MB，请新建对话 / Chat attachments exceed 32 MB")
			}
			images = append(images, mailstore.AIInputAttachment{Label: fmt.Sprintf("User message %d, attachment %s: %s", index+1, attachment.ID, attachment.Filename), Filename: attachment.Filename, ContentType: attachment.ContentType, Base64: base64.StdEncoding.EncodeToString(attachment.Data)})
		}
	}
	return images, nil
}

// Compatibility helper for callers that need the complete prepared projection.
// The chat handler separates planning from rendering to support streamed progress.
func (h *AISettingsHandler) resolveAgentContextAttachments(ctx context.Context, owner, account string, messages []AIAgentChatMessage, model mailstore.AIModelRecord) ([]mailstore.AIInputAttachment, error) {
	plan, err := h.planAgentAttachments(ctx, owner, account, messages, model, true)
	if err != nil {
		return nil, err
	}
	return h.prepareAgentAttachments(ctx, plan, nil)
}
