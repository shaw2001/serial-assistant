//go:build !windows

package core

import (
	"bufio"
	b64std "encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as a fake J-Link Commander: a shell script named
// JLinkExe re-executes it with FAKE_JLINK_MODE set. The fake prints
// Commander-like output, serves an RTT-Telnet-like socket on -RTTTelnetPort
// that greets like J-Link, records what the host sends (down-channel) and
// echoes it back as up-channel data.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_JLINK_MODE"); mode != "" {
		os.Exit(fakeCommander(mode, os.Getenv("FAKE_JLINK_DIR"), os.Args[1:]))
	}
	os.Exit(m.Run())
}

func argValue(args []string, name string) string {
	for i := 0; i+1 < len(args); i++ {
		if strings.EqualFold(args[i], name) {
			return args[i+1]
		}
	}
	return ""
}

func fakeCommander(mode, dir string, args []string) int {
	os.WriteFile(filepath.Join(dir, "args.txt"), []byte(strings.Join(args, "\n")), 0600)
	script, _ := os.ReadFile(argValue(args, "-CommandFile"))
	os.WriteFile(filepath.Join(dir, "script.txt"), script, 0600)
	port, _ := strconv.Atoi(argValue(args, "-RTTTelnetPort"))
	out := os.Stdout
	fmt.Fprint(out, "SEGGER J-Link Commander V7.94i (Compiled Feb 20 2024 17:00:00)\r\nDLL version V7.94i, compiled Feb 20 2024 16:59:00\r\n\r\nConnecting to J-Link via USB...O.K.\r\nFirmware: J-Link OB-nRF5340-NordicSemi compiled Feb 20 2024\r\nVTref=3.300V\r\nDevice \""+strings.ToUpper(argValue(args, "-Device"))+"\" selected.\r\n\r\n")
	stdin := bufio.NewReader(os.Stdin)
	waitExit := func() {
		for {
			line, e := stdin.ReadString('\n')
			if e != nil || strings.TrimSpace(line) == "exit" {
				return
			}
		}
	}
	switch mode {
	case "device":
		fmt.Fprint(out, "Unknown device \""+argValue(args, "-Device")+"\".\r\nPlease specify device / core. <Default>: NRF54L15_M33\r\nType '?' for selection dialog\r\nDevice>")
		waitExit()
		return 1
	case "ask":
		fmt.Fprint(out, "Please specify device / core. <Default>: NRF54L15_M33\r\nType '?' for selection dialog\r\nDevice>")
		waitExit()
		return 1
	case "target":
		fmt.Fprint(out, "Connecting to target via SWD\r\nCannot connect to target.\r\n")
		return 1
	case "late-target":
		time.Sleep(400 * time.Millisecond)
		fmt.Fprint(out, "Connecting to target via SWD\r\nCannot connect to target.\r\n")
		return 1
	case "quiet-exit":
		time.Sleep(300 * time.Millisecond)
		return 1
	}
	fmt.Fprint(out, "Connecting to target via SWD\r\nFound SW-DP with ID 0x6BA02477\r\nCortex-M33 identified.\r\n")
	for _, l := range strings.Split(strings.TrimSpace(string(script)), "\r\n") {
		if strings.HasPrefix(l, "exec") {
			fmt.Fprint(out, "J-Link>"+l+"\r\n")
		}
	}
	if mode == "noserver" {
		fmt.Fprint(out, "J-Link>")
		waitExit()
		return 0
	}
	// Like the real tool on a pipe: no "J-Link>" prompt is ever seen, and the
	// RTT Telnet server comes up a moment after start while J-Link connects.
	time.Sleep(fakeServerDelay)
	l, e := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if e != nil {
		fmt.Fprint(out, "ERROR: RTT telnet port busy\r\n")
		return 3
	}
	var mu sync.Mutex
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.WriteString(c, "SEGGER J-Link V7.94i - Real time terminal output\r\nJ-Link OB-nRF5340-NordicSemi compiled Feb 20 2024, SN=1050000000\r\nProcess: JLinkExe\r\n")
				c.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				cfg := make([]byte, 256)
				n, _ := c.Read(cfg)
				os.WriteFile(filepath.Join(dir, "config.txt"), cfg[:n], 0600)
				c.SetReadDeadline(time.Time{})
				io.WriteString(c, "*** Booting nRF54L15 ***\r\n")
				if mode == "die" {
					time.Sleep(300 * time.Millisecond)
					fmt.Fprint(out, "\r\nERROR: Target connection lost.\r\n")
					os.Exit(2)
				}
				buf := make([]byte, 1024)
				for {
					n, e := c.Read(buf)
					if n > 0 {
						mu.Lock()
						f, _ := os.OpenFile(filepath.Join(dir, "down.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
						f.Write(buf[:n])
						f.Close()
						mu.Unlock()
						c.Write(append([]byte("echo:"), buf[:n]...))
					}
					if e != nil {
						return
					}
				}
			}(c)
		}
	}()
	waitExit()
	l.Close()
	return 0
}

const fakeServerDelay = 700 * time.Millisecond

func fakeCommanderDir(t *testing.T, mode string) RTTOptions {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nFAKE_JLINK_MODE=%s FAKE_JLINK_DIR='%s' exec '%s' \"$@\"\n", mode, dir, exe)
	if e := os.WriteFile(filepath.Join(dir, jlinkCommanderName), []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	o := DefaultRTTOptions()
	o.Dir = dir
	o.TelnetPort = freePort(t)
	old := rttOutputDir
	tmp := t.TempDir()
	rttOutputDir = func() string { return tmp }
	t.Cleanup(func() { rttOutputDir = old })
	return o
}

func freePort(t *testing.T) int {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func readFile(dir, name string) string {
	b, _ := os.ReadFile(filepath.Join(dir, name))
	return string(b)
}

func TestCommanderRTTReceivesAndSendsToDownChannel0(t *testing.T) {
	o := fakeCommanderDir(t, "ok")
	s := rttService(t)
	start := time.Now()
	if _, e := s.Open("session-c1", rttOpts(o)); e != nil {
		t.Fatal(e)
	}
	// Connected by polling the telnet port: right after the server is up,
	// without waiting for a Commander prompt (the fake never prints one).
	if took := time.Since(start); took < fakeServerDelay || took > fakeServerDelay+1500*time.Millisecond {
		t.Fatalf("connect took %s; want shortly after the %s server delay", took, fakeServerDelay)
	}
	// The greeting is not data; the first RTT bytes are.
	wait(t, rxCount(s, "session-c1", uint64(len("*** Booting nRF54L15 ***\r\n"))))
	// Send through the normal path: text + CRLF ending, like a serial send.
	if _, e := s.Send("session-c1", text("ps")); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return readFile(o.Dir, "down.txt") == "ps\r\n" })
	wait(t, rxCount(s, "session-c1", uint64(len("*** Booting nRF54L15 ***\r\n")+len("echo:ps\r\n"))))
	// HEX payload reaches the down-channel byte-exact.
	if _, e := s.Send("session-c1", Payload{Content: "00 FF 7E", Mode: "hex", Encoding: "utf-8", Ending: "none"}); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool { return readFile(o.Dir, "down.txt") == "ps\r\n\x00\xff\x7e" })
	recs, _, _ := s.Records("session-c1", "cache", nil)
	var rx, tx strings.Builder
	sys := ""
	for _, r := range recs {
		switch r.Direction {
		case "RX":
			rx.WriteString(decodeB64(r.DataBase64))
		case "TX":
			tx.WriteString(decodeB64(r.DataBase64))
		case "SYS":
			sys += r.Text + "\n"
		}
	}
	if strings.Contains(rx.String(), "Real time terminal output") || !strings.HasPrefix(rx.String(), "*** Booting nRF54L15 ***\r\necho:ps\r\n") {
		t.Fatalf("rx %q", rx.String())
	}
	if tx.String() != "ps\r\n\x00\xff\x7e" {
		t.Fatalf("tx %q", tx.String())
	}
	for _, want := range []string{"J-Link：Cortex-M33 identified.", "J-Link RTT Telnet：SEGGER J-Link V7.94i - Real time terminal output", "发送写入下行通道 0", "已连接 J-Link RTT · nRF54L15_M33 · SWD 4000 kHz · 通道 0（可收发：发送写入 RTT 下行通道 0）"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("log misses %q:\n%s", want, sys)
		}
	}
	args := strings.Split(readFile(o.Dir, "args.txt"), "\n")
	if strings.Join(args[:15], " ") != "-Device nRF54L15_M33 -If SWD -Speed 4000 -AutoConnect 1 -ExitOnError 1 -NoGui 1 -RTTTelnetPort "+strconv.Itoa(o.TelnetPort)+" -CommandFile" {
		t.Fatalf("args %q", args)
	}
	wait(t, func() bool { _, e := os.Stat(filepath.Join(o.Dir, "config.txt")); return e == nil })
	if readFile(o.Dir, "config.txt") != "" {
		t.Fatal("channel 0 sends no config string")
	}
	scriptPath := args[15]
	start = time.Now()
	if _, e := s.Close("session-c1"); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("close should end Commander with exit, not by kill timeout")
	}
	if _, e := os.Stat(scriptPath); !os.IsNotExist(e) {
		t.Fatal("generated command file must be removed")
	}
}

func decodeB64(s string) string {
	r := Record{DataBase64: s}
	b, _ := base64Decode(r.DataBase64)
	return string(b)
}

func TestCommanderRTTChannelAndAddressConfig(t *testing.T) {
	o := fakeCommanderDir(t, "ok")
	o.Channel, o.AddrMode, o.Address = 1, "address", "0x20001000"
	p, e := openRTT(o)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	wait(t, func() bool { return readFile(o.Dir, "config.txt") == "$$SEGGER_TELNET_ConfigStr=RTTCh;1$$" })
	if !strings.Contains(readFile(o.Dir, "script.txt"), "exec SetRTTAddr 0x20001000") {
		t.Fatal(readFile(o.Dir, "script.txt"))
	}
}

func TestCommanderTelnetPortFallback(t *testing.T) {
	o := fakeCommanderDir(t, "ok")
	l, e := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(o.TelnetPort))
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	p, e := openRTT(o)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	got := argValue(strings.Split(readFile(o.Dir, "args.txt"), "\n"), "-RTTTelnetPort")
	if got == strconv.Itoa(o.TelnetPort) {
		t.Fatal("busy port must not be passed to J-Link")
	}
	st := strings.Join(p.(*commanderPort).TakeStatus(), "\n")
	if !strings.Contains(st, fmt.Sprintf("RTT Telnet 端口 %d 已被占用，改用 %s。", o.TelnetPort, got)) {
		t.Fatal(st)
	}
}

func TestCommanderRejectedDeviceShowsJLinkText(t *testing.T) {
	o := fakeCommanderDir(t, "device")
	o.Device = "nRF54L15_xxAA"
	old := rttPromptIdle
	rttPromptIdle = 200 * time.Millisecond
	defer func() { rttPromptIdle = old }()
	s := rttService(t)
	_, e := s.Open("session-c1", rttOpts(o))
	if e == nil || !strings.Contains(e.Error(), "不认识目标芯片名称“nRF54L15_xxAA”") || !strings.Contains(e.Error(), `Unknown device "nRF54L15_xxAA"`) {
		t.Fatalf("got %v", e)
	}
	if !strings.Contains(logText(s, "session-c1"), "连接失败：J-Link 不认识目标芯片名称") {
		t.Fatal(logText(s, "session-c1"))
	}
}

func TestCommanderErrorsBeforeTelnetAreReportedImmediately(t *testing.T) {
	for _, mode := range []string{"target", "late-target"} {
		o := fakeCommanderDir(t, mode)
		start := time.Now()
		_, e := openRTT(o)
		if e == nil || !strings.Contains(e.Error(), "无法连接目标芯片") || !strings.Contains(e.Error(), "Cannot connect to target.") {
			t.Fatalf("%s: got %v", mode, e)
		}
		if took := time.Since(start); took > 2*time.Second {
			t.Fatalf("%s: error took %s; must not wait for the connect timeout", mode, took)
		}
	}
	o := fakeCommanderDir(t, "quiet-exit")
	start := time.Now()
	if _, e := openRTT(o); e == nil || !strings.Contains(e.Error(), "退出码 1") {
		t.Fatalf("quiet exit: %v", e)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("quiet exit took %s", took)
	}
}

func TestCommanderTelnetRefused(t *testing.T) {
	o := fakeCommanderDir(t, "noserver")
	old := commanderConnectTimeout
	commanderConnectTimeout = 1200 * time.Millisecond
	defer func() { commanderConnectTimeout = old }()
	start := time.Now()
	_, e := openRTT(o)
	if e == nil || !strings.Contains(e.Error(), "RTT Telnet 服务 127.0.0.1:"+strconv.Itoa(o.TelnetPort)+" 仍无法连接") || !strings.Contains(e.Error(), "Cortex-M33 identified.") {
		t.Fatalf("got %v", e)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("too slow")
	}
}

func TestCommanderMissing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, rttLoggerName), []byte("#!/bin/sh\n"), 0700)
	o := DefaultRTTOptions()
	o.Dir = dir
	if _, e := openRTT(o); e == nil || !strings.Contains(e.Error(), "没有找到 "+jlinkCommanderName+"（J-Link Commander）") || !strings.Contains(e.Error(), "JLinkRTTLogger（只读）") {
		t.Fatalf("got %v", e)
	}
}

func TestCommanderExitMidSessionIsExplained(t *testing.T) {
	o := fakeCommanderDir(t, "die")
	s := rttService(t)
	if _, e := s.Open("session-c1", rttOpts(o)); e != nil {
		t.Fatal(e)
	}
	wait(t, func() bool {
		v, _ := s.Snapshot("session-c1")
		return v.(map[string]any)["status"] == "error"
	})
	text := logText(s, "session-c1")
	if !strings.Contains(text, "J-Link RTT 已断开：J-Link Commander 已退出。") || !strings.Contains(text, "Target connection lost") {
		t.Fatal(text)
	}
}

func base64Decode(s string) ([]byte, error) { return b64.DecodeString(s) }

var b64 = b64std.StdEncoding

func TestCommanderDeviceQuestionIsAnErrorNotAWait(t *testing.T) {
	old := rttPromptIdle
	rttPromptIdle = 200 * time.Millisecond
	defer func() { rttPromptIdle = old }()
	o := fakeCommanderDir(t, "ask")
	start := time.Now()
	_, e := openRTT(o)
	if e == nil || !strings.Contains(e.Error(), "正在询问设备名") || !strings.Contains(e.Error(), "Device>") {
		t.Fatalf("got %v", e)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Fatalf("took %s", took)
	}
}
