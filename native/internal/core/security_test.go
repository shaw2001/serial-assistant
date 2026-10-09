package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func secureConfig(t *testing.T) []byte {
	t.Helper()
	c := DefaultConfig()
	c["devices"] = []any{map[string]any{"id": "session-test", "path": "COM5", "baud": "115200", "receiveEncoding": "utf-8", "group": "rtt", "params": map[string]any{"data": 8, "parity": "none", "stop": 1, "flow": "none"}, "draft": text("private-draft"), "history": []Payload{text("private-history")}, "capture": true}}
	b, _ := json.Marshal(c)
	return b
}
func TestSecurityConfigBoundary(t *testing.T) {
	good := secureConfig(t)
	for name, b := range map[string][]byte{
		"unknown group":  []byte(strings.Replace(string(good), `"group":"rtt"`, `"group":"extra"`, 1)),
		"HTML mode":      []byte(strings.Replace(string(good), `"mode":"text"`, `"mode":"<meta>"`, 1)),
		"case ambiguity": []byte(strings.Replace(string(good), `"id":"session-test"`, `"id":"bad","ID":"session-test"`, 1)),
		"duplicate":      []byte(strings.Replace(string(good), `"schema":1`, `"schema":1,"schema":1`, 1)),
		"unknown field":  []byte(strings.Replace(string(good), `"schema":1`, `"schema":1,"updateURL":"https://example.invalid"`, 1)),
	} {
		if ValidateConfig(b) == nil {
			t.Errorf("accepted %s", name)
		}
	}
	var c map[string]any
	json.Unmarshal(good, &c)
	c["commands"].(map[string]any)["extra"] = []any{}
	b, _ := json.Marshal(c)
	if ValidateConfig(b) == nil {
		t.Fatal("extra commands accepted")
	}
	var missingHistory map[string]any
	json.Unmarshal(good, &missingHistory)
	delete(missingHistory["devices"].([]any)[0].(map[string]any), "history")
	withoutHistory, _ := json.Marshal(missingHistory)
	compatible, err := DecodeConfig(withoutHistory)
	if err != nil || compatible.Devices[0].History == nil {
		t.Fatal("missing optional history must normalize to an empty array")
	}
	normalized, e := DecodeConfig(good)
	if e != nil {
		t.Fatal(e)
	}
	if normalized.Devices[0].Capture || normalized.UI.RememberInput || normalized.UI.AutoUpdate {
		t.Fatal("legacy settings enabled sensitive persistence/network")
	}
	safe, e := ConfigBytes(good, false)
	if e != nil || strings.Contains(string(safe), "private-") {
		t.Fatalf("private data persisted: %v", e)
	}
	normalized.UI.RememberInput = true
	b, _ = json.Marshal(normalized)
	saved, _ := ConfigBytes(b, false)
	if !strings.Contains(string(saved), "private-draft") {
		t.Fatal("explicit opt-in lost")
	}
	exported, _ := ConfigBytes(b, true)
	if strings.Contains(string(exported), "private-") {
		t.Fatal("export leaked input")
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if e = SaveConfig(file, good); e != nil {
		t.Fatal(e)
	}
	disk, _ := os.ReadFile(file)
	if strings.Contains(string(disk), "private-") {
		t.Fatal("disk leaked history")
	}
}
func TestSecurityReleaseBoundary(t *testing.T) {
	for _, s := range []string{"https://notgithub.com/x", "https://github.com.evil.invalid/x", "https://github.com/other/repo/releases/latest", "https://github.com:443/shaw2001/serial-assistant/releases/latest", "https://user@github.com/shaw2001/serial-assistant/releases/latest", "https://github.com/shaw2001/serial-assistant/releases/tag/v0.1.8?q=evil", "https://github.com/shaw2001/serial-assistant/releases/tag/%76%30.1.8"} {
		if ValidReleaseURL(s) {
			t.Fatalf("accepted %s", s)
		}
	}
	if !ValidReleaseURL("https://github.com/shaw2001/serial-assistant/releases/tag/v0.1.8") {
		t.Fatal("official release rejected")
	}
	for _, flag := range []string{"draft", "prerelease"} {
		if _, e := ParseRelease([]byte(`{"tag_name":"v0.1.8","` + flag + `":true}`)); e == nil {
			t.Fatal("unstable release accepted")
		}
	}
	info, e := ParseRelease([]byte(`{"tag_name":"v0.1.8","html_url":"https://notgithub.com/"}`))
	if e != nil || !ValidReleaseURL(info.URL) {
		t.Fatal("remote URL trusted")
	}
}
func TestSecurityJournalQuota(t *testing.T) {
	dir := t.TempDir()
	if e := refreshJournalQuota(dir); e != nil {
		t.Fatal(e)
	}
	defer releaseJournalQuota(dir)
	journalQuota.Lock()
	journalQuota.used[dir] = maxJournalBytes - 1
	journalQuota.Unlock()
	if e := refreshJournalQuota(dir); e != nil {
		t.Fatal(e)
	}
	defer releaseJournalQuota(dir)
	wrote := false
	e := writeJournalBytes(dir, []byte("xx"), func(b []byte) (int, error) { wrote = true; return len(b), nil })
	if e == nil || wrote {
		t.Fatal("quota bypassed by concurrent journal")
	}
}
