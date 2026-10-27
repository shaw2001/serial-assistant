package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPersistsLogThemeAndLevelColors(t *testing.T) {
	cfg := DefaultConfig()
	cfg["ui"] = map[string]any{"privacyVersion": 1, "logTheme": "dark", "levelColors": map[string]string{"w": "#d97a00", "e": "#E0342A", "i": "#1f9d48"}}
	data, _ := json.Marshal(cfg)
	file := filepath.Join(t.TempDir(), "config.json")
	if e := SaveConfig(file, data); e != nil {
		t.Fatal(e)
	}
	loaded, warning := LoadConfig(file)
	c := loaded.(Config)
	if warning != "" || c.UI.LogTheme != "dark" || c.UI.LevelColors["w"] != "#d97a00" || c.UI.LevelColors["e"] != "#E0342A" {
		t.Fatalf("round trip lost log theme/colours: %+v %q", c.UI, warning)
	}
	for _, bad := range []map[string]any{
		{"privacyVersion": 1, "logTheme": "blue"},
		{"privacyVersion": 1, "levelColors": map[string]string{"x": "#000000"}},
		{"privacyVersion": 1, "levelColors": map[string]string{"w": "red"}},
		{"privacyVersion": 1, "levelColors": map[string]string{"w": "#12345"}},
		{"privacyVersion": 1, "levelColors": map[string]string{"we": "#123456"}},
	} {
		cfg["ui"] = bad
		data, _ = json.Marshal(cfg)
		if ValidateConfig(data) == nil {
			t.Fatalf("must reject %v", bad)
		}
	}
}

func TestConfigToleratesRemovedDeviceComplete(t *testing.T) {
	cfg := DefaultConfig()
	cfg["ui"] = map[string]any{"privacyVersion": 1, "deviceComplete": true}
	data, _ := json.Marshal(cfg)
	file := filepath.Join(t.TempDir(), "config.json")
	if e := SaveConfig(file, data); e != nil {
		t.Fatal(e)
	}
	loaded, warning := LoadConfig(file)
	if warning != "" {
		t.Fatalf("old config with deviceComplete must still load: %q", warning)
	}
	b, _ := json.Marshal(loaded)
	if strings.Contains(string(b), "deviceComplete") {
		t.Fatalf("removed field must be dropped: %s", b)
	}
}
