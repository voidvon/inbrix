package htmlsafe

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

// maxNormalizedHTMLBytes bounds HTML that is sent through the converter. The
// request body limit is much larger because it also covers attachments; this
// separate limit keeps a rich-text body from consuming disproportionate CPU.
const maxNormalizedHTMLBytes = 2 * 1024 * 1024

var markdownRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	// Raw HTML is intentionally disabled. The conversion pipeline must emit
	// elements produced by Goldmark only, never HTML supplied by the caller.
	goldmark.WithRendererOptions(goldmarkhtml.WithXHTML()),
)

// NormalizeHTML converts a caller-provided HTML body into a small, safe HTML
// representation:
//
//	HTML -> security defanging -> Markdown -> Goldmark HTML
//
// The Markdown string is an intermediate representation only. It is not stored
// as the MIME body because mail clients expect text/html or text/plain parts,
// and converting received mail in place would lose information such as inline
// resources and client-specific structure.
func NormalizeHTML(in string) (string, error) {
	if strings.TrimSpace(in) == "" {
		return "", nil
	}
	if len(in) > maxNormalizedHTMLBytes {
		return "", fmt.Errorf("html body exceeds %d bytes", maxNormalizedHTMLBytes)
	}

	defanged := SanitizeHTML(in)
	if strings.TrimSpace(defanged) == "" {
		return "", nil
	}

	markdown, err := htmltomarkdown.ConvertString(defanged)
	if err != nil {
		return "", fmt.Errorf("convert html to markdown: %w", err)
	}
	markdown = strings.TrimSpace(markdown)
	if markdown == "" {
		return "", nil
	}

	var rendered bytes.Buffer
	if err := markdownRenderer.Convert([]byte(markdown), &rendered); err != nil {
		return "", fmt.Errorf("render markdown as html: %w", err)
	}
	return strings.TrimSpace(rendered.String()), nil
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

// PlainTextFromHTML returns a compact text/plain fallback from normalized HTML.
// It is deliberately conservative: tags are discarded, entities are decoded
// after tag removal, and whitespace is collapsed. The rich HTML has already
// been normalized before this function is called.
func PlainTextFromHTML(in string) string {
	var b strings.Builder
	inTag := false
	for _, r := range in {
		switch {
		case r == '<':
			inTag = true
			b.WriteByte(' ')
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(stdhtml.UnescapeString(b.String())), " ")
}
