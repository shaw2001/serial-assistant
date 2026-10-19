package core

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// J-Link RTT sessions use SEGGER's own tools as child processes, so SEGGER's
// tested code does the probe/target connection and the RTT control-block search.
//
//   - backend "commander" (default, bidirectional): JLink.exe (J-Link Commander)
//     stays connected and J-Link's RTT Telnet server on 127.0.0.1:<port> carries
//     RTT channel N both ways: socket reads are up-channel data (the log), socket
//     writes go to the down-channel (send). See rtt_commander.go.
//   - backend "logger" (receive-only fallback): JLinkRTTLogger.exe appends the
//     raw bytes of one up-channel to a file, which rttPort tails.
//
// Either way the bytes enter the normal receive pipeline (history, display, 保存日志).

const DefaultJLinkDir = `C:\Program Files\SEGGER\JLink_V794i`

// DefaultRTTDevice is the J-Link device-database name of the nRF54L15
// application core (SEGGER supported-devices list: nRF54L15_M33 / _RV32).
const DefaultRTTDevice = "nRF54L15_M33"

type RTTOptions struct {
	Dir      string `json:"dir"`
	Device   string `json:"device"`
	Iface    string `json:"iface"`
	Speed    int    `json:"speed"`
	Channel  int    `json:"channel"`
	AddrMode string `json:"addrMode"`
	Address  string `json:"address,omitempty"`
	Ranges   string `json:"ranges,omitempty"`
	Serial   string `json:"serial,omitempty"`
	// Backend is "commander" (default; send + receive) or "logger" (receive only).
	Backend string `json:"backend,omitempty"`
	// TelnetPort is J-Link's RTT Telnet port (0 = 19021). If it is busy the
	// next free port is used.
	TelnetPort int `json:"telnetPort,omitempty"`
}

const DefaultTelnetPort = 19021

func (r RTTOptions) UsesLogger() bool { return r.Backend == "logger" }
func (r RTTOptions) telnetPort() int {
	if r.TelnetPort == 0 {
		return DefaultTelnetPort
	}
	return r.TelnetPort
}

func DefaultRTTOptions() RTTOptions {
	return RTTOptions{Dir: DefaultJLinkDir, Device: DefaultRTTDevice, Iface: "SWD", Speed: 4000, Channel: 0, AddrMode: "auto", Backend: "commander", TelnetPort: DefaultTelnetPort}
}

var (
	rttDevicePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+/-]{0,63}$`)
	rttAddressPattern = regexp.MustCompile(`^0[xX][0-9a-fA-F]{1,8}$`)
	rttRangePattern   = regexp.MustCompile(`^0[xX][0-9a-fA-F]{1,8} 0[xX][0-9a-fA-F]{1,8}(, ?0[xX][0-9a-fA-F]{1,8} 0[xX][0-9a-fA-F]{1,8}){0,7}$`)
	rttSerialPattern  = regexp.MustCompile(`^[0-9]{1,12}$`)
)

func (r RTTOptions) Validate() error {
	if strings.TrimSpace(r.Dir) == "" {
		return errors.New("请填写 J-Link 软件路径（SEGGER J-Link 安装目录，例如 " + DefaultJLinkDir + "）。")
	}
	if len(r.Dir) > 260 || strings.ContainsAny(r.Dir, "\"\r\n\t\x00") {
		return errors.New("J-Link 软件路径无效。")
	}
	if !rttDevicePattern.MatchString(r.Device) {
		return errors.New("目标芯片名称无效：请填写 J-Link 设备库中的名称，例如 " + DefaultRTTDevice + "。")
	}
	if !oneOf(r.Iface, "SWD", "JTAG") {
		return errors.New("接口只能是 SWD 或 JTAG。")
	}
	if r.Speed < 1 || r.Speed > 100000 {
		return errors.New("速度应为 1–100000 kHz。")
	}
	if r.Channel < 0 || r.Channel > 255 {
		return errors.New("RTT 通道应为 0–255。")
	}
	switch r.AddrMode {
	case "auto":
	case "address":
		if !rttAddressPattern.MatchString(r.Address) {
			return errors.New("RTT 控制块地址应为十六进制，例如 0x20000000。")
		}
	case "ranges":
		if !rttRangePattern.MatchString(r.Ranges) {
			return errors.New("RTT 搜索范围格式应为“起始地址 长度”，多段用逗号分隔，例如 0x20000000 0x40000。")
		}
	default:
		return errors.New("RTT 控制块地址设置无效。")
	}
	if !oneOf(r.Backend, "", "commander", "logger") {
		return errors.New("RTT 后端无效。")
	}
	if r.TelnetPort != 0 && (r.TelnetPort < 1024 || r.TelnetPort > 65535) {
		return errors.New("RTT Telnet 端口应为 1024–65535。")
	}
	if r.Serial != "" && !rttSerialPattern.MatchString(r.Serial) {
		return errors.New("J-Link 序列号只能是数字，留空表示自动选择。")
	}
	return nil
}

// Summary is the human-readable connection line used in logs and the UI.
func (r RTTOptions) Summary() string {
	s := fmt.Sprintf("J-Link RTT · %s · %s %d kHz · 通道 %d", r.Device, r.Iface, r.Speed, r.Channel)
	if r.UsesLogger() {
		s += " · 只读"
	}
	switch r.AddrMode {
	case "address":
		s += " · 控制块 " + r.Address
	case "ranges":
		s += " · 搜索范围 " + r.Ranges
	}
	return s
}

// LoggerArgs builds the non-interactive JLinkRTTLogger command line. Every
// value is passed explicitly so the logger never stops to prompt; the output
// file is the last positional argument.
func (r RTTOptions) LoggerArgs(output string) []string {
	a := []string{"-Device", r.Device, "-If", r.Iface, "-Speed", strconv.Itoa(r.Speed)}
	if r.Serial != "" {
		a = append(a, "-USB", r.Serial)
	}
	switch r.AddrMode {
	case "address":
		a = append(a, "-RTTAddress", r.Address)
	case "ranges":
		a = append(a, "-RTTSearchRanges", r.Ranges)
	}
	return append(a, "-RTTChannel", strconv.Itoa(r.Channel), output)
}

// CommandLine renders exe+args the way Windows (and Go's os/exec) quotes them.
func CommandLine(exe string, args []string) string {
	parts := []string{quoteArg(exe)}
	for _, a := range args {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

func quoteArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes*2+1))
			slashes = 0
		default:
			b.WriteString(strings.Repeat(`\`, slashes))
			slashes = 0
		}
		if c != '\\' {
			b.WriteByte(c)
		}
	}
	b.WriteString(strings.Repeat(`\`, slashes*2))
	b.WriteByte('"')
	return b.String()
}

var rttLoggerName = func() string {
	if runtime.GOOS == "windows" {
		return "JLinkRTTLogger.exe"
	}
	return "JLinkRTTLogger"
}()

var jlinkCommanderName = func() string {
	if runtime.GOOS == "windows" {
		return "JLink.exe"
	}
	return "JLinkExe"
}()

type JLinkTools struct {
	Dir       string `json:"dir"`
	Commander string `json:"commander,omitempty"`
	Logger    string `json:"logger,omitempty"`
	DLL       string `json:"dll,omitempty"`
}

func isFile(p string) bool { s, e := os.Stat(p); return e == nil && !s.IsDir() }

// LocateJLink accepts the J-Link install folder or a file inside it (for
// example JLinkRTTLogger.exe or JLink_x64.dll picked in a file dialog).
func LocateJLink(dir string) (JLinkTools, error) {
	dir = strings.Trim(strings.TrimSpace(dir), `"`)
	if dir == "" {
		return JLinkTools{}, errors.New("请填写 J-Link 软件路径（SEGGER J-Link 安装目录，例如 " + DefaultJLinkDir + "）。")
	}
	info, e := os.Stat(dir)
	if e != nil {
		return JLinkTools{}, fmt.Errorf("J-Link 软件路径不存在：%s。请确认已安装 SEGGER J-Link 软件，并填写其安装目录（例如 %s）。", dir, DefaultJLinkDir)
	}
	if !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	t := JLinkTools{Dir: dir}
	if p := filepath.Join(dir, jlinkCommanderName); isFile(p) {
		t.Commander = p
	}
	if p := filepath.Join(dir, rttLoggerName); isFile(p) {
		t.Logger = p
	}
	if t.Commander == "" && t.Logger == "" {
		return JLinkTools{}, fmt.Errorf("在 %s 中没有找到 %s 或 %s。请把“J-Link 软件路径”设为 SEGGER J-Link 的安装目录（例如 %s）。", dir, jlinkCommanderName, rttLoggerName, DefaultJLinkDir)
	}
	for _, name := range []string{"JLink_x64.dll", "JLinkARM.dll", "libjlinkarm.so"} {
		if _, e := os.Stat(filepath.Join(dir, name)); e == nil {
			t.DLL = filepath.Join(dir, name)
			break
		}
	}
	return t, nil
}

// RTTDiag receives facts for the diagnostics log (command line, connect
// result, byte counts). It never receives RTT payload.
var RTTDiag = func(format string, args ...any) {}

var (
	rttConnectTimeout = 20 * time.Second
	rttPromptIdle     = 800 * time.Millisecond
	rttOutputDir      = func() string { return filepath.Join(os.TempDir(), "SerialAssistant", "rtt") }
	prepareRTTCmd     = func(*exec.Cmd) {}
)

// openRTT locates the logger, starts it and waits until it reports that RTT
// data is flowing, fails, or the connect timeout passes.
func openRTT(o RTTOptions) (Port, error) {
	if e := o.Validate(); e != nil {
		return nil, e
	}
	tools, e := LocateJLink(o.Dir)
	if e != nil {
		RTTDiag("rtt locate failed dir=%q: %v", o.Dir, e)
		return nil, e
	}
	if !o.UsesLogger() {
		if tools.Commander == "" {
			e := fmt.Errorf("在 %s 中没有找到 %s（J-Link Commander），无法收发 RTT。请检查 J-Link 软件路径，或把 RTT 后端改为“JLinkRTTLogger（只读）”。", tools.Dir, jlinkCommanderName)
			RTTDiag("rtt locate failed: %v", e)
			return nil, e
		}
		return startCommander(tools.Commander, o)
	}
	if tools.Logger == "" {
		e := fmt.Errorf("在 %s 中没有找到 %s。请检查 J-Link 软件路径，或把 RTT 后端改为“J-Link Commander（收发）”。", tools.Dir, rttLoggerName)
		RTTDiag("rtt locate failed: %v", e)
		return nil, e
	}
	out, e := rttTempFile("rtt", ".log")
	if e != nil {
		return nil, e
	}
	return startRTT(tools.Logger, o.LoggerArgs(out), out, o)
}

func rttTempFile(prefix, ext string) (string, error) {
	dir := rttOutputDir()
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", fmt.Errorf("无法创建 RTT 临时目录：%v", e)
	}
	var random [6]byte
	_, _ = rand.Read(random[:])
	return filepath.Join(dir, prefix+"-"+time.Now().Format("20060102-150405")+"-"+hex.EncodeToString(random[:])+ext), nil
}

type rttPort struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	out     string
	device  string
	started time.Time

	fileMu sync.Mutex
	file   *os.File

	mu        sync.Mutex
	status    []string
	tail      []string
	partial   string
	lastOut   time.Time
	prompts   int
	emitted   int
	ready     bool
	failed    bool
	firstData bool
	bytes     uint64
	rate      string
	idle      time.Duration

	readyCh   chan struct{}
	failCh    chan struct{}
	exited    chan struct{}
	outDone   chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	exitErr   error
}

func startRTT(exe string, args []string, out string, o RTTOptions) (Port, error) {
	_ = os.Remove(out)
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	prepareRTTCmd(cmd)
	stdin, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	r, w, e := os.Pipe()
	if e != nil {
		return nil, e
	}
	cmd.Stdout, cmd.Stderr = w, w
	RTTDiag("rtt start: %s", CommandLine(exe, args))
	if e := cmd.Start(); e != nil {
		r.Close()
		w.Close()
		RTTDiag("rtt start failed: %v", e)
		return nil, fmt.Errorf("无法启动 %s：%v", filepath.Base(exe), e)
	}
	w.Close()
	p := &rttPort{idle: rttPromptIdle, cmd: cmd, stdin: stdin, out: out, device: o.Device, started: time.Now(), lastOut: time.Now(),
		readyCh: make(chan struct{}), failCh: make(chan struct{}), exited: make(chan struct{}), outDone: make(chan struct{}), closed: make(chan struct{})}
	go p.readOutput(r)
	go p.answerPrompts()
	go func() {
		<-p.outDone
		p.exitErr = cmd.Wait()
		close(p.exited)
	}()
	timer := time.NewTimer(rttConnectTimeout)
	defer timer.Stop()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-p.readyCh:
			RTTDiag("rtt connected pid=%d after=%dms", cmd.Process.Pid, time.Since(p.started).Milliseconds())
			return p, nil
		case <-p.failCh:
			return nil, p.fail("connect error")
		case <-p.exited:
			return nil, p.fail("logger exited")
		case <-timer.C:
			p.pushStatus("J-Link RTT Logger 已启动，但 " + strconv.Itoa(int(rttConnectTimeout/time.Second)) + " 秒内未确认 RTT 连接，继续等待数据…")
			RTTDiag("rtt connect not confirmed after %s; keep waiting", rttConnectTimeout)
			return p, nil
		case <-poll.C:
			if info, e := os.Stat(out); e == nil && info.Size() > 0 {
				RTTDiag("rtt connected (data seen) after=%dms", time.Since(p.started).Milliseconds())
				return p, nil
			}
		}
	}
}

// fail stops the logger and returns a Chinese explanation plus J-Link's text.
func (p *rttPort) fail(why string) error {
	p.stop()
	p.mu.Lock()
	lines := append([]string{}, p.tail...)
	p.mu.Unlock()
	code := -1
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	}
	msg := explainJLink(lines, p.device, code)
	RTTDiag("rtt connect failed (%s) exit=%d: %s", why, code, msg)
	_ = os.Remove(p.out)
	return errors.New(msg)
}

func (p *rttPort) readOutput(r *os.File) {
	defer close(p.outDone)
	defer r.Close()
	buf := make([]byte, 4096)
	for {
		n, e := r.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.lastOut = time.Now()
			text := p.partial + string(buf[:n])
			for {
				i := strings.IndexAny(text, "\r\n")
				if i < 0 {
					break
				}
				p.lineLocked(text[:i])
				text = text[i+1:]
			}
			p.partial = text
			p.mu.Unlock()
		}
		if e != nil {
			p.mu.Lock()
			if p.partial != "" {
				p.lineLocked(p.partial)
				p.partial = ""
			}
			p.mu.Unlock()
			return
		}
	}
}

func (p *rttPort) lineLocked(line string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.Trim(line, "-=") == "" {
		return
	}
	low := strings.ToLower(line)
	if strings.HasPrefix(low, "transfer rate") {
		p.rate = line
		p.signalReadyLocked()
		return
	}
	p.tail = append(p.tail, line)
	if len(p.tail) > 12 {
		p.tail = p.tail[1:]
	}
	RTTDiag("rtt logger: %s", line)
	if p.emitted < 200 {
		p.emitted++
		p.status = append(p.status, "J-Link："+line)
	}
	if strings.Contains(low, "getting rtt data") {
		p.signalReadyLocked()
	}
	if !p.ready && !p.failed && looksLikeJLinkError(low) {
		p.failed = true
		close(p.failCh)
	}
}

func (p *rttPort) signalReadyLocked() {
	if !p.ready && !p.failed {
		p.ready = true
		close(p.readyCh)
	}
}

func looksLikeJLinkError(low string) bool {
	return strings.Contains(low, "error") || strings.Contains(low, "could not") || strings.Contains(low, "failed") || strings.Contains(low, "unknown device") || strings.Contains(low, "cannot")
}

// answerPrompts accepts the default if the logger unexpectedly stops at an
// interactive question (a partial line with no newline that stays idle).
func (p *rttPort) answerPrompts() {
	t := time.NewTicker(p.idle / 4)
	defer t.Stop()
	for {
		select {
		case <-p.exited:
			return
		case <-p.closed:
			return
		case <-t.C:
		}
		p.mu.Lock()
		prompt := strings.TrimSpace(p.partial)
		ask := !p.ready && prompt != "" && p.prompts < 6 && time.Since(p.lastOut) > p.idle
		if ask {
			p.prompts++
			p.partial = ""
			p.lastOut = time.Now()
			p.status = append(p.status, "J-Link RTT Logger 询问参数，已使用默认值："+prompt)
		}
		p.mu.Unlock()
		if ask {
			RTTDiag("rtt logger prompt answered with default: %s", prompt)
			_, _ = io.WriteString(p.stdin, "\r\n")
		}
	}
}

func (p *rttPort) pushStatus(s string) {
	p.mu.Lock()
	p.status = append(p.status, s)
	p.mu.Unlock()
}

// TakeStatus returns logger messages that should appear in the log as SYS lines.
func (p *rttPort) TakeStatus() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	p.status = nil
	return s
}

var errRTTClosed = errors.New("J-Link RTT 会话已关闭。")

func (p *rttPort) readFile(b []byte) (int, error) {
	p.fileMu.Lock()
	defer p.fileMu.Unlock()
	select {
	case <-p.closed:
		return 0, errRTTClosed
	default:
	}
	if p.file == nil {
		f, e := os.Open(p.out)
		if e != nil {
			return 0, nil
		}
		p.file = f
	}
	n, e := p.file.Read(b)
	if e == io.EOF {
		e = nil
	}
	if n > 0 {
		p.mu.Lock()
		p.bytes += uint64(n)
		first := !p.firstData
		p.firstData = true
		p.mu.Unlock()
		if first {
			RTTDiag("rtt first data after=%dms", time.Since(p.started).Milliseconds())
		}
	}
	return n, e
}

func (p *rttPort) Read(b []byte) (int, error) {
	deadline := time.Now().Add(100 * time.Millisecond)
	for {
		n, e := p.readFile(b)
		if n > 0 || e != nil {
			return n, e
		}
		select {
		case <-p.closed:
			return 0, errRTTClosed
		case <-p.exited:
			// Drain whatever the logger wrote before it exited.
			if n, e := p.readFile(b); n > 0 || e != nil {
				return n, e
			}
			return 0, p.exitError()
		default:
		}
		if time.Now().After(deadline) {
			return 0, nil
		}
		select {
		case <-p.closed:
		case <-p.exited:
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (p *rttPort) exitError() error {
	p.mu.Lock()
	lines := append([]string{}, p.tail...)
	bytes := p.bytes
	p.mu.Unlock()
	code := -1
	if p.cmd.ProcessState != nil {
		code = p.cmd.ProcessState.ExitCode()
	}
	RTTDiag("rtt logger exited code=%d bytes=%d", code, bytes)
	return errors.New(explainJLink(lines, p.device, code))
}

func (p *rttPort) Write([]byte) (int, error) {
	return 0, errors.New("J-Link RTT 会话只读：通过 JLinkRTTLogger 只接收 RTT 日志，不支持向目标发送。")
}
func (p *rttPort) Drain() error { return nil }

// stop asks the logger to quit (it waits for a key press), then kills it.
func (p *rttPort) stop() {
	_, _ = io.WriteString(p.stdin, "\r\n")
	select {
	case <-p.exited:
	case <-time.After(1500 * time.Millisecond):
		_ = p.cmd.Process.Kill()
		select {
		case <-p.exited:
		case <-time.After(3 * time.Second):
		}
	}
	_ = p.stdin.Close()
}

func (p *rttPort) Close() error {
	p.closeOnce.Do(func() {
		close(p.closed)
		p.stop()
		p.fileMu.Lock()
		if p.file != nil {
			p.file.Close()
			p.file = nil
		}
		p.fileMu.Unlock()
		_ = os.Remove(p.out)
		p.mu.Lock()
		bytes, rate := p.bytes, p.rate
		p.mu.Unlock()
		RTTDiag("rtt closed bytes=%d last=%q", bytes, rate)
	})
	return nil
}

// explainJLink turns JLinkRTTLogger's English output into a Chinese hint and
// keeps J-Link's own last lines so a rejected device name is visible as-is.
func explainJLink(lines []string, device string, code int) string {
	// Judge by the error lines when there are any: a successful
	// "Connecting to target...OK" must not mask a later RTT error.
	judged := []string{}
	for _, l := range lines {
		if looksLikeJLinkError(strings.ToLower(l)) {
			judged = append(judged, l)
		}
	}
	if len(judged) == 0 {
		judged = lines
	}
	low := strings.ToLower(strings.Join(judged, "\n"))
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(low, w) {
				return true
			}
		}
		return false
	}
	var hint string
	switch {
	case has("device>"):
		hint = "J-Link 不认识目标芯片名称“" + device + "”，正在询问设备名。请在“目标芯片”中填写 J-Link 设备库中的名称（nRF54L15 的应用核为 " + DefaultRTTDevice + "）。"
	case has("tif>", "speed>"):
		hint = "J-Link 不接受当前的接口或速度设置，请检查“接口”和“速度”。"
	case has("could not connect to j-link", "no j-link", "j-link via usb...failed", "j-link via usb...error", "failed to open dll", "could not open j-link", "emulator not found"):
		hint = "无法连接 J-Link 调试器。请检查 J-Link 是否已通过 USB 连接，并且没有被其他程序（J-Link Commander、Ozone、IDE 调试会话等）占用。"
	case has("unknown device", "device not found", "unsupported device", "invalid device", "device name", "could not find device", "device is unknown", "not a valid device"):
		hint = "J-Link 不认识目标芯片名称“" + device + "”。请在“目标芯片”中填写 J-Link 设备库中的名称（nRF54L15 的应用核为 " + DefaultRTTDevice + "）。"
	case has("target"):
		hint = "J-Link 无法连接目标芯片。请检查目标板供电、SWD/JTAG 接线、接口与速度设置；芯片若开启了访问保护，需要先用 nRF 工具解除。"
	case has("rtt", "control block"):
		hint = "没有找到 RTT 控制块或通道。请确认固件已启用 RTT 并正在运行，通道号正确；也可以手动填写 RTT 控制块地址或搜索范围。"
	default:
		hint = "J-Link RTT Logger 已退出"
		if code >= 0 {
			hint += "（退出码 " + strconv.Itoa(code) + "）"
		}
		hint += "。"
	}
	if len(lines) > 0 {
		from := len(lines) - 4
		if from < 0 {
			from = 0
		}
		hint += " J-Link 输出：" + strings.Join(lines[from:], " / ")
	}
	return hint
}
