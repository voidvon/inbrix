package web

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"regexp"
	"strings"
)

// Some compatible gateways put DeepSeek's tool protocol in message.content
// instead of translating it to the requested JSON tool-call format.
const (
	dsmlMarker = "｜｜DSML｜｜"
	dsmlOpen   = "<" + dsmlMarker
	dsmlClose  = "</" + dsmlMarker
)

var (
	dsmlInvoke     = regexp.MustCompile(`(?s)<` + dsmlMarker + `\s+invoke\b([^>]*)>(.*?)</` + dsmlMarker + `\s+invoke\s*>`)
	dsmlParameter  = regexp.MustCompile(`(?s)<` + dsmlMarker + `\s+parameter\b([^>]*)>(.*?)</` + dsmlMarker + `\s+parameter\s*>`)
	dsmlWrapper    = regexp.MustCompile(`</?` + dsmlMarker + `\s+calls\s*>`)
	dsmlInvokeEnd  = regexp.MustCompile(`</` + dsmlMarker + `\s+invoke\s*>`)
	errInvalidDSML = errors.New("AI 返回了不完整或无效的工具调用，请重试 / Invalid AI tool call")
)

// Normalize complete invocations to the existing tool fences, preserving their
// order with any JSON calls. Reject the whole turn if any DSML is malformed:
// executing only a valid prefix could apply an unintended partial operation.
func normalizeDSMLToolCalls(output string) (string, error) {
	if dsmlPartialPrefix(output) >= len("<｜") {
		return "", errInvalidDSML
	}
	if !strings.Contains(output, dsmlMarker) {
		return output, nil
	}
	var parseErr error
	count := 0
	normalized := dsmlInvoke.ReplaceAllStringFunc(output, func(block string) string {
		match := dsmlInvoke.FindStringSubmatch(block)
		attrs, err := dsmlAttributes("invoke", match[1])
		if err != nil || strings.TrimSpace(attrs["name"]) == "" {
			parseErr = errInvalidDSML
			return ""
		}
		args := make(map[string]any)
		remainder := dsmlParameter.ReplaceAllStringFunc(match[2], func(parameter string) string {
			param := dsmlParameter.FindStringSubmatch(parameter)
			attrs, err := dsmlAttributes("parameter", param[1])
			name := attrs["name"]
			if err != nil || strings.TrimSpace(name) == "" {
				parseErr = errInvalidDSML
				return ""
			}
			if _, duplicate := args[name]; duplicate {
				parseErr = errInvalidDSML
				return ""
			}
			switch attrs["string"] {
			case "true":
				// DSML string values are raw text, not XML-escaped content.
				args[name] = param[2]
			case "false":
				var value any
				if json.Unmarshal([]byte(param[2]), &value) != nil {
					parseErr = errInvalidDSML
				}
				args[name] = value
			default:
				parseErr = errInvalidDSML
			}
			return ""
		})
		if strings.TrimSpace(remainder) != "" {
			parseErr = errInvalidDSML
		}
		encoded, err := json.Marshal(AIToolCall{Name: attrs["name"], Args: args})
		if err != nil {
			parseErr = errInvalidDSML
		}
		count++
		// Backticks in raw string parameters must not terminate the fence.
		return "\n```tool_call\n" + strings.ReplaceAll(string(encoded), "`", `\u0060`) + "\n```\n"
	})
	normalized = dsmlWrapper.ReplaceAllString(normalized, "")
	if parseErr != nil || count == 0 || strings.Contains(normalized, dsmlMarker) {
		return "", errInvalidDSML
	}
	return normalized, nil
}

// Use XML only for attributes: parameter bodies can contain literal &, <, or
// JSON and must not be decoded as XML. Reject duplicate/unknown attributes.
func dsmlAttributes(tag, raw string) (map[string]string, error) {
	decoder := xml.NewDecoder(strings.NewReader("<" + tag + raw + "/>"))
	token, err := decoder.Token()
	if err != nil {
		return nil, errInvalidDSML
	}
	start, ok := token.(xml.StartElement)
	if !ok || start.Name.Local != tag {
		return nil, errInvalidDSML
	}
	attrs := make(map[string]string)
	for _, attr := range start.Attr {
		name := attr.Name.Local
		if attr.Name.Space != "" || (name != "name" && !(tag == "parameter" && name == "string")) {
			return nil, errInvalidDSML
		}
		if _, duplicate := attrs[name]; duplicate {
			return nil, errInvalidDSML
		}
		attrs[name] = attr.Value
	}
	return attrs, nil
}

// Consume a hidden DSML region. A missing end tag withholds the rest until more
// stream data arrives; the final parser will report truncation as an error.
func consumeDSMLMarkup(raw string) (string, bool) {
	end := strings.IndexByte(raw, '>')
	if end < 0 {
		return "", false
	}
	tag := raw[:end+1]
	if dsmlWrapper.MatchString(tag) {
		return raw[end+1:], true
	}
	if strings.HasPrefix(tag, dsmlOpen) && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(tag, dsmlOpen)), "invoke") {
		if closing := dsmlInvokeEnd.FindStringIndex(raw[end+1:]); closing != nil {
			return raw[end+1+closing[1]:], true
		}
	}
	return "", false
}

func stripDSMLToolMarkup(content string) string {
	var out strings.Builder
	for content != "" {
		index := strings.Index(content, dsmlOpen)
		if closeIndex := strings.Index(content, dsmlClose); closeIndex >= 0 && (index < 0 || closeIndex < index) {
			index = closeIndex
		}
		if index < 0 {
			if keep := dsmlPartialPrefix(content); keep >= len("<｜") {
				content = content[:len(content)-keep]
			}
			out.WriteString(content)
			break
		}
		out.WriteString(content[:index])
		var ok bool
		content, ok = consumeDSMLMarkup(content[index:])
		if !ok {
			break
		}
	}
	return out.String()
}

func dsmlPartialPrefix(raw string) int {
	keep := 0
	for _, candidate := range []string{dsmlOpen, dsmlClose} {
		for n := 1; n < len(candidate); n++ {
			if strings.HasSuffix(raw, candidate[:n]) && n > keep {
				keep = n
			}
		}
	}
	return keep
}

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
