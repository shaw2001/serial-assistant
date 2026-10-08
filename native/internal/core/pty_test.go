//go:build !windows

package core

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNativePTY(t *testing.T) {
	a, b := os.Getenv("SERIAL_TEST_PTY_A"), os.Getenv("SERIAL_TEST_PTY_B")
	if a == "" || b == "" {
		t.Skip("run scripts/native-pty.py for two kernel serial endpoints")
	}
	s := NewService(t.TempDir(), OpenSerial)
	defer s.Shutdown()
	s.Open("session-one", opts(a))
	if _, e := s.Open("session-two", opts(b)); e != nil {
		t.Fatal(e)
	}
	fmt.Println("PTY READY")
	want := append([]byte{0, 255}, []byte("温度\r\n")...)
	wait(t, func() bool {
		v, _ := s.Snapshot("session-one")
		return v.(map[string]any)["rx"].(uint64) >= uint64(len(want))
	})
	wait(t, func() bool { v, _ := s.Snapshot("session-two"); return v.(map[string]any)["rx"].(uint64) >= 5 })
	records, _, _ := s.Records("session-one", "cache", nil)
	var raw []byte
	for _, r := range records {
		if r.Direction == "RX" {
			v, _ := base64.StdEncoding.DecodeString(r.DataBase64)
			raw = append(raw, v...)
		}
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("RX bytes %x", raw)
	}
	s.Send("session-one", Payload{"00 FF E6 B8 A9 E5 BA A6 0D 0A", "hex", "utf-8", "none"})
	s.Send("session-two", Payload{"port2", "text", "utf-8", "none"})
	file := filepath.Join(t.TempDir(), "all.jsonl")
	if e := s.Export(file, ExportOptions{ID: "session-one", Scope: "all", Format: "jsonl", Encoding: "utf-8"}); e != nil {
		t.Fatal(e)
	}
	fmt.Println("PTY TX COMPLETE")
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 10; i++ {
		s.Close("session-one")
		if _, e := s.Open("session-one", opts(a)); e != nil {
			t.Fatal(e)
		}
	}
	s.StartPeriodic("session-one", text("ps"), 20)
	time.Sleep(100 * time.Millisecond)
	s.StopPeriodic("session-one")
	fmt.Println("PTY RECONNECT COMPLETE")
	wait(t, func() bool { v, _ := s.Snapshot("session-one"); return v.(map[string]any)["status"] == "error" })
	v, _ := s.Snapshot("session-two")
	if v.(map[string]any)["status"] != "connected" {
		t.Fatal("other device was disconnected")
	}
}
