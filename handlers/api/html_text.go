package api

import (
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

// PlainTextFromHTML projects HTML mail to the text the AI reads. Structure is
// preserved: block elements become line breaks and data tables become Markdown
// pipe tables, so a compact quotation table cannot glue a price onto the next
// column's value. Everything is still plain text — the model never receives
// markup, styles, or scripts.

// htmlTextSkipElements carry no readable mail content. Their text is machine
// data (CSS, script source, form widgets) that would otherwise pollute
// previews and evidence.
var htmlTextSkipElements = map[string]bool{
	"head": true, "title": true, "style": true, "script": true, "template": true,
	"svg": true, "math": true, "iframe": true, "canvas": true, "object": true,
	"embed": true, "audio": true, "video": true, "source": true, "track": true,
	"select": true, "datalist": true, "option": true, "optgroup": true,
	"input": true, "textarea": true, "meta": true, "link": true, "base": true,
}

var htmlTextBlockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"caption": true, "center": true, "dd": true, "details": true, "div": true,
	"dl": true, "dt": true, "fieldset": true, "figcaption": true, "figure": true,
	"footer": true, "form": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "header": true, "hr": true, "legend": true, "li": true,
	"main": true, "menu": true, "nav": true, "ol": true, "p": true, "pre": true,
	"section": true, "summary": true, "table": true, "tbody": true, "td": true,
	"tfoot": true, "th": true, "thead": true, "tr": true, "ul": true,
}

type htmlTextWriter struct {
	sb   strings.Builder
	last byte
}

func (w *htmlTextWriter) newline() {
	if w.last != 0 && w.last != '\n' {
		w.sb.WriteByte('\n')
		w.last = '\n'
	}
}

func (w *htmlTextWriter) write(s string) {
	if s == "" {
		return
	}
	if w.last == 0 || w.last == '\n' {
		s = strings.TrimLeft(s, " \t")
		if s == "" {
			return
		}
	}
	w.sb.WriteString(s)
	w.last = s[len(s)-1]
}

func (w *htmlTextWriter) visit(node *xhtml.Node, inPre bool) {
	switch node.Type {
	case xhtml.TextNode:
		if inPre {
			w.write(strings.Map(htmlTextKeepRaw, node.Data))
			return
		}
		w.write(collapseHTMLText(node.Data))
	case xhtml.ElementNode:
		name := strings.ToLower(node.Data)
		if htmlTextSkipElements[name] {
			return
		}
		switch name {
		case "br":
			w.newline()
			return
		case "img":
			if alt := strings.TrimSpace(htmlElementAttr(node, "alt")); alt != "" {
				w.write("[image: " + collapseHTMLText(alt) + "]")
			}
			return
		case "table":
			w.newline()
			w.renderTable(node)
			w.newline()
			return
		}
		block := htmlTextBlockElements[name]
		if block {
			w.newline()
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.visit(child, inPre || name == "pre")
		}
		if block {
			w.newline()
		}
	default:
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			w.visit(child, inPre)
		}
	}
}

// renderTable emits a Markdown pipe table for data tables and falls back to
// plain block text for layout tables: anything nested, single-column, or
// without extractable rows is layout that would only produce pipe noise.
func (w *htmlTextWriter) renderTable(table *xhtml.Node) {
	for child := table.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == xhtml.ElementNode && strings.EqualFold(child.Data, "caption") {
			w.visit(child, false)
		}
	}
	if htmlTableContainsNestedTable(table) {
		w.visitChildrenAsBlocks(table)
		return
	}
	rows, cols := htmlTableGrid(table)
	if cols < 2 || len(rows) == 0 {
		w.visitChildrenAsBlocks(table)
		return
	}
	separator := strings.Repeat(" --- |", cols)
	for i, row := range rows {
		if htmlTableRowEmpty(row) {
			continue
		}
		w.sb.WriteString("|")
		for _, cell := range row {
			w.sb.WriteString(" " + htmlEscapeTableCell(cell) + " |")
		}
		w.sb.WriteByte('\n')
		w.last = '\n'
		if i == 0 {
			w.sb.WriteString("|" + separator)
			w.sb.WriteByte('\n')
			w.last = '\n'
		}
	}
}

func (w *htmlTextWriter) visitChildrenAsBlocks(node *xhtml.Node) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		w.visit(child, false)
	}
}

// htmlTableGrid lays cells out with colspan padding and rowspan placeholders so
// later rows stay aligned with their columns.
func htmlTableGrid(table *xhtml.Node) ([][]string, int) {
	var rows [][]string
	pending := map[int]int{}
	var collect func(*xhtml.Node)
	collect = func(node *xhtml.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if child.Type != xhtml.ElementNode {
				continue
			}
			switch strings.ToLower(child.Data) {
			case "tr":
				rows = append(rows, htmlTableRow(child, pending))
			case "thead", "tbody", "tfoot":
				collect(child)
			default:
				collect(child)
			}
		}
	}
	collect(table)
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	for i := range rows {
		for len(rows[i]) < cols {
			rows[i] = append(rows[i], "")
		}
	}
	return rows, cols
}

func htmlTableRow(row *xhtml.Node, pending map[int]int) []string {
	cells := []string{}
	col := 0
	set := func(index int, value string) {
		for len(cells) <= index {
			cells = append(cells, "")
		}
		cells[index] = value
	}
	for child := row.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != xhtml.ElementNode {
			continue
		}
		name := strings.ToLower(child.Data)
		if name != "td" && name != "th" {
			continue
		}
		for pending[col] > 0 {
			set(col, "")
			pending[col]--
			col++
		}
		set(col, htmlTableCellText(child))
		span := htmlSpanAttr(child, "colspan")
		depth := htmlSpanAttr(child, "rowspan")
		for k := 1; k < span; k++ {
			set(col+k, "")
		}
		if depth > 1 {
			for k := 0; k < span; k++ {
				pending[col+k] += depth - 1
			}
		}
		col += span
	}
	for pending[col] > 0 {
		set(col, "")
		pending[col]--
		col++
	}
	return cells
}

func htmlTableCellText(cell *xhtml.Node) string {
	var inner htmlTextWriter
	for child := cell.FirstChild; child != nil; child = child.NextSibling {
		inner.visit(child, false)
	}
	return strings.Join(strings.Fields(inner.sb.String()), " ")
}

func htmlTableContainsNestedTable(table *xhtml.Node) bool {
	var found bool
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		for child := node.FirstChild; child != nil && !found; child = child.NextSibling {
			if child.Type == xhtml.ElementNode {
				if strings.EqualFold(child.Data, "table") {
					found = true
					return
				}
				walk(child)
			}
		}
	}
	walk(table)
	return found
}

func htmlTableRowEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func htmlEscapeTableCell(cell string) string {
	return strings.ReplaceAll(cell, "|", "\\|")
}

func htmlSpanAttr(cell *xhtml.Node, name string) int {
	value, err := strconv.Atoi(strings.TrimSpace(htmlElementAttr(cell, name)))
	if err != nil || value < 1 {
		return 1
	}
	if value > 1000 {
		return 1000
	}
	return value
}

func htmlElementAttr(node *xhtml.Node, key string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

// collapseHTMLText normalizes one text run: entities are already unescaped by
// the parser, &nbsp; and source line wrapping collapse to spaces, and the
// zero-width characters senders pad preheaders with are removed.
func collapseHTMLText(text string) string {
	var b strings.Builder
	pendingSpace := false
	for _, r := range text {
		switch {
		case htmlTextIsZeroWidth(r):
		case r == '\u00a0' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v':
			pendingSpace = true
		default:
			if pendingSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			pendingSpace = false
			b.WriteRune(r)
		}
	}
	if pendingSpace && b.Len() > 0 {
		b.WriteByte(' ')
	}
	return b.String()
}

func htmlTextIsZeroWidth(r rune) bool {
	switch r {
	case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff', '\u00ad', '\u034f':
		return true
	}
	return false
}

// htmlTextKeepRaw preserves pre formatting while still dropping zero-width
// padding; &nbsp; stays literal inside pre.
func htmlTextKeepRaw(r rune) rune {
	if htmlTextIsZeroWidth(r) {
		return -1
	}
	return r
}

func htmlToPlainText(markup string) string {
	if strings.TrimSpace(markup) == "" {
		return ""
	}
	document, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		// The parser is error-tolerant in practice; keep the linear scanner as
		// the fallback so projection can never lose the body entirely.
		return stripHTML(markup)
	}
	var writer htmlTextWriter
	for child := document.FirstChild; child != nil; child = child.NextSibling {
		writer.visit(child, false)
	}
	lines := strings.Split(writer.sb.String(), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimRight(line, " \t"); line != "" {
			kept = append(kept, line)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
