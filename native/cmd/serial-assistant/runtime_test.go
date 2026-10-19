package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindBundledRuntime(t *testing.T) {
	exe := t.TempDir()
	if dir, _ := findBundledRuntime(exe); dir != "" {
		t.Fatal("no bundled runtime should fall back to Evergreen")
	}
	nested := filepath.Join(exe, bundledRuntimeDir, "Microsoft.WebView2.FixedVersionRuntime.155.0.4283.60.x64")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"msedgewebview2.exe", "155.0.4283.60.manifest"} {
		if err := os.WriteFile(filepath.Join(nested, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir, version := findBundledRuntime(exe)
	if dir != nested || version != "155.0.4283.60" {
		t.Fatalf("got %q %q", dir, version)
	}
	flat := filepath.Join(t.TempDir())
	_ = os.MkdirAll(filepath.Join(flat, bundledRuntimeDir), 0o755)
	_ = os.WriteFile(filepath.Join(flat, bundledRuntimeDir, "msedgewebview2.exe"), []byte("x"), 0o644)
	if dir, _ := findBundledRuntime(flat); dir != filepath.Join(flat, bundledRuntimeDir) {
		t.Fatalf("flat layout not found: %q", dir)
	}
}
