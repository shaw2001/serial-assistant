package core

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type mockPort struct {
	input  chan []byte
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
	writes [][]byte
	fail   bool
	block  bool
	rest   []byte
}

func newMock() *mockPort {
	return &mockPort{input: make(chan []byte, 100), closed: make(chan struct{})}
}
func (p *mockPort) Read(b []byte) (int, error) {
	if len(p.rest) > 0 {
		n := copy(b, p.rest)
		p.rest = p.rest[n:]
		return n, nil
	}
	select {
	case data := <-p.input:
		n := copy(b, data)
		p.rest = data[n:]
		return n, nil
	case <-p.closed:
		return 0, errors.New("closed")
	}
}
func (p *mockPort) Write(b []byte) (int, error) {
	if p.block {
		<-p.closed
		return 0, errors.New("closed")
	}
	if p.fail {
		return 0, errors.New("write failed")
	}
	p.mu.Lock()
	p.writes = append(p.writes, append([]byte{}, b...))
	p.mu.Unlock()
	return len(b), nil
}
func (p *mockPort) Drain() error { return nil }
func (p *mockPort) Close() error { p.once.Do(func() { close(p.closed) }); return nil }
func opts(path string) Options {
	return Options{Path: path, BaudRate: 115200, DataBits: 8, Parity: "none", StopBits: 1, Flow: "none", Capture: true}
}
func text(content string) Payload {
	return Payload{Content: content, Mode: "text", Encoding: "utf-8", Ending: "crlf"}
}
func wait(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second * 3)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("timed out")
}
func TestPayloadEncodingAndBounds(t *testing.T) {
	cases := []struct {
		p    Payload
		want []byte
	}{{text("中文"), []byte("中文\r\n")}, {Payload{"00 FF", "hex", "utf-8", "none"}, []byte{0, 255}}, {Payload{"温度", "text", "gbk", "none"}, []byte{0xce, 0xc2, 0xb6, 0xc8}}}
	for _, c := range cases {
		b, e := c.p.Bytes()
		if e != nil || !bytes.Equal(b, c.want) {
			t.Fatalf("%x %v", b, e)
		}
	}
	for _, p := range []Payload{{"A", "hex", "utf-8", "none"}, {"00 X1", "hex", "utf-8", "none"}, {"中文", "text", "ascii", "none"}, {"🙂", "text", "gbk", "none"}, {string(make([]byte, 65536)), "text", "utf-8", "crlf"}} {
		if _, e := p.Bytes(); e == nil {
			t.Fatal("expected invalid payload")
		}
	}
}
func TestIndependentPortsReopenAndCapture(t *testing.T) {
	var mu sync.Mutex
	ports := map[string]*mockPort{}
	s := NewService(t.TempDir(), func(o Options) (Port, error) {
		p := newMock()
		mu.Lock()
		ports[o.Path] = p
		mu.Unlock()
		return p, nil
	})
	defer s.Shutdown()
	for i, id := range []string{"session-one", "session-two", "session-three", "session-four"} {
		if _, e := s.Open(id, opts([]string{"COM1", "COM2", "COM3", "COM4"}[i])); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Open("session-five", opts("COM5")); e == nil {
		t.Fatal("session limit")
	}
	if _, e := s.Open("session-one", opts("COM2")); e == nil {
		t.Fatal("duplicate/connected")
	}
	raw := make([]byte, 256)
	for i := range raw {
		raw[i] = byte(i)
	}
	ports["COM1"].input <- raw
	wait(t, func() bool { v, _ := s.Snapshot("session-one"); return v.(map[string]any)["rx"].(uint64) == 256 })
	if _, e := s.Send("session-two", text("ps")); e != nil {
		t.Fatal(e)
	}
	if len(ports["COM1"].writes) != 0 {
		t.Fatal("cross port send")
	}
	file := filepath.Join(t.TempDir(), "all.jsonl")
	if e := s.Export(file, ExportOptions{ID: "session-one", Scope: "all", Format: "jsonl", Encoding: "utf-8"}); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(file)
	if !bytes.Contains(data, []byte(base64.StdEncoding.EncodeToString(raw))) {
		t.Fatal("raw export lost bytes")
	}
	for i := 0; i < 30; i++ {
		s.Close("session-one")
		o := opts("COM1")
		o.BaudRate = 1000000
		if _, e := s.Open("session-one", o); e != nil {
			t.Fatal(e)
		}
	}
	v, _ := s.Snapshot("session-one")
	if v.(map[string]any)["baudRate"] != 1000000 {
		t.Fatal("baud not applied")
	}
}
func TestWriteFailuresAndCloseDuringSend(t *testing.T) {
	p := newMock()
	p.fail = true
	s := NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	s.Open("session-one", opts("COM1"))
	if _, e := s.Send("session-one", text("ps")); e == nil {
		t.Fatal("write should fail")
	}
	v, _ := s.Snapshot("session-one")
	if v.(map[string]any)["tx"] != uint64(0) {
		t.Fatal("failed TX counted")
	}
	s.Shutdown()
	p = newMock()
	p.block = true
	s = NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	s.Open("session-one", opts("COM1"))
	done := make(chan struct{})
	go func() { s.Send("session-one", text("ps")); close(done) }()
	time.Sleep(10 * time.Millisecond)
	s.Close("session-one")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not cancel send")
	}
	s.Shutdown()
}
func TestPeriodicStopsAndDisplayBounded(t *testing.T) {
	p := newMock()
	s := NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	defer s.Shutdown()
	s.Open("session-one", opts("COM1"))
	s.StartPeriodic("session-one", text("ps"), 10)
	time.Sleep(60 * time.Millisecond)
	s.StopPeriodic("session-one")
	v, _ := s.Snapshot("session-one")
	if v.(map[string]any)["periodic"] != nil {
		t.Fatal("periodic remains")
	}
	for i := 0; i < 30; i++ {
		p.input <- bytes.Repeat([]byte{65}, 65536)
	}
	wait(t, func() bool { v, _ := s.Snapshot("session-one"); return v.(map[string]any)["rx"].(uint64) >= 30*65536 })
	s.mu.Lock()
	if s.eventBytes > 4*1024*1024 || len(s.sessions["session-one"].records) > 10000 {
		t.Fatal("queue not bounded")
	}
	s.mu.Unlock()
}
func TestConfigValidationAndAtomicSave(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	data, _ := json.Marshal(DefaultConfig())
	if e := SaveConfig(file, data); e != nil {
		t.Fatal(e)
	}
	_, warning := LoadConfig(file)
	if warning != "" {
		t.Fatal(warning)
	}
	if e := SaveConfig(file, []byte(`{"schema":1,"devices":[],"commands":{}}`)); e == nil {
		t.Fatal("bad groups accepted")
	}
	data, _ = os.ReadFile(file)
	if e := ValidateConfig(data); e != nil {
		t.Fatal("prior config damaged")
	}
}
