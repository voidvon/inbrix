package mailstore

import (
	"regexp"
	"strings"

	xhtml "golang.org/x/net/html"
	"inbrix/handlers/htmlsafe"
)

var (
	quotedTextLineRe      = regexp.MustCompile(`^\s*>+\s?`)
	attributionLineRe     = regexp.MustCompile(`(?i)^\s*(?:on\s+.+\bwrote\s*:|(?:在|於)\s*.+(?:写道|寫道)\s*[:：])\s*$`)
	originalMessageLineRe = regexp.MustCompile(`(?i)^\s*(?:[-_]{2,}\s*)?(?:original(?:\s+message)?|forwarded\s+message|原始邮件|原始郵件|转发邮件|轉發郵件)\s*[-_:：]?\s*[-_]*\s*$`)
	outlookHeaderPrefixRe = regexp.MustCompile(`(?i)^\s*(?:from|发件人)\s*[:：]`)
	outlookNextHeaderRe   = regexp.MustCompile(`(?i)^\s*(?:sent|date|to|subject|发送时间|日期|收件人|主题)\s*[:：]`)
)

// currentMessageText removes the conventional trailing copy of earlier
// messages. Prefer text/plain because it most accurately reflects the MIME
// alternative the sender authored; HTML is converted only as a fallback.
func currentMessageText(messageBody, messageHTML string) string {
	plainContent := strings.TrimSpace(messageBody)
	var htmlContent string
	if strings.TrimSpace(messageHTML) != "" {
		htmlContent = strings.TrimSpace(htmlsafe.PlainTextFromHTML(removeQuotedHTMLNodes(messageHTML)))
	}
	strippedPlain := stripTrailingQuotedText(plainContent)
	strippedHTML := stripTrailingQuotedText(htmlContent)

	if strippedPlain != "" && strippedHTML != "" {
		// If plain text failed to strip quotes that HTML tags cleanly identified,
		// prefer the stripped HTML version.
		if len(strippedHTML) < len(strippedPlain) {
			return strippedHTML
		}
		return strippedPlain
	}
	if strippedPlain != "" {
		return strippedPlain
	}
	return strippedHTML
}

func stripTrailingQuotedText(text string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	for i, line := range lines {
		if quotedTextLineRe.MatchString(line) || attributionLineRe.MatchString(line) || originalMessageLineRe.MatchString(line) {
			return strings.TrimSpace(strings.Join(lines[:i], "\n"))
		}
		if outlookHeaderPrefixRe.MatchString(line) {
			// Check if any of the next 3 lines looks like an email header continuation
			for j := i + 1; j < len(lines) && j <= i+3; j++ {
				if outlookNextHeaderRe.MatchString(lines[j]) {
					return strings.TrimSpace(strings.Join(lines[:i], "\n"))
				}
			}
		}
	}
	return strings.TrimSpace(text)
}

func removeQuotedHTMLNodes(raw string) string {
	document, err := xhtml.Parse(strings.NewReader(raw))
	if err != nil {
		return raw
	}
	removeQuotedHTMLChildren(document)
	var output strings.Builder
	if err := xhtml.Render(&output, document); err != nil {
		return raw
	}
	return output.String()
}

func removeQuotedHTMLChildren(parent *xhtml.Node) {
	for child := parent.FirstChild; child != nil; {
		next := child.NextSibling
		if isQuotedHTMLNode(child) {
			parent.RemoveChild(child)
		} else {
			removeQuotedHTMLChildren(child)
		}
		child = next
	}
}

func isQuotedHTMLNode(node *xhtml.Node) bool {
	if node.Type != xhtml.ElementNode {
		return false
	}
	name := strings.ToLower(node.Data)
	if name == "blockquote" || name == "includetail" {
		return true
	}
	for _, attribute := range node.Attr {
		key := strings.ToLower(attribute.Key)
		if key == "data-inbrix-reply-quote" {
			return true
		}
		if key != "class" && key != "id" {
			continue
		}
		value := strings.ToLower(attribute.Val)
		for _, marker := range []string{
			"gmail_quote", "yahoo_quoted", "moz-cite-prefix", "protonmail_quote",
			"outlook_quote", "quoted-text", "quotedcontent", "original-message",
			"inbrix-reply-quote", "apple-orig-quote",
		} {
			if strings.Contains(value, marker) {
				return true
			}
		}
	}
	return false
}
