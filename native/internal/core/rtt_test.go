package core

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRTTDefaultsAndLoggerCommandLine(t *testing.T) {
	o := DefaultRTTOptions()
	if o.Dir != `C:\Program Files\SEGGER\JLink_V794i` || o.Device != "nRF54L15_M33" || o.Iface != "SWD" || o.Speed != 4000 || o.Channel != 0 || o.AddrMode != "auto" || o.Backend != "commander" || o.TelnetPort != 19021 {
		t.Fatalf("defaults: %+v", o)
	}
	if e := o.Validate(); e != nil {
		t.Fatal(e)
	}
	out := `C:\Users\me\AppData\Local\Temp\SerialAssistant\rtt\rtt-1.log`
	got := CommandLine(`C:\Program Files\SEGGER\JLink_V794i\JLinkRTTLogger.exe`, o.LoggerArgs(out))
	want := `"C:\Program Files\SEGGER\JLink_V794i\JLinkRTTLogger.exe" -Device nRF54L15_M33 -If SWD -Speed 4000 -RTTChannel 0 C:\Users\me\AppData\Local\Temp\SerialAssistant\rtt\rtt-1.log`
	if got != want {
		t.Fatalf("command line\n got %s\nwant %s", got, want)
	}
	o.Iface, o.Speed, o.Channel, o.Serial = "JTAG", 1000, 2, "123456"
	o.AddrMode, o.Address = "address", "0x20001000"
	if got := strings.Join(o.LoggerArgs("o.log"), " "); got != "-Device nRF54L15_M33 -If JTAG -Speed 1000 -USB 123456 -RTTAddress 0x20001000 -RTTChannel 2 o.log" {
		t.Fatal(got)
	}
	o.AddrMode, o.Ranges = "ranges", "0x20000000 0x40000"
	args := o.LoggerArgs(`C:\Temp dir\o.log`)
	if got := CommandLine("JLinkRTTLogger.exe", args); !strings.Contains(got, `-RTTSearchRanges "0x20000000 0x40000" -RTTChannel 2 "C:\Temp dir\o.log"`) {
		t.Fatal(got)
	}
	if args[len(args)-1] != `C:\Temp dir\o.log` {
		t.Fatal("output file must be the last argument")
	}
}

func TestQuoteArgFollowsWindowsRules(t *testing.T) {
	for in, want := range map[string]string{"": `""`, "a": "a", `a b`: `"a b"`, `a"b`: `"a\"b"`, `C:\dir with space\`: `"C:\dir with space\\"`, `x\"y z`: `"x\\\"y z"`} {
		if got := quoteArg(in); got != want {
			t.Errorf("quoteArg(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestRTTValidateRejectsBadSettings(t *testing.T) {
	bad := map[string]func(*RTTOptions){
		"J-Link 软件路径": func(o *RTTOptions) { o.Dir = " " },
		"路径无效":        func(o *RTTOptions) { o.Dir = `C:\a"b` },
		"目标芯片名称无效":    func(o *RTTOptions) { o.Device = "nRF54L15 -If JTAG" },
		"SWD 或 JTAG":  func(o *RTTOptions) { o.Iface = "cJTAG" },
		"kHz":         func(o *RTTOptions) { o.Speed = 0 },
		"RTT 通道":      func(o *RTTOptions) { o.Channel = -1 },
		"十六进制":        func(o *RTTOptions) { o.AddrMode, o.Address = "address", "20000000" },
		"起始地址 长度":     func(o *RTTOptions) { o.AddrMode, o.Ranges = "ranges", "0x20000000" },
		"序列号":         func(o *RTTOptions) { o.Serial = "12a" },
		"控制块地址设置无效":   func(o *RTTOptions) { o.AddrMode = "manual" },
	}
	for want, f := range bad {
		o := DefaultRTTOptions()
		f(&o)
		if e := o.Validate(); e == nil || !strings.Contains(e.Error(), want) {
			t.Errorf("%s: got %v", want, e)
		}
	}
}

func TestLocateJLinkExplainsMissingPathAndTool(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "JLink_V794i")
	if _, e := LocateJLink(missing); e == nil || !strings.Contains(e.Error(), "J-Link 软件路径不存在") {
		t.Fatalf("missing dir: %v", e)
	}
	empty := t.TempDir()
	if _, e := LocateJLink(empty); e == nil || !strings.Contains(e.Error(), "没有找到 "+jlinkCommanderName+" 或 "+rttLoggerName) {
		t.Fatalf("no logger: %v", e)
	}
	if _, e := LocateJLink(""); e == nil || !strings.Contains(e.Error(), "请填写 J-Link 软件路径") {
		t.Fatalf("empty: %v", e)
	}
	dir := t.TempDir()
	logger := filepath.Join(dir, rttLoggerName)
	os.WriteFile(logger, []byte("x"), 0700)
	os.WriteFile(filepath.Join(dir, "JLink_x64.dll"), []byte("x"), 0600)
	commander := filepath.Join(dir, jlinkCommanderName)
	os.WriteFile(commander, []byte("x"), 0700)
	for _, in := range []string{dir, `"` + dir + `"`, logger, filepath.Join(dir, "JLink_x64.dll")} {
		tools, e := LocateJLink(in)
		if e != nil || tools.Logger != logger || tools.Commander != commander || tools.Dir != dir || !strings.HasSuffix(tools.DLL, "JLink_x64.dll") {
			t.Fatalf("%s: %+v %v", in, tools, e)
		}
	}
}

func TestServiceRTTMissingToolFailsVisiblyInChinese(t *testing.T) {
	s := NewService(t.TempDir(), func(o Options) (Port, error) { return openRTT(*o.RTT) })
	r := DefaultRTTOptions()
	r.Dir = filepath.Join(t.TempDir(), "nope")
	_, e := s.Open("session-r1", Options{Kind: "rtt", Path: "RTT · nRF54L15_M33", RTT: &r})
	if e == nil || !strings.Contains(e.Error(), "J-Link 软件路径不存在") {
		t.Fatalf("got %v", e)
	}
	recs, _, _ := s.Records("session-r1", "cache", nil)
	if len(recs) == 0 || !strings.Contains(recs[len(recs)-1].Text, "连接失败：J-Link 软件路径不存在") {
		t.Fatalf("the failure must be in the log: %+v", recs)
	}
	snap, _ := s.Snapshot("session-r1")
	if snap.(map[string]any)["status"] != "disconnected" {
		t.Fatal(snap)
	}
	// RTT options are checked before anything starts.
	if _, e := s.Open("session-r2", Options{Kind: "rtt", Path: "RTT"}); e == nil {
		t.Fatal("missing rtt settings must fail")
	}
	if _, e := s.Open("session-r2", Options{Kind: "usb", Path: "COM1", BaudRate: 9600, DataBits: 8, Parity: "none", StopBits: 1, Flow: "none"}); e == nil {
		t.Fatal("unknown kind must fail")
	}
}

func TestExplainJLinkErrors(t *testing.T) {
	cases := map[string][]string{
		"不认识目标芯片名称“nRF54L15_xxAA”": {"Connecting to J-Link via USB...OK", "Connecting to target...ERROR: Unknown device \"nRF54L15_xxAA\""},
		"无法连接 J-Link 调试器":          {"Connecting to J-Link via USB...FAILED", "ERROR: Could not connect to J-Link."},
		"无法连接目标芯片":                 {"Connecting to J-Link via USB...OK", "Connecting to target...ERROR: Could not connect to target."},
		"没有找到 RTT 控制块":             {"Connecting to target...OK", "ERROR: RTT control block not found."},
		"已退出（退出码 3）":               {},
	}
	for want, lines := range cases {
		got := explainJLink(lines, "nRF54L15_xxAA", 3)
		if !strings.Contains(got, want) {
			t.Errorf("%v -> %s (want %s)", lines, got, want)
		}
		if len(lines) > 0 && (!strings.Contains(got, "J-Link 输出：") || !strings.Contains(got, lines[len(lines)-1])) {
			t.Errorf("J-Link's own text must be kept: %s", got)
		}
	}
}

func TestConfigPersistsRTTDevicesAndDefaults(t *testing.T) {
	r := DefaultRTTOptions()
	r.Device, r.Channel, r.AddrMode, r.Ranges = "nRF54L15_M33", 1, "ranges", "0x20000000 0x40000"
	device := map[string]any{"id": "session-rtt1", "path": "RTT · nRF54L15_M33", "baud": "115200", "receiveEncoding": "utf-8", "params": map[string]any{"data": 8, "parity": "none", "stop": 1, "flow": "none"}, "draft": map[string]any{"content": "", "mode": "text", "encoding": "utf-8", "ending": "crlf"}, "history": []any{}, "kind": "rtt", "rtt": r}
	cfg := DefaultConfig()
	cfg["devices"] = []any{device}
	cfg["ui"] = map[string]any{"privacyVersion": 1, "jlink": r}
	data, _ := json.Marshal(cfg)
	file := filepath.Join(t.TempDir(), "config.json")
	if e := SaveConfig(file, data); e != nil {
		t.Fatal(e)
	}
	loaded, warning := LoadConfig(file)
	c := loaded.(Config)
	if warning != "" || c.Devices[0].Kind != "rtt" || c.Devices[0].RTT == nil || *c.Devices[0].RTT != r || c.UI.JLink == nil || *c.UI.JLink != r {
		t.Fatalf("round trip lost RTT settings: %+v %q", c, warning)
	}
	device["rtt"] = map[string]any{"dir": "C:\\x", "device": "bad name", "iface": "SWD", "speed": 4000, "channel": 0, "addrMode": "auto"}
	data, _ = json.Marshal(cfg)
	if e := ValidateConfig(data); e == nil || !strings.Contains(e.Error(), "J-Link RTT 设置无效") {
		t.Fatalf("bad device name must be rejected: %v", e)
	}
	delete(device, "rtt")
	data, _ = json.Marshal(cfg)
	if ValidateConfig(data) == nil {
		t.Fatal("rtt device without settings must be rejected")
	}
}

func TestCommanderCommandLineScriptAndTelnetConfig(t *testing.T) {
	o := DefaultRTTOptions()
	script := `C:\Users\me\AppData\Local\Temp\SerialAssistant\rtt\jlink-1.jlink`
	got := CommandLine(`C:\Program Files\SEGGER\JLink_V794i\JLink.exe`, o.CommanderArgs(19021, script))
	want := `"C:\Program Files\SEGGER\JLink_V794i\JLink.exe" -Device nRF54L15_M33 -If SWD -Speed 4000 -AutoConnect 1 -ExitOnError 1 -NoGui 1 -RTTTelnetPort 19021 -CommandFile C:\Users\me\AppData\Local\Temp\SerialAssistant\rtt\jlink-1.jlink`
	if got != want {
		t.Fatalf("command line\n got %s\nwant %s", got, want)
	}
	body := o.CommanderScript(19021)
	if strings.Contains(body, "exec") || !strings.Contains(body, "127.0.0.1:19021") || !strings.HasSuffix(body, "\r\n") {
		t.Fatalf("auto script: %q", body)
	}
	for _, r := range body {
		if r > 0x7e && r != '\r' && r != '\n' {
			t.Fatal("command file must be ASCII")
		}
	}
	if o.TelnetConfig() != "" {
		t.Fatal("channel 0 needs no config string (it would otherwise reach the target as data)")
	}
	o.Serial, o.Channel, o.AddrMode, o.Address = "123456", 1, "address", "0x20001000"
	if a := strings.Join(o.CommanderArgs(19022, "s.jlink"), " "); a != "-Device nRF54L15_M33 -If SWD -Speed 4000 -USB 123456 -AutoConnect 1 -ExitOnError 1 -NoGui 1 -RTTTelnetPort 19022 -CommandFile s.jlink" {
		t.Fatal(a)
	}
	if !strings.Contains(o.CommanderScript(19022), "\r\nexec SetRTTAddr 0x20001000\r\n") || o.TelnetConfig() != "$$SEGGER_TELNET_ConfigStr=RTTCh;1$$" {
		t.Fatal(o.CommanderScript(19022), o.TelnetConfig())
	}
	o.AddrMode, o.Ranges = "ranges", "0x20000000 0x40000"
	if !strings.Contains(o.CommanderScript(19022), "exec SetRTTSearchRanges 0x20000000 0x40000\r\n") {
		t.Fatal(o.CommanderScript(19022))
	}
	bad := DefaultRTTOptions()
	bad.Backend = "dll"
	if bad.Validate() == nil {
		t.Fatal("unknown backend")
	}
	bad = DefaultRTTOptions()
	bad.TelnetPort = 80
	if e := bad.Validate(); e == nil || !strings.Contains(e.Error(), "1024–65535") {
		t.Fatal(e)
	}
}

func TestStripTelnetBanner(t *testing.T) {
	greeting := "SEGGER J-Link V7.94i - Real time terminal output\r\nJ-Link OB compiled Feb 20 2024, SN=1050000000\r\nProcess: JLink.exe\r\n"
	banner, rest, done := stripTelnetBanner([]byte(greeting + "boot ok\r\n"))
	if !done || len(banner) != 3 || banner[2] != "Process: JLink.exe" || string(rest) != "boot ok\r\n" {
		t.Fatalf("%q %q %v", banner, rest, done)
	}
	if _, _, done := stripTelnetBanner([]byte(greeting[:30])); done {
		t.Fatal("partial greeting is not done")
	}
	if _, _, done := stripTelnetBanner([]byte("SEGG")); done {
		t.Fatal("a prefix of the greeting may still grow")
	}
	if b, rest, done := stripTelnetBanner([]byte("[00:00:01] hello\r\n")); !done || b != nil || string(rest) != "[00:00:01] hello\r\n" {
		t.Fatal("plain data must pass through")
	}
}

func TestPickTelnetPortFallsBackWhenBusy(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	busy := l.Addr().(*net.TCPAddr).Port
	got, e := pickTelnetPort(busy)
	if e != nil || got == busy || got < busy || got > busy+49 {
		t.Fatalf("busy %d -> %d %v", busy, got, e)
	}
	free, _ := pickTelnetPort(got)
	if free != got {
		t.Fatalf("a free port is kept: %d -> %d", got, free)
	}
}
