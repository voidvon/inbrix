package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"inbrix/internal/pdfinput"
	"inbrix/mailstore"
)

const maxPreparedAttachmentBytes = 32 << 20 // Base64 content, not original PDF bytes.

type aiPDFCacheEntry struct {
	pages [][]byte
	size  int
	used  time.Time
}

// Cache only derived JPEGs, never originals. Every lookup is preceded by the
// existing owner/mailbox-scoped database read. No derivative URLs are exposed.
type aiPDFCache struct {
	mu      sync.Mutex
	entries map[string]aiPDFCacheEntry
	bytes   int
}

func (c *aiPDFCache) get(key string) [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune()
	entry, ok := c.entries[key]
	if !ok {
		return nil
	}
	entry.used = time.Now()
	c.entries[key] = entry
	return entry.pages // Immutable after publication.
}

func (c *aiPDFCache) prune() {
	for key, entry := range c.entries {
		if time.Since(entry.used) > 15*time.Minute {
			delete(c.entries, key)
			c.bytes -= entry.size
		}
	}
}

func (c *aiPDFCache) put(key string, pages [][]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune()
	if c.entries == nil {
		c.entries = make(map[string]aiPDFCacheEntry)
	}
	size := 0
	for _, page := range pages {
		size += len(page)
	}
	if size > 64<<20 {
		return
	}
	if old, ok := c.entries[key]; ok {
		c.bytes -= old.size
		delete(c.entries, key)
	}
	for len(c.entries) >= 32 || c.bytes+size > 64<<20 {
		oldest := ""
		var used time.Time
		for k, entry := range c.entries {
			if oldest == "" || entry.used.Before(used) {
				oldest, used = k, entry.used
			}
		}
		c.bytes -= c.entries[oldest].size
		delete(c.entries, oldest)
	}
	c.entries[key] = aiPDFCacheEntry{pages: pages, size: size, used: time.Now()}
	c.bytes += size
}

func (c *aiPDFCache) forget(owner, account, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := owner + "\x00" + account + "\x00" + id + "\x00"
	for key, entry := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
			c.bytes -= entry.size
		}
	}
}

type aiPlannedAttachment struct {
	source   mailstore.AIInputAttachment
	key      string
	pages    int // Zero denotes an original file/image; positive denotes rendered PDF pages.
	order    int
	required bool
}

type aiAttachmentPlan struct{ attachments []aiPlannedAttachment }

func (h *AISettingsHandler) planAttachment(ctx context.Context, owner, account, id string, source mailstore.AIInputAttachment, model mailstore.AIModelRecord) (aiPlannedAttachment, error) {
	plan := aiPlannedAttachment{source: source}
	if source.ContentType != "application/pdf" {
		return plan, model.ValidateInputAttachment(source)
	}
	routing, err := model.PDFInputRouting()
	if err != nil {
		return plan, err
	}
	if routing == "native" {
		return plan, model.ValidateInputAttachment(source)
	}
	// Include source identity and bytes; owner and mailbox prevent cross-tenant reuse.
	plan.key = fmt.Sprintf("%s\x00%s\x00%s\x00%x", owner, account, id, sha256.Sum256([]byte(source.Base64)))
	if pages := h.pdfCache.get(plan.key); pages != nil {
		plan.pages = len(pages)
		return plan, nil
	}
	data, err := base64.StdEncoding.DecodeString(source.Base64)
	if err != nil {
		return plan, errors.New("PDF 数据无效 / Invalid PDF data")
	}
	plan.pages, err = pdfinput.Default.Inspect(ctx, data)
	return plan, err
}

// Plan before saving a user turn. Current sources must all be usable; historical
// sources are admitted as whole documents, newest first, within the image budget.
func (h *AISettingsHandler) planAgentAttachments(ctx context.Context, owner, account string, messages []AIAgentChatMessage, model mailstore.AIModelRecord, projectHistory bool) (aiAttachmentPlan, error) {
	var plan aiAttachmentPlan
	count, encoded, inspected := 0, 0, 0
	for index := len(messages) - 1; index >= 0; index-- {
		required := !projectHistory || index == len(messages)-1
		message := messages[index]
		if len(message.Attachments) > 6 || message.Role != "user" && len(message.Attachments) > 0 {
			return plan, fiber.NewError(400, "仅用户消息可附附件，每条消息最多 6 个 / Up to 6 attachments per user message")
		}
		if !required && (count >= 24 || inspected >= 24) {
			break
		}
		for refIndex, ref := range message.Attachments {
			if !required && (count >= 24 || inspected >= 24) {
				break
			}
			inspected++
			stored, err := h.mailDB.GetAIChatAttachment(ctx, owner, account, ref.ID)
			if errors.Is(err, mailstore.ErrNotFound) && !required {
				continue
			}
			if errors.Is(err, mailstore.ErrNotFound) {
				return plan, fiber.NewError(400, "附件不存在或不属于当前邮箱，请重新上传 / Attachment unavailable for this mailbox")
			}
			if err != nil {
				return plan, err
			}
			source := mailstore.AIInputAttachment{
				Label:    fmt.Sprintf("User message %d, attachment %s: %s", index+1, stored.ID, stored.Filename),
				Filename: stored.Filename, ContentType: stored.ContentType, Base64: base64.StdEncoding.EncodeToString(stored.Data),
			}
			item, err := h.planAttachment(ctx, owner, account, stored.ID, source, model)
			if ctx.Err() != nil {
				return plan, ctx.Err()
			}
			if err != nil && !required {
				continue
			}
			if err != nil {
				return plan, fiber.NewError(400, err.Error())
			}
			n, size := 1, len(source.Base64)
			if item.pages > 0 {
				n, size = item.pages, 0
			} // Actual encoded image budget checked after rendering.
			if count+n > 24 || encoded+size > maxPreparedAttachmentBytes {
				if !required {
					continue
				}
				return plan, fiber.NewError(413, "附件展开后超过 24 张图片/文件或 32 MB 编码大小，请拆分发送 / Prepared attachments exceed the request budget")
			}
			item.required, item.order = required, index*6+refIndex
			plan.attachments = append(plan.attachments, item)
			count, encoded = count+n, encoded+size
		}
	}
	return plan, nil
}

func (h *AISettingsHandler) prepareAgentAttachments(ctx context.Context, plan aiAttachmentPlan, emit func(agentChatEvent) error) ([]mailstore.AIInputAttachment, error) {
	type group struct {
		order  int
		inputs []mailstore.AIInputAttachment
	}
	var groups []group
	count, encoded := 0, 0
	// The plan is newest first, ensuring historical derivatives cannot displace
	// the current question. A PDF is either included in full or omitted in full.
	for _, item := range plan.attachments {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		inputs := []mailstore.AIInputAttachment{item.source}
		if item.pages > 0 {
			progress := func(page, total int) error {
				if emit == nil {
					return nil
				}
				return emit(agentChatEvent{Type: "attachment_progress", Attachment: &aiAttachmentProgress{Filename: item.source.Filename, Page: page, Pages: total}})
			}
			pages := h.pdfCache.get(item.key)
			if pages == nil {
				data, err := base64.StdEncoding.DecodeString(item.source.Base64)
				if err != nil {
					return nil, err
				}
				pages, err = pdfinput.Default.Render(ctx, data, progress)
				if err != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					if !item.required {
						continue
					}
					return nil, fiber.NewError(400, err.Error())
				}
				h.pdfCache.put(item.key, pages)
			}
			// Check encoded size before allocating base64 copies of every page.
			pageBytes := 0
			for _, data := range pages {
				pageBytes += base64.StdEncoding.EncodedLen(len(data))
			}
			if count+len(pages) > 24 || encoded+pageBytes > maxPreparedAttachmentBytes {
				if !item.required {
					continue
				}
				return nil, fiber.NewError(413, "PDF 转换后的附件超过请求大小上限，请拆分文件 / Prepared PDF attachments exceed the request budget; split the file")
			}
			inputs = nil
			for page, data := range pages {
				inputs = append(inputs, mailstore.AIInputAttachment{
					Label:    fmt.Sprintf("%s, PDF page %d/%d (all %d pages rendered as images)", item.source.Label, page+1, len(pages), len(pages)),
					Filename: fmt.Sprintf("%s.page-%d.jpg", item.source.Filename, page+1), ContentType: "image/jpeg", Base64: base64.StdEncoding.EncodeToString(data),
				})
			}
			if err := progress(len(pages), len(pages)); err != nil {
				return nil, err
			}
		}
		size := 0
		for _, input := range inputs {
			size += len(input.Base64)
		}
		if count+len(inputs) > 24 || encoded+size > maxPreparedAttachmentBytes {
			if !item.required {
				continue
			}
			return nil, fiber.NewError(413, "PDF 转换后的附件超过请求大小上限，请拆分文件 / Prepared PDF attachments exceed the request budget; split the file")
		}
		groups = append(groups, group{item.order, inputs})
		count, encoded = count+len(inputs), encoded+size
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].order < groups[j].order })
	var inputs []mailstore.AIInputAttachment
	for _, group := range groups {
		inputs = append(inputs, group.inputs...)
	}
	return inputs, nil
}
