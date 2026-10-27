package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestImportedCommandGroupsAndDescriptionsValidate(t *testing.T) {
	cfg := DefaultConfig()
	cmds := cfg["commands"].(map[string][]map[string]any)
	cmds["g:电源管理"] = []map[string]any{{"id": "x1", "name": "关机", "content": "poweroff", "mode": "text", "encoding": "utf-8", "ending": "crlf", "description": "让设备关机"}}
	data, _ := json.Marshal(cfg)
	if e := ValidateConfig(data); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "c.json")
	if e := SaveConfig(file, data); e != nil {
		t.Fatal(e)
	}
	loaded, _ := LoadConfig(file)
	if c := loaded.(Config); c.Commands["g:电源管理"][0].Description != "让设备关机" {
		t.Fatalf("description lost: %+v", c.Commands)
	}
	for _, bad := range []string{"other", "g:", "g: x", "g:" + string(make([]rune, 33))} {
		cfg := DefaultConfig()
		cfg["commands"].(map[string][]map[string]any)[bad] = []map[string]any{}
		data, _ := json.Marshal(cfg)
		if ValidateConfig(data) == nil {
			t.Fatalf("group %q must be rejected", bad)
		}
	}
	cfg = DefaultConfig()
	delete(cfg["commands"].(map[string][]map[string]any), "at")
	data, _ = json.Marshal(cfg)
	if ValidateConfig(data) == nil {
		t.Fatal("built-in group is required")
	}
}

func TestReadTextFileHandlesBOMAndGBK(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.csv")
	os.WriteFile(a, append([]byte{0xEF, 0xBB, 0xBF}, []byte("分组,名称\n")...), 0600)
	if s, e := ReadTextFile(a); e != nil || s != "分组,名称\n" {
		t.Fatalf("%q %v", s, e)
	}
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("分组,名称\n"))
	b := filepath.Join(dir, "b.csv")
	os.WriteFile(b, gbk, 0600)
	if s, e := ReadTextFile(b); e != nil || s != "分组,名称\n" {
		t.Fatalf("gbk: %q %v", s, e)
	}
	c := filepath.Join(dir, "c.csv")
	if e := WriteTextFile(c, "x", true); e != nil {
		t.Fatal(e)
	}
	if s, _ := ReadTextFile(c); s != "x" {
		t.Fatal("bom round trip")
	}
	big := filepath.Join(dir, "big")
	os.WriteFile(big, make([]byte, maxTextImport+1), 0600)
	if _, e := ReadTextFile(big); e == nil {
		t.Fatal("size limit")
	}
}
