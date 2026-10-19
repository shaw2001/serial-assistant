package main

import (
	"os"
	"path/filepath"
	"strings"
)

// bundledRuntimeDir is the folder, next to the EXE, where the portable
// package ships the WebView2 Fixed Version runtime.
const bundledRuntimeDir = "WebView2Runtime"

// findBundledRuntime returns the folder holding msedgewebview2.exe inside
// <exeDir>\WebView2Runtime, either directly or one level down (the folder name
// Microsoft's CAB extracts to), plus the runtime version from its manifest.
// It returns "" when no bundled runtime is present, so the system Evergreen
// runtime is used.
func findBundledRuntime(exeDir string) (folder, version string) {
	root := filepath.Join(exeDir, bundledRuntimeDir)
	candidates := []string{root}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates, filepath.Join(root, e.Name()))
			}
		}
	}
	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "msedgewebview2.exe")); err == nil && info.Mode().IsRegular() {
			return dir, runtimeVersionIn(dir)
		}
	}
	return "", ""
}

func runtimeVersionIn(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasSuffix(name, ".manifest") {
			v := strings.TrimSuffix(name, ".manifest")
			if strings.Count(v, ".") == 3 {
				return v
			}
		}
	}
	return ""
}
