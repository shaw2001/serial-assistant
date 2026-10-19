package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// diagLog is a small append-only text log the user can send back when the
// window misbehaves. It never holds serial data, only window facts.
type diagLog struct {
	mu   sync.Mutex
	path string
}

const diagLogLimit = 256 * 1024

func openDiagLog(path string) *diagLog {
	if info, err := os.Stat(path); err == nil && info.Size() > diagLogLimit {
		_ = os.Remove(path)
	}
	return &diagLog{path: path}
}

func (d *diagLog) Printf(format string, args ...any) {
	if d == nil || d.path == "" {
		return
	}
	line := time.Now().Format("2006-01-02 15:04:05.000 -0700 ") + sanitizeDiag(fmt.Sprintf(format, args...)) + "\r\n"
	d.mu.Lock()
	defer d.mu.Unlock()
	f, err := os.OpenFile(d.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

// sanitizeDiag keeps one printable line of bounded length.
func sanitizeDiag(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > 600 {
		s = s[:600] + "…"
	}
	return s
}
