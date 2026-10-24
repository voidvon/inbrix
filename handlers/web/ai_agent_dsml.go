package web

import (
	"inbrix/internal/agentruntime"
	"strings"
)

const dsmlMarker = "｜｜DSML｜｜"

func consumeDSMLMarkup(raw string) (string, bool) { return agentruntime.ConsumeDSMLMarkup(raw) }
func stripDSMLToolMarkup(raw string) string       { return agentruntime.StripDSMLToolMarkup(raw) }
func dsmlPartialPrefix(raw string) int            { return agentruntime.DSMLPartialPrefix(raw) }

// Clean the read model rather than rewriting stored conversations. Keep user
// messages, document cards, source references and executed tool metadata intact.
func sanitizeDSMLChatMessages(messages []any) {
	clean := func(text string) string {
		if !strings.Contains(text, dsmlMarker) && dsmlPartialPrefix(text) < len("<｜") {
			return text
		}
		text = strings.TrimSpace(stripDSMLToolMarkup(text))
		if text == "" {
			return "此前的 AI 回复未完成，请重新发送该问题。 / Previous AI reply was incomplete; please resend your question."
		}
		return text
	}
	for _, value := range messages {
		message, ok := value.(map[string]any)
		if !ok || message["role"] != "assistant" {
			continue
		}
		if content, ok := message["content"].(string); ok {
			message["content"] = clean(content)
		}
		if parts, ok := message["parts"].([]any); ok {
			for _, value := range parts {
				if part, ok := value.(map[string]any); ok && part["type"] == "text" {
					if content, ok := part["content"].(string); ok {
						part["content"] = clean(content)
					}
				}
			}
		}
	}
}
