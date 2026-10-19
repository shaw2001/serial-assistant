package core

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func exportService(t *testing.T) (*Service, *mockPort) {
	t.Helper()
	p := newMock()
	s := NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	t.Cleanup(s.Shutdown)
	return s, p
}

func rxCount(s *Service, id string, n uint64) func() bool {
	return func() bool { v, _ := s.Snapshot(id); return v.(map[string]any)["rx"].(uint64) == n }
}

// With “记录” off (the default) the full-session export used to contain only
// the header line. It must now carry every cached record.
func TestExportAllWithoutCaptureWritesCachedRecords(t *testing.T) {
	AppVersion = "9.9.9"
	defer func() { AppVersion = "dev" }()
	s, p := exportService(t)
	o := opts("COM7")
	o.Capture = false
	if _, e := s.Open("session-s1", o); e != nil {
		t.Fatal(e)
	}
	p.input <- []byte("hello\n")
	wait(t, rxCount(s, "session-s1", 6))
	p.input <- []byte("world\n")
	wait(t, rxCount(s, "session-s1", 12))
	if ok, _ := s.HasExportable("session-s1", "all", nil); !ok {
		t.Fatal("cached records are exportable with capture off")
	}
	file := filepath.Join(t.TempDir(), "log.txt")
	n, e := s.Export(file, ExportOptions{ID: "session-s1", Scope: "all", Format: "txt", Encoding: "utf-8"})
	if e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(file)
	text := string(data)
	if n < 1 || !strings.Contains(text, "hello") || !strings.Contains(text, "world") {
		t.Fatalf("export lost cached records (n=%d):\n%s", n, text)
	}
	if !strings.HasPrefix(text, "# 串口助手 v9.9.9 · COM7\n") || strings.Contains(text, "v0.1.1") {
		t.Fatalf("header must carry the real version and port:\n%s", text)
	}
}

// Capture switched on mid-session: journal and cache overlap. Every record
// appears exactly once, in sequence order.
func TestExportAllMergesJournalAndCacheWithoutDuplicates(t *testing.T) {
	s, p := exportService(t)
	o := opts("COM8")
	o.Capture = false
	if _, e := s.Open("session-s1", o); e != nil {
		t.Fatal(e)
	}
	p.input <- []byte("before")
	wait(t, rxCount(s, "session-s1", 6))
	if _, e := s.Capture("session-s1", true); e != nil {
		t.Fatal(e)
	}
	p.input <- []byte("after1")
	wait(t, rxCount(s, "session-s1", 12))
	p.input <- []byte("after2")
	wait(t, rxCount(s, "session-s1", 18))
	file := filepath.Join(t.TempDir(), "log.jsonl")
	n, e := s.Export(file, ExportOptions{ID: "session-s1", Scope: "all", Format: "jsonl", Encoding: "utf-8"})
	if e != nil {
		t.Fatal(e)
	}
	f, _ := os.Open(file)
	defer f.Close()
	scan := bufio.NewScanner(f)
	var seqs []uint64
	var payload bytes.Buffer
	for scan.Scan() {
		var r Record
		if json.Unmarshal(scan.Bytes(), &r) != nil || r.Type != "record" {
			continue
		}
		seqs = append(seqs, r.Seq)
		raw, _ := base64.StdEncoding.DecodeString(r.DataBase64)
		payload.Write(raw)
	}
	if n != len(seqs) || len(seqs) == 0 {
		t.Fatalf("count %d, records %d", n, len(seqs))
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Fatalf("records out of order or duplicated: %v", seqs)
		}
	}
	got := payload.String()
	for _, want := range []string{"before", "after1", "after2"} {
		if strings.Count(got, want) != 1 {
			t.Fatalf("%q should appear once in %q", want, got)
		}
	}
}

// 清屏 empties the display cache only; 保存日志 still has everything, while the
// cache-only scope has nothing left and writes no file.
func TestClearKeepsFullHistoryAndEmptyScopeWritesNoFile(t *testing.T) {
	s, p := exportService(t)
	o := opts("COM9")
	o.Capture = false
	if _, e := s.Open("session-s1", o); e != nil {
		t.Fatal(e)
	}
	p.input <- []byte("kept")
	wait(t, rxCount(s, "session-s1", 4))
	if _, e := s.Clear("session-s1"); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "cache.txt")
	if _, e := s.Export(file, ExportOptions{ID: "session-s1", Scope: "cache", Format: "txt", Encoding: "utf-8"}); !errors.Is(e, ErrNothingToExport) {
		t.Fatalf("cleared cache: want ErrNothingToExport, got %v", e)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("no file or temp file may be written: %v", entries)
	}
	if ok, _ := s.HasExportable("session-s1", "all", nil); !ok {
		t.Fatal("full history survives 清屏")
	}
	all := filepath.Join(dir, "all.txt")
	if _, e := s.Export(all, ExportOptions{ID: "session-s1", Scope: "all", Format: "txt", Encoding: "utf-8"}); e != nil {
		t.Fatal(e)
	}
	if data, _ := os.ReadFile(all); !strings.Contains(string(data), "kept") {
		t.Fatalf("history lost data after 清屏:\n%s", data)
	}
}

// The save buffer keeps more than the display cache: with 落盘 never touched,
// every one of cacheMaxRecords+N received chunks is saved.
func TestFullHistoryRetainsMoreThanTheDisplayCache(t *testing.T) {
	s, p := exportService(t)
	o := opts("COM10")
	o.Capture = false
	if _, e := s.Open("session-s1", o); e != nil {
		t.Fatal(e)
	}
	total := cacheMaxRecords + 2500
	go func() {
		for i := 0; i < total; i++ {
			p.input <- []byte{byte('a' + i%26)}
		}
	}()
	deadline := time.Now().Add(30 * time.Second)
	for {
		v, _ := s.Snapshot("session-s1")
		if v.(map[string]any)["rx"].(uint64) == uint64(total) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("receive timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
	snap, _ := s.Snapshot("session-s1")
	m := snap.(map[string]any)
	cache, _, _ := s.Records("session-s1", "cache", nil)
	if len(cache) > cacheMaxRecords {
		t.Fatalf("display cache %d exceeds its cap", len(cache))
	}
	if m["history"].(int) <= cacheMaxRecords || m["capture"].(bool) {
		t.Fatalf("history %v should exceed the display cap without 落盘", m["history"])
	}
	file := filepath.Join(t.TempDir(), "all.jsonl")
	n, e := s.Export(file, ExportOptions{ID: "session-s1", Scope: "all", Format: "jsonl", Encoding: "utf-8"})
	if e != nil {
		t.Fatal(e)
	}
	var bytesSaved int
	f, _ := os.Open(file)
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var r Record
		if json.Unmarshal(scan.Bytes(), &r) == nil && r.Type == "record" && r.Direction == "RX" {
			bytesSaved += r.Length
		}
	}
	if bytesSaved != total || n <= cacheMaxRecords {
		t.Fatalf("saved %d of %d received bytes in %d records", bytesSaved, total, n)
	}
}

// Past the history budget the oldest records go first and are counted.
func TestHistoryBudgetDropsOldestAndCounts(t *testing.T) {
	old := historyMaxBytes
	historyMaxBytes = 64 * 1024
	defer func() { historyMaxBytes = old }()
	s, p := exportService(t)
	o := opts("COM11")
	o.Capture = false
	if _, e := s.Open("session-s1", o); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 40; i++ {
		p.input <- bytes.Repeat([]byte{'x'}, 4096)
	}
	wait(t, rxCount(s, "session-s1", 40*4096))
	snap, _ := s.Snapshot("session-s1")
	if snap.(map[string]any)["historyTrimmed"].(uint64) == 0 {
		t.Fatal("over budget must trim and count")
	}
	file := filepath.Join(t.TempDir(), "all.txt")
	if _, e := s.Export(file, ExportOptions{ID: "session-s1", Scope: "all", Format: "txt", Encoding: "utf-8"}); e != nil {
		t.Fatal(e)
	}
	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "内存上限") {
		t.Fatalf("export must say that old records were dropped:\n%.300s", data)
	}
}
