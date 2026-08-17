package htmlsafe

import (
	"fmt"
	stdhtml "html"
	"strings"

	xhtml "golang.org/x/net/html"
)

// maxNormalizedHTMLBytes bounds HTML that is sent through the sanitizer. The
// request body limit is much larger because it also covers attachments; this
// separate limit keeps a rich-text body from consuming disproportionate CPU.
const maxNormalizedHTMLBytes = 2 * 1024 * 1024

// NormalizeHTML applies the active-content policy while preserving the caller's
// HTML structure. Do not route mail HTML through Markdown: Markdown cannot
// represent intentional blank lines, tables, inline styles, or image sizing.
func NormalizeHTML(in string) (string, error) {
	if strings.TrimSpace(in) == "" {
		return "", nil
	}
	if len(in) > maxNormalizedHTMLBytes {
		return "", fmt.Errorf("html body exceeds %d bytes", maxNormalizedHTMLBytes)
	}

	sanitized := SanitizeHTML(in)
	if strings.TrimSpace(sanitized) == "" {
		return "", nil
	}
	return sanitized, nil
}

// NormalizeComposeBodies normalizes the rich body and derives the plain-text
// alternative only when the caller did not provide one. The explicit text body
// is otherwise preserved because it may contain intentional line breaks or a
// reply quote that is not represented by the HTML editor.
func NormalizeComposeBodies(plain, richHTML string) (string, string, error) {
	normalized, err := NormalizeHTML(richHTML)
	if err != nil {
		return plain, "", err
	}
	if strings.TrimSpace(plain) == "" && normalized != "" {
		plain = PlainTextFromHTML(normalized)
	}
	return plain, normalized, nil
}

// SanitizeHTML applies the shared active-content policy without the short
// snippet cap used by signatures and draft previews. NormalizeHTML has its own
// larger, explicit input bound for compose bodies.
func SanitizeHTML(in string) string {
	if in == "" {
		return ""
	}
	return sanitize(in, maxNormalizedHTMLBytes)
}

var plainTextBlockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"dd": true, "div": true, "dl": true, "dt": true, "figcaption": true,
	"figure": true, "footer": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "header": true, "hr": true,
	"li": true, "main": true, "nav": true, "ol": true, "p": true,
	"pre": true, "section": true, "table": true, "td": true, "th": true,
	"tr": true, "ul": true,
}

// PlainTextFromHTML returns a text/plain fallback while retaining meaningful
// line breaks from <br> and block elements. It is used only when the caller did
// not provide a plain-text alternative; the rich HTML remains the source of
// truth for formatting.
func PlainTextFromHTML(in string) string {
	doc, err := xhtml.Parse(strings.NewReader(in))
	if err != nil {
		return strings.TrimSpace(stdhtml.UnescapeString(in))
	}

	var b strings.Builder
	writeLineBreak := func() {
		if b.Len() > 0 && b.String()[b.Len()-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	var visit func(*xhtml.Node, bool)
	visit = func(node *xhtml.Node, inPre bool) {
		switch node.Type {
		case xhtml.TextNode:
			text := strings.ReplaceAll(stdhtml.UnescapeString(node.Data), "\u00a0", " ")
			if !inPre && strings.TrimSpace(text) == "" {
				if !strings.ContainsAny(text, "\r\n") && text != "" {
					b.WriteByte(' ')
				}
				return
			}
			b.WriteString(text)
		case xhtml.ElementNode:
			name := strings.ToLower(node.Data)
			if name == "br" {
				b.WriteByte('\n')
				return
			}
			block := plainTextBlockElements[name]
			if block {
				writeLineBreak()
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child, inPre || name == "pre")
			}
			if block {
				writeLineBreak()
			}
		default:
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child, inPre)
			}
		}
	}
	visit(doc, false)
	return strings.TrimSpace(b.String())
}
