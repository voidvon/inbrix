package api

import (
	"strings"
	"testing"
)

func TestPlainTextFromHTMLRendersDataTablesAsMarkdown(t *testing.T) {
	markup := `<table><tr><td>Product</td><td>Unit price</td><td>Lead time</td></tr>` +
		`<tr><td>Maintenance kit AE14ESV</td><td>USD 215.00</td><td>1 week</td></tr></table>`
	want := "| Product | Unit price | Lead time |\n" +
		"| --- | --- | --- |\n" +
		"| Maintenance kit AE14ESV | USD 215.00 | 1 week |"
	if got := PlainTextFromHTML(markup); got != want {
		t.Fatalf("table projection:\n got: %q\nwant: %q", got, want)
	}
}

func TestPlainTextFromHTMLNeverGluesCompactTableCells(t *testing.T) {
	markup := `<html><body><table><tr><td>Product</td><td>Unit price</td><td>Lead time</td></tr>` +
		`<tr><td>Maintenance kit AE14ESV</td><td>USD 215.00</td><td>1 week</td></tr>` +
		`<tr><td>Freight</td><td colspan="2">not included</td></tr></table></body></html>`
	text := PlainTextFromHTML(markup)
	for _, glue := range []string{"215.001", "ProductUnit", "priceLead"} {
		if strings.Contains(text, glue) {
			t.Fatalf("compact table glued cells: %q", text)
		}
	}
	if !strings.Contains(text, "| USD 215.00 | 1 week |") {
		t.Fatalf("price row lost structure: %q", text)
	}
	if !strings.Contains(text, "| Freight | not included |  |") {
		t.Fatalf("colspan was not padded into columns: %q", text)
	}
}

func TestPlainTextFromHTMLTableRowspanKeepsColumnAlignment(t *testing.T) {
	markup := `<table><tr><td rowspan="2">Model</td><td>USD 215.00</td></tr>` +
		`<tr><td>USD 230.00</td></tr></table>`
	want := "| Model | USD 215.00 |\n" +
		"| --- | --- |\n" +
		"|  | USD 230.00 |"
	if got := PlainTextFromHTML(markup); got != want {
		t.Fatalf("rowspan projection:\n got: %q\nwant: %q", got, want)
	}
}

func TestPlainTextFromHTMLLayoutTableFallsBackToBlocks(t *testing.T) {
	markup := `<table><tr><td><table><tr><td>Button</td><td>Menu</td></tr></table></td></tr>` +
		`<tr><td>Body text</td></tr></table>`
	text := PlainTextFromHTML(markup)
	// The inner two-column table is data and stays Markdown; the outer
	// single-column wrapper must not wrap its own rows in pipes.
	if !strings.Contains(text, "| Button | Menu |") {
		t.Fatalf("inner data table lost structure: %q", text)
	}
	if strings.Contains(text, "| Body text") {
		t.Fatalf("layout wrapper rendered as pipe noise: %q", text)
	}
}

func TestPlainTextFromHTMLSingleColumnTableStaysBlocks(t *testing.T) {
	markup := `<table><tr><td>first</td></tr><tr><td>second</td></tr></table>`
	if got := PlainTextFromHTML(markup); got != "first\nsecond" {
		t.Fatalf("single-column stack: %q", got)
	}
}

func TestPlainTextFromHTMLEscapesPipesInCells(t *testing.T) {
	markup := `<table><tr><td>a|b</td><td>c</td></tr></table>`
	if got := PlainTextFromHTML(markup); !strings.Contains(got, "a\\|b") {
		t.Fatalf("pipe not escaped: %q", got)
	}
}

func TestPlainTextFromHTMLBlockBoundariesAndWhitespace(t *testing.T) {
	markup := "<div>Good\n  day,</div>\n<p style='x'>The request was for 1 kit&nbsp; only.</p>" +
		`<span>tail</span>`
	want := "Good day,\nThe request was for 1 kit only.\ntail"
	if got := PlainTextFromHTML(markup); got != want {
		t.Fatalf("block projection:\n got: %q\nwant: %q", got, want)
	}
}

func TestPlainTextFromHTMLSkipsMachineContentAndTitle(t *testing.T) {
	markup := `<html><head><title>Brand Newsletter</title><style>#outlook a{padding:0}</style></head>` +
		`<body><script>tracker()</script><p>Real content</p><img src="pixel.gif" alt="">` +
		`<img src="chart.png" alt="Sales chart"></body></html>`
	text := PlainTextFromHTML(markup)
	for _, noise := range []string{"Brand Newsletter", "padding:0", "tracker()", "http"} {
		if strings.Contains(text, noise) {
			t.Fatalf("machine content leaked: %q", text)
		}
	}
	if !strings.Contains(text, "Real content") || !strings.Contains(text, "[image: Sales chart]") {
		t.Fatalf("content or image alt lost: %q", text)
	}
}

func TestPlainTextFromHTMLStripsZeroWidthPadding(t *testing.T) {
	markup := "<p>Caf&eacute; &amp; bar\u200c\u200b\ufeff</p><p>&nbsp;</p><p>next</p>"
	if got := PlainTextFromHTML(markup); got != "Café & bar\nnext" {
		t.Fatalf("entity/zero-width projection: %q", got)
	}
}

func TestPlainTextFromHTMLPreservesPreAndBr(t *testing.T) {
	markup := "<pre>line1\n  line2</pre><p>after<br>break</p>"
	want := "line1\n  line2\nafter\nbreak"
	if got := PlainTextFromHTML(markup); got != want {
		t.Fatalf("pre/br projection:\n got: %q\nwant: %q", got, want)
	}
}
