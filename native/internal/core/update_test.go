package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.7", "0.1.6", 1},
		{"0.2.0", "0.1.9", 1},
		{"1.0.0", "1.0.0", 0},
		{"v0.1.6", "0.1.6", 0},
		{"0.1.6", "0.1.10", -1},
		{"", "0.0.1", -1},
		{"0.2", "0.1.9", 1},
		{"0.1.6-beta", "0.1.6", 0},
		{"0.1.7+build", "0.1.6", 1},
		{"garbage", "0.0.0", 0},
	}
	for _, c := range cases {
		if got := CompareVersion(c.a, c.b); got != c.want {
			t.Fatalf("CompareVersion(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNewer(t *testing.T) {
	if !Newer("v0.1.7", "0.1.6") {
		t.Fatal("expected 0.1.7 to be newer than 0.1.6")
	}
	if Newer("v0.1.6", "0.1.6") || Newer("v0.1.5", "0.1.6") {
		t.Fatal("equal or older releases must not be reported as newer")
	}
}

func TestParseRelease(t *testing.T) {
	body := `{"tag_name":"v0.1.7","name":"v0.1.7 · 新版","html_url":"https://github.com/shaw2001/serial-assistant/releases/tag/v0.1.7",
		"published_at":"2026-10-09T02:53:57Z","draft":false,"prerelease":false,
		"assets":[{"name":"SHA256SUMS.txt","size":101},{"name":"SerialAssistant-0.1.7-win-x64.exe","size":4545024}]}`
	info, e := ParseRelease([]byte(body))
	if e != nil {
		t.Fatal(e)
	}
	if info.Latest != "v0.1.7" || info.Name != "v0.1.7 · 新版" || !info.Checked {
		t.Fatalf("unexpected release: %+v", info)
	}
	if info.AssetName != "SerialAssistant-0.1.7-win-x64.exe" || info.AssetSize != 4545024 {
		t.Fatalf("windows asset not detected: %+v", info)
	}
	if got := Compare(info, "0.1.6"); !got.Available || got.Current != "0.1.6" {
		t.Fatalf("expected an available update: %+v", got)
	}
	if got := Compare(info, "0.1.7"); got.Available {
		t.Fatalf("same version must not be newer: %+v", got)
	}
}

func TestParseReleaseRejectsBadPayloads(t *testing.T) {
	for name, body := range map[string]string{
		"broken": `{"tag_name":`,
		"empty":  `{}`,
		"draft":  `{"tag_name":"v9.9.9","draft":true,"prerelease":true}`,
		"notag":  `{"name":"no tag"}`,
	} {
		if _, e := ParseRelease([]byte(body)); e == nil {
			t.Fatalf("%s payload should fail", name)
		}
	}
}

func TestParseReleaseWithoutWindowsAsset(t *testing.T) {
	info, e := ParseRelease([]byte(`{"tag_name":"v0.2.0","assets":[{"name":"notes.txt","size":10}]}`))
	if e != nil || info.AssetName != "" || info.AssetSize != 0 {
		t.Fatalf("expected no asset, got %+v (%v)", info, e)
	}
}

type offlineTransport struct{}

func (offlineTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

func TestFetchRelease(t *testing.T) {
	var seenUserAgent, seenAccept, seenPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUserAgent, seenAccept, seenPath = r.Header.Get("User-Agent"), r.Header.Get("Accept"), r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.2.0", "html_url": "https://example.invalid/r"})
	}))
	defer server.Close()
	info, e := fetchRelease(context.Background(), server.Client(), server.URL+"/repos/owner/repo/releases/latest", "0.1.7")
	if e != nil {
		t.Fatal(e)
	}
	if !info.Available || info.Current != "0.1.7" || info.Latest != "v0.2.0" {
		t.Fatalf("unexpected info: %+v", info)
	}
	if seenUserAgent != "SerialAssistant/0.1.7" || seenAccept != "application/vnd.github+json" || seenPath != "/repos/owner/repo/releases/latest" {
		t.Fatalf("unexpected request: %q %q %q", seenUserAgent, seenAccept, seenPath)
	}
}

func TestFetchReleaseErrors(t *testing.T) {
	for name, status := range map[string]int{"rate limited": http.StatusTooManyRequests, "forbidden": http.StatusForbidden, "missing": http.StatusNotFound} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		if _, e := fetchRelease(context.Background(), server.Client(), server.URL, "0.1.7"); e == nil {
			t.Fatalf("%s should surface an error", name)
		}
		server.Close()
	}
	offline := &http.Client{Transport: offlineTransport{}, Timeout: time.Second}
	if _, e := fetchRelease(context.Background(), offline, "https://api.github.com/x", "0.1.7"); e == nil {
		t.Fatal("offline check should surface an error")
	} else if !strings.Contains(e.Error(), "无法连接 GitHub") {
		t.Fatalf("unexpected offline message: %v", e)
	}
}

func TestUpdateStateRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "update-check.json")
	if got := LoadUpdateState(file); !got.LastCheck.IsZero() {
		t.Fatalf("missing state must be zero, got %v", got.LastCheck)
	}
	want := UpdateState{LastCheck: time.Now().UTC().Truncate(time.Second)}
	if e := SaveUpdateState(file, want); e != nil {
		t.Fatal(e)
	}
	got := LoadUpdateState(file)
	if got.LastCheck.IsZero() || !got.LastCheck.Equal(want.LastCheck) {
		t.Fatalf("state round trip failed: %v vs %v", got.LastCheck, want.LastCheck)
	}
}

func TestShouldCheck(t *testing.T) {
	now := time.Now()
	if !ShouldCheck(UpdateState{}, now, 24*time.Hour) {
		t.Fatal("first check must run")
	}
	if ShouldCheck(UpdateState{LastCheck: now.Add(-time.Hour)}, now, 24*time.Hour) {
		t.Fatal("recent check must be throttled")
	}
	if !ShouldCheck(UpdateState{LastCheck: now.Add(-25 * time.Hour)}, now, 24*time.Hour) {
		t.Fatal("stale check must run again")
	}
}

func TestLoadUpdateStateIgnoresGarbage(t *testing.T) {
	file := filepath.Join(t.TempDir(), "update-check.json")
	if e := os.WriteFile(file, []byte("not json"), 0600); e != nil {
		t.Fatal(e)
	}
	if got := LoadUpdateState(file); !got.LastCheck.IsZero() {
		t.Fatalf("corrupt state must fall back to zero, got %v", got.LastCheck)
	}
}
