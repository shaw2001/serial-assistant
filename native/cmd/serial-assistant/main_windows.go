//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jchv/go-webview2/webviewloader"
	"go.bug.st/serial/enumerator"
	"golang.org/x/sys/windows"
	"io.github.shaw2001/serialassistant/internal/core"
	webview "io.github.shaw2001/serialassistant/internal/webview"
	"io.github.shaw2001/serialassistant/internal/webview/pkg/edge"
	"io.github.shaw2001/serialassistant/web"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// version is injected at build time: -ldflags "-X main.version=1.2.3"
var version = "0.4.0"

const releaseRepo = "shaw2001/serial-assistant"

var user32 = windows.NewLazySystemDLL("user32.dll")
var messageBox = user32.NewProc("MessageBoxW")
var setWindowPos = user32.NewProc("SetWindowPos")
var setWindowLong = user32.NewProc("SetWindowLongPtrW")
var getWindowLong = user32.NewProc("GetWindowLongPtrW")
var callWindowProc = user32.NewProc("CallWindowProcW")
var postMessage = user32.NewProc("PostMessageW")
var showWindow = user32.NewProc("ShowWindow")
var isZoomed = user32.NewProc("IsZoomed")
var isIconic = user32.NewProc("IsIconic")
var setForegroundWindow = user32.NewProc("SetForegroundWindow")
var updateWindow = user32.NewProc("UpdateWindow")
var redrawWindow = user32.NewProc("RedrawWindow")
var setTimer = user32.NewProc("SetTimer")
var killTimer = user32.NewProc("KillTimer")
var getSystemMetrics = user32.NewProc("GetSystemMetrics")
var getWindowRect = user32.NewProc("GetWindowRect")
var releaseCapture = user32.NewProc("ReleaseCapture")
var getClientRect = user32.NewProc("GetClientRect")
var monitorFromRect = user32.NewProc("MonitorFromRect")
var getDpiForWindow = user32.NewProc("GetDpiForWindow")
var getSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
var getMonitorInfo = user32.NewProc("GetMonitorInfoW")
var invalidateRect = user32.NewProc("InvalidateRect")
var getCursorPos = user32.NewProc("GetCursorPos")
var beginPaint = user32.NewProc("BeginPaint")
var endPaint = user32.NewProc("EndPaint")
var fillRect = user32.NewProc("FillRect")
var setClassLongPtr = user32.NewProc("SetClassLongPtrW")
var getStockObject = gdi32.NewProc("GetStockObject")
var createSolidBrush = gdi32.NewProc("CreateSolidBrush")

// hostBrush is created once and lives for the whole process.
var hostBrush uintptr
var shell32 = windows.NewLazySystemDLL("shell32.dll")
var shellExecute = shell32.NewProc("ShellExecuteW")
var shAppBarMessage = shell32.NewProc("SHAppBarMessage")
var dwmapi = windows.NewLazySystemDLL("dwmapi.dll")
var dwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
var dwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
var gdi32 = windows.NewLazySystemDLL("gdi32.dll")

// DWM attributes used to keep the frame opaque, light and rounded.
const (
	dwmwaUseImmersiveDarkMode   = 20
	dwmwaWindowCornerPreference = 33
	dwmwaCaptionColor           = 35
	dwmwaSystemBackdropType     = 38
	dwmCornerRound              = 2            // DWMWCP_ROUND
	gwlStyle                    = ^uintptr(15) // -16
	wsCaption                   = 0x00C00000
	wsThickFrame                = 0x00040000
	wsSysMenu                   = 0x00080000
	wsMinimizeBox               = 0x00020000
	wsMaximizeBox               = 0x00010000
	wsClipChildren              = 0x02000000  // host paint never covers the WebView2 child
	gwlWndProc                  = ^uintptr(3) // GWLP_WNDPROC
	wmNcHitTest                 = 0x0084
	wmNcLButtonDown             = 0x00A1
)

var lastSizeType atomic.Int32
var sizeLogs atomic.Int32
var refreshLogs atomic.Int32
var refreshPending atomic.Bool

type dwmMargins struct{ Left, Right, Top, Bottom int32 }

func hrFailed(hr uintptr) bool { return int32(hr) < 0 }

func setDwmAttribute(hwnd uintptr, attr uint32, value int32) uintptr {
	hr, _, _ := dwmSetWindowAttribute.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	return hr
}

// applyOpaqueFrame turns the borderless host into a plain opaque window:
// Win11 rounded corners and the DWM shadow stay, every glass effect is off
// (no system backdrop, no blur-behind, no full-sheet frame extension). Every
// DWM HRESULT is written to the diagnostics log.
func applyOpaqueFrame(hwnd uintptr, diag *diagLog) {
	var buildNumber uint32
	if v := windows.RtlGetVersion(); v != nil {
		buildNumber = v.BuildNumber
		diag.Printf("windows version=%d.%d build=%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	}
	corner := setDwmAttribute(hwnd, dwmwaWindowCornerPreference, dwmCornerRound)
	dark := setDwmAttribute(hwnd, dwmwaUseImmersiveDarkMode, 0)
	caption := setDwmAttribute(hwnd, dwmwaCaptionColor, int32(hostBrushColor))
	backdrop := uintptr(0)
	if buildNumber >= 22621 {
		backdrop = setDwmAttribute(hwnd, dwmwaSystemBackdropType, 1) // DWMSBT_NONE
	}
	m := opaqueFrameMargins()
	margins := dwmMargins{m[0], m[1], m[2], m[3]}
	extend, _, _ := dwmExtendFrameIntoClientArea.Call(hwnd, uintptr(unsafe.Pointer(&margins)))
	_, _, _ = setWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0027) // SWP_NOSIZE|SWP_NOMOVE|SWP_NOZORDER|SWP_FRAMECHANGED
	diag.Printf("opaque frame: corner=%#x darkmode=%#x caption=%#x backdropNone=%#x extendFrame%v=%#x", uint32(corner), uint32(dark), uint32(caption), uint32(backdrop), m, uint32(extend))
}

// paintHostWhite fills the host client with the opaque backdrop colour
// (hostBrushColor, #eef2fb), so only the page's own background colour can ever
// show around or before WebView2.
func paintHostWhite(hwnd, hdc uintptr) {
	var rect struct{ Left, Top, Right, Bottom int32 }
	getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if hostBrush == 0 {
		hostBrush, _, _ = createSolidBrush.Call(uintptr(hostBrushColor))
	}
	brush := hostBrush
	if brush == 0 {
		brush, _, _ = getStockObject.Call(0) // WHITE_BRUSH fallback
	}
	fillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), brush)
}

func chromeHit(cmd string) uintptr {
	switch cmd {
	case "drag":
		return 2 // HTCAPTION
	case "resize-left":
		return 10
	case "resize-right":
		return 11
	case "resize-top":
		return 12
	case "resize-topleft":
		return 13
	case "resize-topright":
		return 14
	case "resize-bottom":
		return 15
	case "resize-bottomleft":
		return 16
	case "resize-bottomright":
		return 17
	default:
		return 0
	}
}

// beginChromeMove hands a page drag/resize gesture to the system move/size
// loop, the way tao/winit do: release capture, then post WM_NCLBUTTONDOWN with
// the hit-test code and the cursor position. Posting (not sending) keeps the
// modal loop out of the WebView2 message callback.
func beginChromeMove(hwnd, hit uintptr) {
	if hwnd == 0 || hit == 0 {
		return
	}
	if hit != 2 {
		if zoomed, _, _ := isZoomed.Call(hwnd); zoomed != 0 {
			return
		}
	}
	var pt struct{ X, Y int32 }
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	lp := uintptr(uint32(uint16(int16(pt.X))) | uint32(uint16(int16(pt.Y)))<<16)
	releaseCapture.Call()
	postMessage.Call(hwnd, wmNcLButtonDown, hit, lp)
}

func windowCommand(hwnd uintptr, cmd string) {
	switch cmd {
	case "minimize":
		showWindow.Call(hwnd, 6)
	case "maximize":
		if zoomed, _, _ := isZoomed.Call(hwnd); zoomed != 0 {
			showWindow.Call(hwnd, 9)
		} else {
			showWindow.Call(hwnd, 3)
		}
	case "close":
		postMessage.Call(hwnd, 0x10, 0, 0)
	}
}

func wide(s string) *uint16 { p, _ := windows.UTF16PtrFromString(s); return p }
func alert(s string) {
	messageBox.Call(0, uintptr(unsafe.Pointer(wide(s))), uintptr(unsafe.Pointer(wide("串口助手"))), 0x10)
}

type request struct {
	Token  string            `json:"token"`
	ID     int               `json:"id"`
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

func arg[T any](r request, i int) (T, error) {
	var v T
	if i >= len(r.Args) {
		return v, errors.New("缺少调用参数。")
	}
	e := json.Unmarshal(r.Args[i], &v)
	return v, e
}
func main() {
	runtime.LockOSThread()
	started := time.Now()
	smoke := len(os.Args) > 1 && os.Args[1] == "--smoke-test"
	report := ""
	if smoke && len(os.Args) > 2 {
		report = os.Args[2]
	}
	if smoke && report != "" {
		f, e := os.Create(report + ".trace")
		if e == nil {
			defer f.Close()
			log.SetOutput(f)
			var traceCount atomic.Uint32
			edge.SecurityTrace = func(format string, args ...any) {
				if traceCount.Add(1) <= 200 {
					log.Printf(format, args...)
				}
			}
			log.Print("smoke starting")
		}
	}
	configBase, e := os.UserConfigDir()
	if e != nil {
		alert(e.Error())
		return
	}
	dir := filepath.Join(configBase, "SerialAssistant")
	if smoke {
		dir, _ = os.MkdirTemp("", "serial-assistant-smoke-")
		defer os.RemoveAll(dir)
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		alert(e.Error())
		return
	}
	diag := openDiagLog(filepath.Join(dir, "serial-assistant-diag.log"))
	if smoke {
		diag = nil
	}
	core.AppVersion = version
	diag.Printf("---- start v%s %s/%s", version, runtime.GOOS, runtime.GOARCH)
	// A portable package ships a WebView2 Fixed Version runtime next to the EXE.
	// Use it when present; otherwise fall back to the system Evergreen runtime.
	var browserFolder, runtimeVersion string
	var detectError error
	if exePath, err := os.Executable(); err == nil {
		browserFolder, runtimeVersion = findBundledRuntime(filepath.Dir(exePath))
	}
	if browserFolder != "" {
		diag.Printf("webview2 bundled fixed runtime=%q version=%s", browserFolder, runtimeVersion)
		grantFixedRuntimeAccess(browserFolder, diag)
	} else {
		// The shared WebView2 runtime is detected before constructing the native window.
		runtimeVersion, detectError = webviewloader.GetInstalledVersion()
		diag.Printf("webview2 evergreen runtime version=%s err=%v", runtimeVersion, detectError)
	}
	if browserFolder == "" && (detectError != nil || runtimeVersion == "") {
		alert("需要 Microsoft WebView2 Runtime。Windows 11 和多数 Windows 10 已自带。\n请从 https://developer.microsoft.com/microsoft-edge/webview2 下载 Evergreen Runtime 后，再运行本 EXE。")
		if smoke {
			os.Exit(1)
		}
		return
	}
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		alert("无法初始化安全会话。")
		return
	}
	bridgeToken := hex.EncodeToString(key)
	// Opaque #eef2fb WebView2 background (AARRGGBB); the controller sets the
	// same colour through DefaultBackgroundColor.
	_ = os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", webviewBackgroundEnv)
	w := webview.NewWithOptions(webview.WebViewOptions{DataPath: filepath.Join(dir, "WebView2"), BrowserExecutableFolder: browserFolder, AutoFocus: true, WindowOptions: webview.WindowOptions{Hidden: true, Title: "串口助手 · v" + version, Width: 1440, Height: 900}})
	if w == nil {
		alert("窗口初始化失败。")
		if smoke {
			os.Exit(1)
		}
		return
	}
	defer w.Destroy()
	w.SetSize(1024, 640, webview.HintMin)
	core.RTTDiag = diag.Printf
	service := core.NewService(filepath.Join(dir, "logs"), core.OpenAny)
	defer service.Shutdown()
	var quitting, closing atomic.Bool
	var smokeDone atomic.Bool
	var original uintptr
	// refreshHost re-applies the WebView2 bounds to the full client rectangle,
	// makes the controller visible and forces the host and its WebView2 child
	// windows to repaint. It is a no-op while the window is minimized.
	surface := &webviewSurface{w: w}
	refreshHost := func(hwnd uintptr, reason string) {
		if iconic, _, _ := isIconic.Call(hwnd); iconic != 0 {
			return
		}
		var client rect32
		getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		surface.hwnd = hwnd
		applied := refreshHostSurface(surface, client)
		if refreshLogs.Add(1) <= 60 {
			diag.Printf("host refresh reason=%s visible=true bounds=%t client=(%d,%d,%d,%d)", reason, applied, client.Left, client.Top, client.Right, client.Bottom)
		}
	}
	// scheduleRefresh refreshes again after the current restore/show/activate
	// burst: once from the queue (posted message) and once after a short timer,
	// when the Win11 restore animation has ended and the client rect is final.
	scheduleRefresh := func(hwnd uintptr) {
		if refreshPending.CompareAndSwap(false, true) {
			postMessage.Call(hwnd, wmAppRefreshHost, 0, 0)
		}
		setTimer.Call(hwnd, refreshHostTimerID, refreshHostDelayMs, 0)
	}
	callback := windows.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
		if msg == wmAppRefreshHost {
			refreshPending.Store(false)
			refreshHost(hwnd, "posted")
			return 0
		}
		if msg == 0x0113 && wp == refreshHostTimerID { // WM_TIMER
			killTimer.Call(hwnd, refreshHostTimerID)
			refreshHost(hwnd, "timer")
			return 0
		}
		if msg == wmSize { // WM_SIZE: handled here instead of the library's plain Resize
			kind := map[uintptr]string{0: "restored", 1: "minimized", 2: "maximized"}[wp]
			// SIZE_MINIMIZED is deliberately a no-op: WebView2 is neither hidden
			// nor shrunk to the iconic client, so nothing has to be undone later.
			switch hostActionFor(uint32(msg), wp, 0) {
			case hostRefreshNow:
				refreshHost(hwnd, "wm_size-"+kind)
				scheduleRefresh(hwnd)
			}
			if prev := lastSizeType.Swap(int32(wp)); prev != int32(wp) || sizeLogs.Add(1) <= 8 {
				diag.Printf("wm_size type=%s(%d) lparam=%dx%d", kind, wp, lp&0xffff, (lp>>16)&0xffff)
			}
			return 0
		}
		if msg == wmShowWindow || msg == wmActivate || msg == wmSysCommand || msg == wmWindowPosChanged || msg == wmDisplayChange || msg == wmDpiChanged {
			var flags uint32
			if msg == wmWindowPosChanged && lp != 0 {
				flags = *(*uint32)(unsafe.Pointer(lp + windowPosFlagOffset))
			}
			if hostActionFor(uint32(msg), wp, flags) == hostRefreshLater {
				result, _, _ := callWindowProc.Call(original, hwnd, msg, wp, lp)
				scheduleRefresh(hwnd)
				return result
			}
		}
		if msg == 0x0083 && wp != 0 { // WM_NCCALCSIZE: client area fills the borderless window
			if zoomed, _, _ := isZoomed.Call(hwnd); zoomed != 0 {
				// A maximized window overhangs the monitor by its resize frame.
				// Clamp the client to the monitor work area so nothing is cut
				// off and the taskbar stays visible.
				r := (*rect32)(unsafe.Pointer(lp))
				before := *r
				work, ok := monitorWorkArea(before)
				fx, fy := resizeFrame(hwnd)
				client, source := maximizedClientRect(before, work, ok, fx, fy)
				*r = client
				if lastSizeType.Load() != 2 || source != "work-area" {
					diag.Printf("nccalcsize maximized window=%v work=%v ok=%t frame=%d,%d client=%v source=%s", before, work, ok, fx, fy, client, source)
				}
			}
			return 0
		}
		if msg == 0x0086 { // WM_NCACTIVATE: there is no non-client frame to repaint
			r, _, _ := callWindowProc.Call(original, hwnd, msg, wp, ^uintptr(0))
			return r
		}
		if msg == wmNcHitTest { // WM_NCHITTEST: keep the borderless window resizable
			if zoomed, _, _ := isZoomed.Call(hwnd); zoomed == 0 {
				var rect struct{ Left, Top, Right, Bottom int32 }
				getWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
				x := int32(int16(lp & 0xffff))
				y := int32(int16((lp >> 16) & 0xffff))
				const border int32 = 8
				onLeft := x < rect.Left+border
				onRight := x >= rect.Right-border
				onTop := y < rect.Top+border
				onBottom := y >= rect.Bottom-border
				switch {
				case onTop && onLeft:
					return 13 // HTTOPLEFT
				case onTop && onRight:
					return 14 // HTTOPRIGHT
				case onBottom && onLeft:
					return 16 // HTBOTTOMLEFT
				case onBottom && onRight:
					return 17 // HTBOTTOMRIGHT
				case onLeft:
					return 10 // HTLEFT
				case onRight:
					return 11 // HTRIGHT
				case onTop:
					return 12 // HTTOP
				case onBottom:
					return 15 // HTBOTTOM
				}
			}
		}
		if msg == 0x0014 { // WM_ERASEBKGND: solid white host
			paintHostWhite(hwnd, wp)
			return 1
		}
		if msg == 0x000F { // WM_PAINT: never leave the host surface unpainted (white)
			var ps [72]byte // PAINTSTRUCT on amd64
			hdc, _, _ := beginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps[0])))
			if hdc != 0 {
				paintHostWhite(hwnd, hdc)
			}
			endPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps[0])))
			return 0
		}
		if msg == 0x10 && !quitting.Load() {
			if closing.CompareAndSwap(false, true) {
				w.Eval("window.__nativeClosing && window.__nativeClosing()")
				go func() {
					time.Sleep(2 * time.Second)
					if !quitting.Load() {
						quitting.Store(true)
						postMessage.Call(hwnd, 0x10, 0, 0)
					}
				}()
			}
			return 0
		}
		result, _, _ := callWindowProc.Call(original, hwnd, msg, wp, lp)
		return result
	})
	original, _, _ = setWindowLong.Call(uintptr(w.Window()), gwlWndProc, callback)
	hostHwnd := uintptr(w.Window())
	// Drop WS_CAPTION so DWM never draws native title text or caption buttons
	// (the doubled min/max/close); the page draws its own. WS_THICKFRAME, the
	// system menu and min/max boxes stay for resize, snap and the taskbar.
	style, _, _ := getWindowLong.Call(hostHwnd, gwlStyle)
	newStyle := (style &^ wsCaption) | wsThickFrame | wsSysMenu | wsMinimizeBox | wsMaximizeBox | wsClipChildren
	_, _, _ = setWindowLong.Call(hostHwnd, gwlStyle, newStyle)
	diag.Printf("window style %#x -> %#x", style, newStyle)
	_, _, _ = setClassLongPtr.Call(hostHwnd, ^uintptr(9), func() uintptr { b, _, _ := getStockObject.Call(0); return b }()) // GCLP_HBRBACKGROUND = WHITE_BRUSH
	bgHR, bgARGB := w.BackgroundState()
	diag.Printf("webview2 runtime=%s defaultBackground put hr=%#x readback=%#08x env=%q", runtimeVersion, bgHR, bgARGB, os.Getenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR"))
	if int32(bgHR) < 0 || !backgroundIsOpaque(bgARGB) {
		diag.Printf("warning: webview2 default background is not opaque white")
	}
	applyOpaqueFrame(hostHwnd, diag)
	// The window was created hidden. It is shown once, when the page reports
	// ready (laid out, fonts loaded) or after startupShowTimeout, whichever is
	// first, so startup never flashes an empty white window.
	var firstShow firstShowGate
	showFirst := func(reason string) {
		if !firstShow.claim(reason) {
			return
		}
		w.Dispatch(func() {
			hwnd := uintptr(w.Window())
			showWindow.Call(hwnd, swShowDefault)
			setForegroundWindow.Call(hwnd)
			updateWindow.Call(hwnd)
			refreshHost(hwnd, "first-show")
			scheduleRefresh(hwnd)
			diag.Printf("window first shown reason=%s after=%dms", reason, time.Since(started).Milliseconds())
		})
	}
	_, _, _ = invalidateRect.Call(hostHwnd, 0, 1)
	invoke := func(r request) (any, error) {
		id, _ := arg[string](r, 0)
		switch r.Method {
		case "diag":
			text, e := arg[string](r, 0)
			if e != nil {
				return nil, e
			}
			diag.Printf("page: %s", text)
			return nil, nil
		case "licenses":
			bytes, e := web.Assets.ReadFile("THIRD_PARTY_NOTICES.txt")
			return string(bytes), e
		case "initialize":
			config, warning := core.LoadConfig(filepath.Join(dir, "config.json"))
			return map[string]any{"config": config, "warning": warning, "version": version, "platform": "win32", "logsDirectory": filepath.Join(dir, "logs"), "rttDefaults": core.DefaultRTTOptions()}, nil
		case "listPorts":
			ports, e := enumerator.GetDetailedPortsList()
			if e != nil {
				return nil, e
			}
			out := []any{}
			for _, p := range ports {
				friendly := p.Product
				if friendly == "" && p.IsUSB {
					friendly = "USB 串口 · VID " + p.VID + " / PID " + p.PID
				}
				out = append(out, map[string]any{"path": p.Name, "friendlyName": friendly, "manufacturer": p.Manufacturer})
			}
			return out, nil
		case "open":
			o, e := arg[core.Options](r, 1)
			if e != nil {
				return nil, e
			}
			return service.Open(id, o)
		case "checkJLink":
			tools, e := core.LocateJLink(id)
			diag.Printf("jlink check dir=%q ok=%v", id, e == nil)
			return tools, e
		case "pickJLink":
			file, e := fileDialog(w, false, "选择 J-Link 安装目录中的 JLinkRTTLogger.exe", "JLinkRTTLogger.exe", "exe")
			if e != nil || file == "" {
				return map[string]any{"canceled": true}, e
			}
			return map[string]any{"canceled": false, "dir": filepath.Dir(file)}, nil
		case "close":
			return service.Close(id)
		case "remove":
			return nil, service.Remove(id)
		case "send":
			p, e := arg[core.Payload](r, 1)
			if e != nil {
				return nil, e
			}
			return service.Send(id, p)
		case "preview":
			p, e := arg[core.Payload](r, 0)
			if e != nil {
				return nil, e
			}
			b, e := p.Bytes()
			if e != nil {
				return nil, e
			}
			out := make([]int, len(b))
			for i, v := range b {
				out[i] = int(v)
			}
			return map[string]any{"bytes": out}, nil
		case "poll":
			return service.Batch(), nil
		case "startPeriodic":
			p, e := arg[core.Payload](r, 1)
			if e != nil {
				return nil, e
			}
			interval, e := arg[int](r, 2)
			if e != nil {
				return nil, e
			}
			return service.StartPeriodic(id, p, interval)
		case "stopPeriodic":
			return nil, service.StopPeriodic(id)
		case "setCapture":
			on, e := arg[bool](r, 1)
			if e != nil {
				return nil, e
			}
			return service.Capture(id, on)
		case "clear":
			return service.Clear(id)
		case "saveConfig":
			if len(r.Args) == 0 {
				return nil, errors.New("缺少配置。")
			}
			return nil, core.SaveConfig(filepath.Join(dir, "config.json"), r.Args[0])
		case "importConfig":
			file, e := fileDialog(w, false, "导入配置方案", "serial-assistant-profile.json", "json")
			if e != nil || file == "" {
				return nil, e
			}
			f, e := os.Open(file)
			if e != nil {
				return nil, e
			}
			defer f.Close()
			info, e := f.Stat()
			if e != nil {
				return nil, e
			}
			if info.Size() > 2*1024*1024 {
				return nil, errors.New("配置文件不能超过 2 MiB。")
			}
			data, e := os.ReadFile(file)
			if e != nil {
				return nil, e
			}
			return core.DecodeConfig(data)
		case "exportConfig":
			if len(r.Args) == 0 {
				return nil, errors.New("缺少配置。")
			}
			if e := core.ValidateConfig(r.Args[0]); e != nil {
				return nil, e
			}
			file, e := fileDialog(w, true, "导出配置方案", "serial-assistant-profile.json", "json")
			if e != nil || file == "" {
				return map[string]any{"canceled": true}, e
			}
			safe, err := core.ConfigBytes(r.Args[0], true)
			if err != nil {
				return nil, err
			}
			e = core.AtomicWrite(file, safe)
			return map[string]any{"canceled": false, "path": file}, e
		case "exportLog":
			o, e := arg[core.ExportOptions](r, 0)
			if e != nil {
				return nil, e
			}
			if ok, e := service.HasExportable(o.ID, o.Scope, o.IDs); e != nil {
				return nil, e
			} else if !ok {
				return nil, core.ErrNothingToExport
			}
			file, e := fileDialog(w, true, "导出串口日志", "serial-log-"+time.Now().Format("20060102-150405")+"."+o.Format, o.Format)
			if e != nil || file == "" {
				return map[string]any{"canceled": true}, e
			}
			count, e := service.Export(file, o)
			diag.Printf("export log scope=%s format=%s records=%d err=%v", o.Scope, o.Format, count, e)
			if e != nil {
				return nil, e
			}
			return map[string]any{"canceled": false, "path": file, "count": count}, nil
		case "showLogs":
			path := filepath.Join(dir, "logs")
			if e = os.MkdirAll(path, 0700); e != nil {
				return nil, e
			}
			result, _, _ := shellExecute.Call(uintptr(w.Window()), uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(path))), 0, 0, 1)
			if result <= 32 {
				return nil, errors.New("无法打开日志目录。")
			}
			return nil, nil
		case "checkUpdate":
			force, _ := arg[bool](r, 0)
			if !force {
				data, err := os.ReadFile(filepath.Join(dir, "config.json"))
				if err != nil {
					return map[string]any{"skipped": true}, nil
				}
				cfg, err := core.DecodeConfig(data)
				if err != nil || !cfg.UI.AutoUpdate {
					return map[string]any{"skipped": true}, nil
				}
			}
			stateFile := filepath.Join(dir, "update-check.json")
			state := core.LoadUpdateState(stateFile)
			if !force && !core.ShouldCheck(state, time.Now(), 24*time.Hour) {
				return map[string]any{"current": version, "checked": false, "skipped": true}, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			info, e := core.FetchRelease(ctx, nil, releaseRepo, version)
			if e != nil {
				return nil, e
			}
			_ = core.SaveUpdateState(stateFile, core.UpdateState{LastCheck: time.Now()})
			return info, nil
		case "openExternal":
			target, e := arg[string](r, 0)
			if e != nil {
				return nil, e
			}
			if !core.ValidReleaseURL(target) {
				return nil, errors.New("仅允许打开 GitHub 链接。")
			}
			result, _, _ := shellExecute.Call(uintptr(w.Window()), uintptr(unsafe.Pointer(wide("open"))), uintptr(unsafe.Pointer(wide(target))), 0, 0, 1)
			if result <= 32 {
				return nil, errors.New("无法打开浏览器。")
			}
			return nil, nil
		case "windowCommand":
			cmd, e := arg[string](r, 0)
			if e != nil {
				return nil, e
			}
			done := make(chan struct{})
			w.Dispatch(func() {
				windowCommand(uintptr(w.Window()), cmd)
				close(done)
			})
			<-done
			return nil, nil
		case "setAlwaysOnTop":
			on, e := arg[bool](r, 0)
			if e != nil {
				return nil, e
			}
			done := make(chan struct{})
			w.Dispatch(func() {
				after := ^uintptr(1)
				if on {
					after = ^uintptr(0)
				}
				setWindowPos.Call(uintptr(w.Window()), after, 0, 0, 0, 0, 0x13)
				close(done)
			})
			<-done
			return nil, nil
		case "quit":
			quitting.Store(true)
			w.Dispatch(func() { postMessage.Call(uintptr(w.Window()), 0x10, 0, 0) })
			return nil, nil
		case "ready":
			data, e := arg[map[string]any](r, 0)
			if e != nil {
				return nil, e
			}
			diag.Printf("page ready font=%v layout=%v screen=%v window=%v", data["fontReady"], data["layoutOK"], data["screenHeight"], data["windowHeight"])
			showFirst("page-ready")
			if smoke {
				log.Printf("ready probe=%v version=%v font=%v layout=%v blocked=%d unauthorized=%v", data["securityProbe"], data["version"], data["fontReady"], data["layoutOK"], w.BlockedNavigations(), data["unauthorizedRejected"])
				if data["securityProbe"] != true {
					initial, _ := json.Marshal(data)
					w.Dispatch(func() {
						w.Eval(`(async()=>{let rejected=false;try{await window.__enqueue({id:999999,method:'initialize',args:[],token:'invalid'})}catch{rejected=true}const meta=document.createElement('meta');meta.httpEquiv='refresh';meta.content='0;url=https://security-probe.invalid/';document.head.appendChild(meta);setTimeout(()=>window.serialAPI.ready({...` + string(initial) + `,securityProbe:true,unauthorizedRejected:rejected}),500)})()`)
					})
					return nil, nil
				}
				if w.BlockedNavigations() == 0 || data["unauthorizedRejected"] != true {
					return nil, errors.New("原生页面安全检查失败。")
				}
				data["blockedNavigations"] = w.BlockedNavigations()
				data["readyMs"] = time.Since(started).Milliseconds()
				data["runtime"] = "Go + WebView2"
				if data["version"] != version || data["fontReady"] != true || data["layoutOK"] != true {
					return nil, errors.New("桌面字体或布局检查失败。")
				}
				bytes, _ := json.MarshalIndent(data, "", "  ")
				if report != "" {
					if e = os.WriteFile(report, bytes, 0600); e != nil {
						return nil, e
					}
				}
				fmt.Println(string(bytes))
				smokeDone.Store(true)
				quitting.Store(true)
				w.Dispatch(func() { postMessage.Call(uintptr(w.Window()), 0x10, 0, 0) })
			}
			return nil, nil
		default:
			return nil, errors.New("未知操作。")
		}
	}
	requests := make(chan struct{}, 32)
	_ = w.Bind("__enqueue", func(r request) error {
		if smoke && r.Method != "poll" {
			log.Printf("request method=%s authenticated=%t", r.Method, r.Token == bridgeToken)
		}
		if r.Token != bridgeToken {
			return errors.New("页面调用未授权。")
		}
		// Drag and resize must enter the modal loop before this callback returns,
		// while the mouse button that started the gesture is still down.
		if r.Method == "windowCommand" && len(r.Args) > 0 && len(r.Args) <= 4 && r.ID >= 1 {
			var cmd string
			if json.Unmarshal(r.Args[0], &cmd) == nil {
				if hit := chromeHit(cmd); hit != 0 {
					beginChromeMove(uintptr(w.Window()), hit)
					w.Dispatch(func() {
						w.Eval(fmt.Sprintf("window.__nativeResolve(%d,%s)", r.ID, `{"ok":true,"value":null}`))
					})
					return nil
				}
			}
		}
		select {
		case requests <- struct{}{}:
		default:
			return errors.New("调用过于频繁。")
		}

		if len(r.Args) > 4 || r.ID < 1 {
			<-requests
			return errors.New("调用无效。")
		}
		go func() {
			defer func() { <-requests }()
			result, e := invoke(r)
			if smoke && e != nil {
				log.Printf("request error %s: %v", r.Method, e)
			}
			response := map[string]any{"ok": e == nil, "value": result}
			if e != nil {
				response["error"] = e.Error()
			}
			data, _ := json.Marshal(response)
			w.Dispatch(func() { w.Eval(fmt.Sprintf("window.__nativeResolve(%d,%s)", r.ID, data)) })
		}()
		return nil
	})
	htmlBytes, _ := web.Assets.ReadFile("index.html")
	css, _ := web.Assets.ReadFile("app.css")
	js, _ := web.Assets.ReadFile("app.js")
	font, _ := web.Assets.ReadFile("OPPOSans-Regular.woff2")
	bridge, _ := web.Assets.ReadFile("bridge.js")
	nonceData := make([]byte, 16)
	if _, e = rand.Read(nonceData); e != nil {
		alert("无法初始化安全页面。")
		return
	}
	nonce := hex.EncodeToString(nonceData)
	html, e := composeDocument(documentParts{HTML: htmlBytes, CSS: css, JS: js, Bridge: bridge, Font: font, Nonce: nonce, BridgeToken: bridgeToken})
	if e != nil {
		alert("界面资源无法加载：" + e.Error())
		if smoke {
			os.Exit(1)
		}
		return
	}
	if smoke {
		log.Print("setting HTML")
	}
	time.AfterFunc(startupShowTimeout, func() { showFirst("timeout-fallback") })
	if e = w.SetHtmlChecked(html); e != nil {
		alert("界面加载失败：" + e.Error())
		if smoke {
			os.Exit(1)
		}
		return
	}
	if smoke {
		go func() {
			time.Sleep(25 * time.Second)
			if !smokeDone.Load() {
				os.Exit(1)
			}
		}()
	}
	w.Run()
	if smoke && !smokeDone.Load() {
		os.Exit(1)
	}
}

// webviewSurface drives the real WebView2 controller for refreshHostSurface.
type webviewSurface struct {
	w    webview.WebView
	hwnd uintptr
}

func (s *webviewSurface) SetVisible(v bool) { s.w.SetHostVisible(v) }
func (s *webviewSurface) ResizeToClient()   { s.w.ResizeHost() }
func (s *webviewSurface) NotifyMoved()      { s.w.NotifyHostMoved() }

// Redraw repaints the host and every child HWND, including the WebView2
// controller's own windows (RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_UPDATENOW|RDW_FRAME).
func (s *webviewSurface) Redraw() { redrawWindow.Call(s.hwnd, 0, 0, 0x1|0x80|0x100|0x400) }

// grantFixedRuntimeAccess applies Microsoft's documented Windows 10 fix for a
// Fixed Version runtime outside Program Files: since v120 the renderer runs in
// an AppContainer and needs read/execute for ALL APPLICATION PACKAGES
// (S-1-15-2-1) and ALL RESTRICTED APPLICATION PACKAGES (S-1-15-2-2).
// Windows 11 is unaffected. It runs once per folder and is best effort.
func grantFixedRuntimeAccess(folder string, diag *diagLog) {
	v := windows.RtlGetVersion()
	if v == nil || v.BuildNumber >= 22000 {
		return
	}
	marker := filepath.Join(folder, ".serial-assistant-acl")
	if _, err := os.Stat(marker); err == nil {
		return
	}
	icacls := filepath.Join(os.Getenv("SystemRoot"), "System32", "icacls.exe")
	cmd := exec.Command(icacls, folder, "/grant", "*S-1-15-2-2:(OI)(CI)(RX)", "/grant", "*S-1-15-2-1:(OI)(CI)(RX)", "/Q")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	diag.Printf("icacls fixed runtime err=%v out=%s", err, strings.TrimSpace(string(out)))
	if err == nil {
		_ = os.WriteFile(marker, []byte("granted\r\n"), 0o644)
	}
}

// monitorWorkArea is the work area (screen minus taskbar) of the monitor the
// window is on. An auto-hide taskbar keeps a 1px strip so it can still appear.
// monitorWorkArea is the work area of the monitor nearest to r (screen
// coordinates). It deliberately takes the rectangle the window is about to
// occupy, not the window handle: while a window is restored from the taskbar
// it is still parked at the iconic position, and MonitorFromWindow then
// answers with the primary monitor.
func monitorWorkArea(r rect32) (rect32, bool) {
	mon, _, _ := monitorFromRect.Call(uintptr(unsafe.Pointer(&r)), 2) // MONITOR_DEFAULTTONEAREST
	if mon == 0 {
		return rect32{}, false
	}
	var info struct {
		Size          uint32
		Monitor, Work rect32
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	if ok, _, _ := getMonitorInfo.Call(mon, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return rect32{}, false
	}
	work := info.Work
	if work == info.Monitor && autoHideTaskbar() {
		work.Bottom--
	}
	return work, work.Right > work.Left && work.Bottom > work.Top
}

// resizeFrame is the per-side resize frame (SM_CXSIZEFRAME+SM_CXPADDEDBORDER)
// at the window's own DPI; a maximized window overhangs its monitor by it.
func resizeFrame(hwnd uintptr) (int32, int32) {
	metric := func(index uintptr) int32 {
		if getSystemMetricsForDpi.Find() == nil && getDpiForWindow.Find() == nil {
			if dpi, _, _ := getDpiForWindow.Call(hwnd); dpi != 0 {
				v, _, _ := getSystemMetricsForDpi.Call(index, dpi)
				return int32(v)
			}
		}
		v, _, _ := getSystemMetrics.Call(index)
		return int32(v)
	}
	return metric(32) + metric(92), metric(33) + metric(92) // SM_CXSIZEFRAME/SM_CYSIZEFRAME + SM_CXPADDEDBORDER
}

func autoHideTaskbar() bool {
	var data struct {
		Size   uint32
		Hwnd   uintptr
		CB     uint32
		Edge   uint32
		Rect   rect32
		LParam uintptr
	}
	data.Size = uint32(unsafe.Sizeof(data))
	state, _, _ := shAppBarMessage.Call(4, uintptr(unsafe.Pointer(&data))) // ABM_GETSTATE
	return state&1 != 0                                                    // ABS_AUTOHIDE
}
