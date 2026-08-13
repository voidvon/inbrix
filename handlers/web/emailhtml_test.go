package web

import (
	"strings"
	"testing"
)

// remoteVectors enumerates every auto-loading remote-content vector the privacy
// pass must neutralise. Each case supplies HTML with a UNIQUE remote host so a
// leak is easy to attribute, plus the http URL and the protocol-relative "//"
// form. The assertion: after blockRemoteContent the *live* markup must not fetch
// the remote host (any surviving occurrence must be inside a data-blocked-*
// stash or a rewritten about:blank url()), and blocked must be true.
func TestBlockRemoteContent_AllVectors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		host string // remote host that must not be live-fetchable afterwards
	}{
		{"input-type-image", `<input type="image" src="http://evil4/q">`, "evil4"},
		{"video-poster", `<video poster="http://evil5/p"></video>`, "evil5"},
		{"video-src", `<video src="http://evil6/m.mp4"></video>`, "evil6"},
		{"source-src", `<source src="http://evil7/s.webm">`, "evil7"},
		{"source-srcset", `<source srcset="http://evil8/s 1x">`, "evil8"},
		{"audio-src", `<audio src="http://evil9/a.mp3"></audio>`, "evil9"},
		{"track-src", `<track src="http://evil10/t.vtt">`, "evil10"},
		{"embed-src", `<embed src="http://evil11/e.swf">`, "evil11"},
		{"object-data", `<object data="http://evil12/o.svg"></object>`, "evil12"},
		{"iframe-src", `<iframe src="http://evil13/f"></iframe>`, "evil13"},
		{"link-stylesheet", `<link rel="stylesheet" href="http://evil14/z.css">`, "evil14"},
		{"link-unquoted", `<link rel=stylesheet href=http://evil15/z>`, "evil15"},
		{"background-attr", `<td background="http://evil16/bg.png"></td>`, "evil16"},
		{"inline-style-url", `<div style="background:url(http://evil17/x)"></div>`, "evil17"},
		{"style-block-url", `<style>body{background:url(http://evil18/x)}</style>`, "evil18"},
		{"style-block-import-url", `<style>@import url(http://evil19/y);</style>`, "evil19"},
		{"style-block-import-str", `<style>@import "http://evil20/y";</style>`, "evil20"},
		{"protocol-relative-link", `<link rel=stylesheet href="//evil22/r.css">`, "evil22"},
		{"protocol-relative-css", `<style>body{background:url(//evil23/r)}</style>`, "evil23"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, blocked := blockRemoteContent(tc.in)
			if !blocked {
				t.Fatalf("%s: expected blocked=true, got false (out=%q)", tc.name, out)
			}
			assertNoLiveRemote(t, out, tc.host)
		})
	}
}

func TestBlockRemoteContent_DisplaysMailImagesByDefault(t *testing.T) {
	for _, in := range []string{
		`<img src="http://images.example/pixel.gif">`,
		`<image src=http://images.example/logo>`,
		`<img srcset="http://images.example/a 1x, http://images.example/b 2x">`,
		`<img src="//images.example/banner">`,
	} {
		out, blocked := blockRemoteContent(in)
		if blocked || out != in {
			t.Fatalf("mail image should load unchanged by default: in=%q out=%q blocked=%v", in, out, blocked)
		}
	}
}

// assertNoLiveRemote fails if the rewritten HTML would still auto-fetch host.
// A reference to host is only acceptable if it is safely stashed: inside a
// data-blocked-* attribute (browser never fetches those) or rewritten to
// about:blank inside a url()/@import. Any other occurrence (a live src=, href=,
// poster=, data=, background=, or a live url(host)) is a leak.
func assertNoLiveRemote(t *testing.T, out, host string) {
	t.Helper()
	// Strip everything the browser will NOT fetch, then the host must be gone.
	stripped := out
	// Remove data-blocked-* attribute values (stashed, inert).
	for _, name := range []string{
		"data-blocked-src", "data-blocked-srcset", "data-blocked-poster",
		"data-blocked-href", "data-blocked-data", "data-blocked-background",
	} {
		stripped = removeAttr(stripped, name)
	}
	// Remove neutralised CSS references.
	stripped = strings.ReplaceAll(stripped, "about:blank#blocked", "")
	if strings.Contains(stripped, host) {
		t.Fatalf("live remote reference to %q survives after blocking: %q", host, out)
	}
}

// removeAttr deletes every `name="..."`, `name='...'` or `name=bareword` run from
// s so the test can confirm the host is not reachable via any LIVE attribute.
func removeAttr(s, name string) string {
	for {
		idx := indexAttr(s, name)
		if idx < 0 {
			return s
		}
		end := attrValueEnd(s, idx+len(name))
		s = s[:idx] + s[end:]
	}
}

func indexAttr(s, name string) int {
	return strings.Index(s, name+"=")
}

// attrValueEnd returns the index just past the attribute value that starts at
// eqStart (which points at the '=').
func attrValueEnd(s string, eqStart int) int {
	i := eqStart
	if i >= len(s) || s[i] != '=' {
		return i
	}
	i++
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '"':
		if j := strings.IndexByte(s[i+1:], '"'); j >= 0 {
			return i + 1 + j + 1
		}
		return len(s)
	case '\'':
		if j := strings.IndexByte(s[i+1:], '\''); j >= 0 {
			return i + 1 + j + 1
		}
		return len(s)
	default:
		// bareword: up to whitespace or '>'
		for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '>' && s[i] != '\n' {
			i++
		}
		return i
	}
}

// TestBlockRemoteContent_DataURIPreserved confirms inline and remote images are
// both displayed unchanged.
func TestBlockRemoteContent_DataURIPreserved(t *testing.T) {
	dataImg := `<img src="data:image/png;base64,iVBORw0KGgoAAAANSU">`
	in := dataImg + `<img src="http://evil/pixel.gif">`
	out, blocked := blockRemoteContent(in)
	if blocked {
		t.Fatalf("ordinary images should not be blocked")
	}
	if !strings.Contains(out, `src="data:image/png;base64,iVBORw0KGgoAAAANSU"`) {
		t.Fatalf("data: image was altered/blocked, got %q", out)
	}
	if !strings.Contains(out, `src="http://evil/pixel.gif"`) {
		t.Fatalf("expected the remote img to remain visible, got %q", out)
	}
}

// TestBlockRemoteContent_NoOverBlock confirms local content and ordinary remote
// images pass through untouched.
func TestBlockRemoteContent_NoOverBlock(t *testing.T) {
	local := `<img src="data:image/gif;base64,AAAA">` +
		`<img src="cid:logo@example">` +
		`<img src="/relative/path.png">` +
		`<img src="images/rel.png">` +
		`<a href="http://example.com/page">link</a>` +
		`<div style="background:url(/local/bg.png)">x</div>` +
		`<style>body{background:url(cid:bg@x)}</style>`
	out, blocked := blockRemoteContent(local)
	if blocked {
		t.Fatalf("over-blocked purely-local content: blocked=true, out=%q", out)
	}
	if out != local {
		t.Fatalf("local content was modified.\n got: %q\nwant: %q", out, local)
	}

	const remoteURL = "https://cdn.example.com/hero.jpg?u=abc"
	remote := `<img src="` + remoteURL + `">`
	rout, rblocked := blockRemoteContent(remote)
	if rblocked || rout != remote {
		t.Fatalf("remote image should pass through unchanged, got %q blocked=%v", rout, rblocked)
	}
}

// TestBlockRemoteContent_LinkAnchorUntouched makes sure ordinary <a href> links
// (user-followed, not auto-loaded) are never rewritten — only <link href> is.
func TestBlockRemoteContent_LinkAnchorUntouched(t *testing.T) {
	in := `<a href="http://example.com/page">click</a>`
	out, blocked := blockRemoteContent(in)
	if blocked {
		t.Fatalf("anchor href should not be blocked, blocked=true out=%q", out)
	}
	if out != in {
		t.Fatalf("anchor href was rewritten: %q", out)
	}
}

// TestBlockRemoteContent_InputNonImageUntouched confirms only <input type=image>
// (the one input that fetches a remote asset) has its src neutralised.
func TestBlockRemoteContent_InputNonImageUntouched(t *testing.T) {
	in := `<input type="text" src="http://evil/x">`
	out, blocked := blockRemoteContent(in)
	// A text input never fetches src, so nothing should be blocked.
	if blocked {
		t.Fatalf("non-image input should not be blocked, out=%q", out)
	}
	if out != in {
		t.Fatalf("non-image input rewritten: %q", out)
	}
}

// TestPrepareEmailHTML_WrapsAndDisplaysImages confirms ordinary remote and
// embedded images are both visible in the prepared iframe document.
func TestPrepareEmailHTML_WrapsAndDisplaysImages(t *testing.T) {
	out, blocked := prepareEmailHTML(`<img src="http://evil/p"><img src="data:image/gif;base64,AAAA">`)
	if blocked {
		t.Fatalf("ordinary images should not be reported as blocked")
	}
	if !strings.Contains(out, "<!DOCTYPE html>") || !strings.Contains(out, emailFrameCSS) {
		t.Fatalf("wrapper document not produced: %q", out)
	}
	if !strings.Contains(out, `src="http://evil/p"`) || strings.Contains(out, "data-blocked-src=") {
		t.Fatalf("remote img not restored in wrapper: %q", out)
	}
	if !strings.Contains(out, "data:image/gif;base64,AAAA") {
		t.Fatalf("data: image lost in wrapper: %q", out)
	}
}

func TestPrepareEmailHTML_DisplaysRemoteSrcsetButKeepsOtherContentBlocked(t *testing.T) {
	out, _ := prepareEmailHTML(`<img srcset="https://images.example/a 1x, https://images.example/b 2x"><iframe src="https://frame.example/"></iframe>`)
	if !strings.Contains(out, `srcset="https://images.example/a 1x, https://images.example/b 2x"`) {
		t.Fatalf("remote srcset not restored: %q", out)
	}
	if !strings.Contains(out, `loading="lazy"`) || !strings.Contains(out, `decoding="async"`) {
		t.Fatalf("remote image loading hints missing: %q", out)
	}
	if strings.Contains(out, `<iframe src="https://frame.example/"`) || !strings.Contains(out, `data-blocked-src="https://frame.example/"`) {
		t.Fatalf("non-image remote content should remain blocked: %q", out)
	}
}

func TestPrepareEmailHTMLKeepsLocalImagesEager(t *testing.T) {
	out, _ := prepareEmailHTML(`<img src="cid:logo@example.com"><img src="data:image/gif;base64,AAAA">`)
	if strings.Contains(out, `loading="lazy"`) {
		t.Fatalf("local images should not be delayed: %q", out)
	}
	if strings.Count(out, `decoding="async"`) != 2 {
		t.Fatalf("expected async decoding on both images: %q", out)
	}
}
