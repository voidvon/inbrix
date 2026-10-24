package mailstore

import (
	"strings"

	xhtml "golang.org/x/net/html"
	mailapi "inbrix/handlers/api"
	"inbrix/models"
)

type mailAnalysisContent struct {
	NewText   string `json:"newText"`
	Quoted    string `json:"quotedOrForwardedText"`
	Signature string `json:"signature"`
	FullText  string `json:"fullText"`
}

// Keep evidence intact and partition only for interpretation. Translation's
// currentMessageText intentionally has different semantics and stays separate.
func partitionMailAnalysisContent(mail models.Email) mailAnalysisContent {
	content := mailAnalysisContent{FullText: AgentMailText(mail)}
	if strings.TrimSpace(mail.HTML) != "" {
		if document, err := xhtml.Parse(strings.NewReader(mail.HTML)); err == nil {
			var quotes, signatures []string
			var walk func(*xhtml.Node)
			walk = func(parent *xhtml.Node) {
				for node := parent.FirstChild; node != nil; {
					next := node.NextSibling
					quote, signature := isQuotedHTMLNode(node), isMailSignatureNode(node)
					if quote || signature {
						var out strings.Builder
						_ = xhtml.Render(&out, node)
						text := strings.TrimSpace(mailapi.PlainTextFromHTML(out.String()))
						if quote {
							quotes = append(quotes, text)
						} else {
							signatures = append(signatures, text)
						}
						parent.RemoveChild(node)
					} else {
						walk(node)
					}
					node = next
				}
			}
			walk(document)
			var remaining strings.Builder
			_ = xhtml.Render(&remaining, document)
			content.NewText = strings.TrimSpace(mailapi.PlainTextFromHTML(remaining.String()))
			content.Quoted, content.Signature = strings.Join(quotes, "\n\n"), strings.Join(signatures, "\n\n")
			if content.NewText != "" || content.Quoted != "" || content.Signature != "" {
				return content
			}
		}
	}
	full := strings.ReplaceAll(strings.ReplaceAll(content.FullText, "\r\n", "\n"), "\r", "\n")
	content.NewText = stripTrailingQuotedText(full)
	if len(content.NewText) < len(full) {
		content.Quoted = strings.TrimSpace(full[len(content.NewText):])
	}
	if index := strings.Index("\n"+content.NewText, "\n-- \n"); index >= 0 {
		content.Signature = strings.TrimSpace(content.NewText[index:])
		content.NewText = strings.TrimSpace(content.NewText[:index])
	}
	return content
}

func isMailSignatureNode(node *xhtml.Node) bool {
	if node.Type != xhtml.ElementNode {
		return false
	}
	for _, attr := range node.Attr {
		if attr.Key == "class" || attr.Key == "id" || attr.Key == "data-smartmail" {
			value := strings.ToLower(attr.Val)
			if strings.Contains(value, "gmail_signature") || value == "signature" || strings.Contains(value, "moz-signature") {
				return true
			}
		}
	}
	return false
}
