package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagLogAppendsSanitizedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diag.log")
	d := openDiagLog(path)
	d.Printf("build=%d\nhr=%#x", 22631, 0x80070057)
	d.Printf("%s", strings.Repeat("x", 2000))
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\r\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "build=22631 hr=0x80070057") || len(lines[1]) > 700 {
		t.Fatalf("unexpected log: %q", b)
	}
	var nilLog *diagLog
	nilLog.Printf("ignored")
}
