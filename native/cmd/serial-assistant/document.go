package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// WebView2 NavigateToString turns the page into a
// "data:text/html;charset=utf-8;base64,..." URL, and Chromium refuses URLs
// longer than 2 MiB characters. The real budget for the UTF-8 page is therefore
// about 1.5 MB, not the 2 MB the API docs suggest. Past it the call fails
// with E_INVALIDARG and the window stays blank. Keep a safety margin.
const (
	documentURIPrefix      = "data:text/html;charset=utf-8;base64,"
	chromiumMaxURLChars    = 2 * 1024 * 1024
	maxDocumentURIChars    = chromiumMaxURLChars - 128*1024
	fontAssetRef           = "OPPOSans-Regular.woff2"
	cssLinkTag             = `<link rel="stylesheet" href="app.css">`
	scriptTag              = `<script src="app.js"></script>`
	cspMetaPrefix          = `<meta http-equiv="Content-Security-Policy"`
	bridgeTokenPlaceholder = "__BRIDGE_TOKEN__"
)

type documentParts struct {
	HTML, CSS, JS, Bridge, Font []byte
	Nonce, BridgeToken          string
}

// documentURILength is the length of the data: URL WebView2 builds for html.
func documentURILength(html string) int {
	return len(documentURIPrefix) + base64.StdEncoding.EncodedLen(len(html))
}

// composeDocument inlines CSS, font and scripts into the single page handed to
// NavigateToString, and refuses pages that would exceed the URL cap.
func composeDocument(p documentParts) (string, error) {
	html := string(p.HTML)
	start := strings.Index(html, cspMetaPrefix)
	if start < 0 {
		return "", errors.New("index.html has no Content-Security-Policy")
	}
	end := strings.Index(html[start:], ">")
	if end < 0 {
		return "", errors.New("index.html has a broken Content-Security-Policy")
	}
	end += start
	policy := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-` + p.Nonce + `'; style-src 'unsafe-inline'; font-src data:; img-src data:; connect-src 'none'; base-uri 'none'; form-action 'none'">`
	html = html[:start] + policy + html[end+1:]
	cssText := strings.ReplaceAll(string(p.CSS), fontAssetRef, "data:font/woff2;base64,"+base64.StdEncoding.EncodeToString(p.Font))
	if !strings.Contains(html, cssLinkTag) || !strings.Contains(html, scriptTag) {
		return "", errors.New("index.html is missing the app.css or app.js reference")
	}
	html = strings.Replace(html, cssLinkTag, "<style>"+cssText+"</style>", 1)
	script := strings.Replace(string(p.Bridge), bridgeTokenPlaceholder, p.BridgeToken, 1) + "\n" + strings.ReplaceAll(string(p.JS), "</script", "<\\/script")
	html = strings.Replace(html, scriptTag, `<script nonce="`+p.Nonce+`">`+script+`</script>`, 1)
	if n := documentURILength(html); n > maxDocumentURIChars {
		return "", fmt.Errorf("page is too large for WebView2 (%d of %d URL characters)", n, maxDocumentURIChars)
	}
	return html, nil
}
