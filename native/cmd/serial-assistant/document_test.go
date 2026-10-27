package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testParts(font []byte) documentParts {
	return documentParts{
		HTML:        []byte(`<html><head><meta http-equiv="Content-Security-Policy" content="x"><link rel="stylesheet" href="app.css"></head><body><script src="app.js"></script></body></html>`),
		CSS:         []byte(`@font-face{src:url(OPPOSans-Regular.woff2)}`),
		JS:          []byte(`console.log("</script>")`),
		Bridge:      []byte(`const token='__BRIDGE_TOKEN__';`),
		Font:        font,
		Nonce:       "n0",
		BridgeToken: "t0",
	}
}

func TestComposeDocumentInlinesAssets(t *testing.T) {
	html, err := composeDocument(testParts([]byte("font")))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"OPPOSans-Regular.woff2", "__BRIDGE_TOKEN__", `src="app.js"`, `href="app.css"`, "</script>\")"} {
		if strings.Contains(html, bad) {
			t.Fatalf("composed page still contains %q", bad)
		}
	}
	for _, want := range []string{"data:font/woff2;base64,", "'nonce-n0'", `<script nonce="n0">`, "token='t0'"} {
		if !strings.Contains(html, want) {
			t.Fatalf("composed page lacks %q", want)
		}
	}
}

func TestComposeDocumentRejectsPagesOverTheWebView2URLCap(t *testing.T) {
	// 1.6 MB of font becomes ~2.8 M data: URL characters, the failure seen on Windows.
	if _, err := composeDocument(testParts(make([]byte, 1_600_000))); err == nil {
		t.Fatal("an oversized page must be rejected instead of loading blank")
	}
	if documentURILength("abc") != len(documentURIPrefix)+4 {
		t.Fatal("data: URL length is base64 of the UTF-8 page")
	}
}

// The shipped assets must fit with room to spare. Runs after npm run build.
func TestBuiltAssetsFitWebView2Budget(t *testing.T) {
	dist := filepath.Join("..", "..", "..", "dist")
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dist, name))
		if err != nil {
			t.Skip("dist not built: " + err.Error())
		}
		return b
	}
	p := documentParts{HTML: read("index.html"), CSS: read("app.css"), JS: read("app.js"), Font: read("OPPOSans-Regular.woff2"), Bridge: []byte(strings.Repeat("b", 4096)), Nonce: strings.Repeat("0", 32), BridgeToken: strings.Repeat("0", 64)}
	html, err := composeDocument(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := documentURILength(html); n > maxDocumentURIChars*9/10 {
		t.Fatalf("page uses %d of %d URL characters; leave headroom", n, maxDocumentURIChars)
	}
}

// The page runs from a data: URL (NavigateToString) with img-src data: only,
// so a relative asset such as <img src="logo.svg"> can never load in WebView2
// (it showed an empty box in 0.4.1). Every src/href must be inline.
func TestComposedPageHasNoRelativeAssetsAndInlinesLogo(t *testing.T) {
	dist := filepath.Join("..", "..", "..", "dist")
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dist, name))
		if err != nil {
			t.Skip("dist not built: " + err.Error())
		}
		return b
	}
	p := documentParts{HTML: read("index.html"), CSS: read("app.css"), JS: read("app.js"), Font: read("OPPOSans-Regular.woff2"), Bridge: []byte("b"), Nonce: strings.Repeat("0", 32), BridgeToken: strings.Repeat("0", 64)}
	html, err := composeDocument(p)
	if err != nil {
		t.Fatal(err)
	}
	body := html[:strings.Index(html, "<script nonce=")]
	for _, attr := range []string{` src="`, ` href="`} {
		for rest := body; ; {
			i := strings.Index(rest, attr)
			if i < 0 {
				break
			}
			rest = rest[i+len(attr):]
			if v := rest[:strings.Index(rest, `"`)]; !strings.HasPrefix(v, "data:") && !strings.HasPrefix(v, "#") {
				t.Fatalf("page references %q, which cannot load from a NavigateToString data: page", v)
			}
		}
	}
	i := strings.Index(body, `<img class="su-logo"`)
	if i < 0 || !strings.Contains(body[i:i+200], `src="data:image/png;base64,`) {
		t.Fatal("title-bar logo must be the app icon as an inline data: PNG")
	}
}
