//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeJLink writes a shell script named like JLinkRTTLogger into a temp
// "J-Link install folder". The script gets the same arguments the real logger
// would; its output file is the last argument.
func fakeJLink(t *testing.T, body string) RTTOptions {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a; do out=$a; done\necho \"$@\" > \"" + filepath.Join(dir, "args.txt") + "\"\n" + body
	if e := os.WriteFile(filepath.Join(dir, rttLoggerName), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	o := DefaultRTTOptions()
	o.Dir = dir
	o.Backend = "logger"
	old := rttOutputDir
	out := t.TempDir()
	rttOutputDir = func() string { return out }
	t.Cleanup(func() { rttOutputDir = old })
	return o
}

const fakeBanner = `echo "SEGGER J-Link RTT Logger"
echo "Connecting to J-Link via USB...OK"
echo "Connecting to target...OK"
echo "Getting RTT data from target. Press any key to quit."
echo "------------------------------------------------------------"
`

func rttService(t *testing.T) *Service {
	s := NewService(t.TempDir(), func(o Options) (Port, error) { return openRTT(*o.RTT) })
	t.Cleanup(s.Shutdown)
	return s
}

func rttOpts(r RTTOptions) Options {
	return Options{Kind: "rtt", Path: "RTT · " + r.Device, RTT: &r}
}

func logText(s *Service, id string) string {
	recs, _, _ := s.Records(id, "cache", nil)
	var b strings.Builder
	for _, r := range recs {
		b.WriteString(r.Text + "\n")
	}
	return b.String()
}

func TestRTTLoggerBytesFlowIntoTheReceivePipelineAndExport(t *testing.T) {
	o := fakeJLink(t, fakeBanner+`printf 'boot ok\r\n' >> "$out"
sleep 0.2
printf 'temp=24.6\r\n' >> "$out"
printf '\r\nTransfer rate: 0 Bytes/s Data written: 22 Bytes\r'
read key
echo "J-Link RTT Logger is shutting down..."
`)
	s := rttService(t)
	snap, e := s.Open("session-r1", rttOpts(o))
	if e != nil {
		t.Fatal(e)
	}
	if snap.(map[string]any)["status"] != "connected" {
		t.Fatal(snap)
	}
	wait(t, rxCount(s, "session-r1", 20))
	text := logText(s, "session-r1")
	for _, want := range []string{"J-Link：Connecting to target...OK", "已连接 J-Link RTT · nRF54L15_M33 · SWD 4000 kHz · 通道 0 · 只读"} {
		if !strings.Contains(text, want) {
			t.Fatalf("log misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Transfer rate") {
		t.Fatal("the logger's rate ticker must not flood the log")
	}
	args, _ := os.ReadFile(filepath.Join(o.Dir, "args.txt"))
	if !strings.HasPrefix(string(args), "-Device nRF54L15_M33 -If SWD -Speed 4000 -RTTChannel 0 ") {
		t.Fatalf("args: %s", args)
	}
	// 保存日志 (full history) has the RTT bytes, exactly like serial data.
	file := filepath.Join(t.TempDir(), "rtt.txt")
	if _, e := s.Export(file, ExportOptions{ID: "session-r1", Scope: "all", Format: "txt", Encoding: "utf-8"}); e != nil {
		t.Fatal(e)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), "boot ok") || !strings.Contains(string(data), "temp=24.6") {
		t.Fatalf("export:\n%s", data)
	}
	// Receive-only.
	if _, e := s.Send("session-r1", text2("x")); e == nil || !strings.Contains(e.Error(), "只读") {
		t.Fatalf("send: %v", e)
	}
	out := strings.TrimSpace(strings.Fields(string(args))[len(strings.Fields(string(args)))-1])
	start := time.Now()
	if _, e := s.Close("session-r1"); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 1200*time.Millisecond {
		t.Fatal("close should end the logger via its key press, not by timeout")
	}
	if _, e := os.Stat(out); !os.IsNotExist(e) {
		t.Fatal("temporary RTT file must be removed on close")
	}
	if !strings.Contains(logText(s, "session-r1"), "J-Link RTT 已断开。") {
		t.Fatal("close must be logged")
	}
}

func text2(c string) Payload {
	return Payload{Content: c, Mode: "text", Encoding: "utf-8", Ending: "none"}
}

func TestRTTRejectedDeviceNameShowsJLinkText(t *testing.T) {
	o := fakeJLink(t, `echo "SEGGER J-Link RTT Logger"
echo "Connecting to J-Link via USB...OK"
echo "Connecting to target...ERROR: Unknown device \"$2\""
echo "Shutting down..."
exit 1
`)
	o.Device = "nRF54L15_xxAA"
	s := rttService(t)
	_, e := s.Open("session-r1", rttOpts(o))
	if e == nil || !strings.Contains(e.Error(), "不认识目标芯片名称“nRF54L15_xxAA”") || !strings.Contains(e.Error(), `Unknown device "nRF54L15_xxAA"`) {
		t.Fatalf("got %v", e)
	}
	if !strings.Contains(logText(s, "session-r1"), "连接失败：J-Link 不认识目标芯片名称") {
		t.Fatal("failure must be visible in the log")
	}
}

func TestRTTErrorWithoutExitIsStillReportedQuickly(t *testing.T) {
	// The logger prints an error but then waits for a key press.
	o := fakeJLink(t, `echo "Connecting to J-Link via USB...FAILED"
echo "ERROR: Could not connect to J-Link."
read key
`)
	start := time.Now()
	_, e := openRTT(o)
	if e == nil || !strings.Contains(e.Error(), "无法连接 J-Link 调试器") {
		t.Fatalf("got %v", e)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("too slow")
	}
}

func TestRTTPromptGetsDefaultAnswer(t *testing.T) {
	old := rttPromptIdle
	rttPromptIdle = 150 * time.Millisecond
	defer func() { rttPromptIdle = old }()
	o := fakeJLink(t, `printf 'RTT control block address. Default: auto-detection > '
read answer
`+fakeBanner+`printf 'hi' >> "$out"
read key
`)
	p, e := openRTT(o)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	if st := strings.Join(p.(*rttPort).TakeStatus(), "\n"); !strings.Contains(st, "已使用默认值：RTT control block address") {
		t.Fatalf("status: %s", st)
	}
}

func TestRTTLoggerExitMidSessionMarksError(t *testing.T) {
	o := fakeJLink(t, fakeBanner+`printf 'last words\n' >> "$out"
sleep 0.2
echo "ERROR: Connection to target lost."
exit 2
`)
	s := rttService(t)
	if _, e := s.Open("session-r1", rttOpts(o)); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool {
		v, _ := s.Snapshot("session-r1")
		return v.(map[string]any)["status"] == "error"
	})
	text := logText(s, "session-r1")
	if !strings.Contains(text, "last words") && !strings.Contains(text, "J-Link RTT 已断开") {
		t.Fatal(text)
	}
	if !strings.Contains(text, "J-Link RTT 已断开：") || !strings.Contains(text, "Connection to target lost") {
		t.Fatalf("exit must be explained:\n%s", text)
	}
	if v, _ := s.Snapshot("session-r1"); v.(map[string]any)["rx"].(uint64) != uint64(len("last words\n")) {
		t.Fatal("bytes written before exit must be drained")
	}
}

func TestTwoRTTSessionsOnTheSameProbeConflict(t *testing.T) {
	o := fakeJLink(t, fakeBanner+"read key\n")
	s := rttService(t)
	if _, e := s.Open("session-r1", rttOpts(o)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Open("session-r2", rttOpts(o)); e == nil || !strings.Contains(e.Error(), "另一个 RTT 会话") {
		t.Fatalf("got %v", e)
	}
}
