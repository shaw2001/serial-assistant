//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// version is injected at build time: -ldflags "-X main.version=1.2.3"
var version = "0.2.0"

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
var getSystemMetrics = user32.NewProc("GetSystemMetrics")
var getWindowRect = user32.NewProc("GetWindowRect")
var releaseCapture = user32.NewProc("ReleaseCapture")
var sendMessageW = user32.NewProc("SendMessageW")
var getWindow = user32.NewProc("GetWindow")
var defWindowProc = user32.NewProc("DefWindowProcW")
var shellExecute = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")
var dwmapi = windows.NewLazySystemDLL("dwmapi.dll")
var dwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
var dwmExtendFrameIntoClientArea = dwmapi.NewProc("DwmExtendFrameIntoClientArea")

// DWMWA_USE_IMMERSIVE_DARK_MODE, DWMWA_WINDOW_CORNER_PREFERENCE, DWMWA_SYSTEMBACKDROP_TYPE.
const (
	dwmwaUseImmersiveDarkMode   = 20
	dwmwaWindowCornerPreference = 33
	dwmwaBorderColor            = 34
	dwmwaCaptionColor           = 35
	dwmwaSystemBackdropType     = 38
	dwmBackdropMica             = 2         // DWMSBT_MAINWINDOW
	dwmBackdropAcrylic          = 3         // DWMSBT_TRANSIENTWINDOW
	dwmCornerRound              = 2         // DWMWCP_ROUND
	dwmColorNone                = int32(-2) // DWMWA_COLOR_NONE
	wsExNoRedirectionBitmap     = 0x00200000
	gwlExStyle                  = ^uintptr(19) // -20
	gwlWndProc                  = ^uintptr(3)  // GWLP_WNDPROC
	wmNcHitTest                 = 0x0084
	wmParentNotify              = 0x0210
	wmNcLButtonDown             = 0x00A1
	htTransparent               = ^uintptr(0) // HTTRANSPARENT
)

var hostWindow uintptr
var childProc uintptr
var childProcs sync.Map

var glassOn atomic.Bool

type dwmMargins struct{ Left, Right, Top, Bottom int32 }

func hrFailed(hr uintptr) bool { return int32(hr) < 0 }

func setDwmAttribute(hwnd uintptr, attr uint32, value int32) uintptr {
	hr, _, _ := dwmSetWindowAttribute.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	return hr
}

// applyGlass makes the window a borderless Win11 Acrylic/Mica surface.
// WebView2 must keep its redirection surface: WS_EX_NOREDIRECTIONBITMAP hides
// the DWM backdrop behind an opaque client, which is why the glass looked solid.
// It returns false on Windows 10 or when the backdrop attribute is rejected;
// the page then paints the bundled wallpaper and keeps the same chrome.
func applyGlass(hwnd uintptr) bool {
	build := windows.RtlGetVersion()
	_ = setDwmAttribute(hwnd, dwmwaWindowCornerPreference, dwmCornerRound)
	_ = setDwmAttribute(hwnd, dwmwaUseImmersiveDarkMode, 1)
	// Drop the caption/border fill so rounded corners stay transparent.
	_ = setDwmAttribute(hwnd, dwmwaBorderColor, dwmColorNone)
	_ = setDwmAttribute(hwnd, dwmwaCaptionColor, dwmColorNone)
	margins := dwmMargins{-1, -1, -1, -1}
	extend, _, _ := dwmExtendFrameIntoClientArea.Call(hwnd, uintptr(unsafe.Pointer(&margins)))
	ex, _, _ := getWindowLong.Call(hwnd, gwlExStyle)
	if ex&wsExNoRedirectionBitmap != 0 {
		_, _, _ = setWindowLong.Call(hwnd, gwlExStyle, ex&^wsExNoRedirectionBitmap)
	}
	_, _, _ = setWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0027) // SWP_NOSIZE|SWP_NOMOVE|SWP_NOZORDER|SWP_FRAMECHANGED
	if build == nil || !glassBackdropSupported(build.BuildNumber) || hrFailed(extend) {
		return false
	}
	if hrFailed(setDwmAttribute(hwnd, dwmwaSystemBackdropType, dwmBackdropAcrylic)) &&
		hrFailed(setDwmAttribute(hwnd, dwmwaSystemBackdropType, dwmBackdropMica)) {
		return false
	}
	return true
}

func borderZone(lp uintptr) bool {
	if hostWindow == 0 {
		return false
	}
	if zoomed, _, _ := isZoomed.Call(hostWindow); zoomed != 0 {
		return false
	}
	var rect struct{ Left, Top, Right, Bottom int32 }
	getWindowRect.Call(hostWindow, uintptr(unsafe.Pointer(&rect)))
	x := int32(int16(lp & 0xffff))
	y := int32(int16((lp >> 16) & 0xffff))
	const border int32 = 8
	return x < rect.Left+border || x >= rect.Right-border || y < rect.Top+border || y >= rect.Bottom-border
}

func subclassChild(hwnd uintptr) {
	if hwnd == 0 || hwnd == hostWindow || childProc == 0 {
		return
	}
	if _, ok := childProcs.Load(hwnd); ok {
		return
	}
	prev, _, _ := setWindowLong.Call(hwnd, gwlWndProc, childProc)
	if prev == 0 || prev == childProc {
		return
	}
	childProcs.Store(hwnd, prev)
}

func subclassTree(root uintptr) {
	var walk func(uintptr, int)
	walk = func(hwnd uintptr, depth int) {
		if hwnd == 0 || depth > 8 {
			return
		}
		child, _, _ := getWindow.Call(hwnd, 5) // GW_CHILD
		for i := 0; child != 0 && i < 32; i++ {
			subclassChild(child)
			walk(child, depth+1)
			next, _, _ := getWindow.Call(child, 2) // GW_HWNDNEXT
			if next == 0 || next == child || next == hwnd {
				break
			}
			child = next
		}
	}
	walk(root, 0)
}

func installChildHooks(hwnd uintptr) {
	hostWindow = hwnd
	if childProc == 0 {
		childProc = windows.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
			if msg == wmNcHitTest && borderZone(lp) {
				return htTransparent
			}
			if orig, ok := childProcs.Load(hwnd); ok {
				r, _, _ := callWindowProc.Call(orig.(uintptr), hwnd, msg, wp, lp)
				return r
			}
			r, _, _ := defWindowProc.Call(hwnd, msg, wp, lp)
			return r
		})
	}
	subclassTree(hwnd)
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

// beginChromeMove starts the system move/resize loop while the mouse button is
// still down. It must run on the UI thread inside the web message callback.
func beginChromeMove(hwnd, hit uintptr) {
	if hwnd == 0 || hit == 0 {
		return
	}
	if hit != 2 {
		if zoomed, _, _ := isZoomed.Call(hwnd); zoomed != 0 {
			return
		}
	}
	releaseCapture.Call()
	sendMessageW.Call(hwnd, wmNcLButtonDown, hit, 0)
}

func setImmersiveDark(hwnd uintptr, dark bool) {
	value := int32(0)
	if dark {
		value = 1
	}
	_ = setDwmAttribute(hwnd, dwmwaUseImmersiveDarkMode, value)
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
	// The shared WebView2 runtime is detected before constructing the native window.
	runtimeVersion, detectError := webviewloader.GetInstalledVersion()
	if detectError != nil || runtimeVersion == "" {
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
	// 0x00000000 is the documented transparent WebView2 background (AARRGGBB).
	// A bare "0" is ignored by some runtimes and leaves an opaque black client.
	_ = os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "0x00000000")
	w := webview.NewWithOptions(webview.WebViewOptions{DataPath: filepath.Join(dir, "WebView2"), AutoFocus: true, WindowOptions: webview.WindowOptions{Title: "串口助手 · v" + version, Width: 1440, Height: 900}})
	if w == nil {
		alert("窗口初始化失败。")
		if smoke {
			os.Exit(1)
		}
		return
	}
	defer w.Destroy()
	w.SetSize(1024, 640, webview.HintMin)
	service := core.NewService(filepath.Join(dir, "logs"), core.OpenSerial)
	defer service.Shutdown()
	var quitting, closing atomic.Bool
	var smokeDone atomic.Bool
	var original uintptr
	callback := windows.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
		if msg == 0x0083 && wp != 0 { // WM_NCCALCSIZE: client area fills the borderless window
			if zoomed, _, _ := isZoomed.Call(hwnd); zoomed != 0 {
				frameX, _, _ := getSystemMetrics.Call(32)
				padX, _, _ := getSystemMetrics.Call(92)
				frameY, _, _ := getSystemMetrics.Call(33)
				padY, _, _ := getSystemMetrics.Call(93)
				r := (*struct{ Left, Top, Right, Bottom int32 })(unsafe.Pointer(lp))
				r.Left += int32(frameX + padX)
				r.Top += int32(frameY + padY)
				r.Right -= int32(frameX + padX)
				r.Bottom -= int32(frameY + padY)
			}
			return 0
		}
		if msg == wmParentNotify && uint16(wp) == 1 { // child created; WebView2 adds HWNDs late
			subclassTree(hwnd)
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
		if msg == 0x0014 { // WM_ERASEBKGND: the page or the DWM backdrop paints this
			return 1
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
	glassOn.Store(applyGlass(uintptr(w.Window())))
	installChildHooks(uintptr(w.Window()))
	invoke := func(r request) (any, error) {
		id, _ := arg[string](r, 0)
		switch r.Method {
		case "licenses":
			bytes, e := web.Assets.ReadFile("THIRD_PARTY_NOTICES.txt")
			return string(bytes), e
		case "initialize":
			config, warning := core.LoadConfig(filepath.Join(dir, "config.json"))
			return map[string]any{"config": config, "warning": warning, "version": version, "platform": "win32", "glass": glassOn.Load(), "logsDirectory": filepath.Join(dir, "logs")}, nil
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
			file, e := fileDialog(w, true, "导出串口日志", "serial-log-"+time.Now().Format("20060102-150405")+"."+o.Format, o.Format)
			if e != nil || file == "" {
				return map[string]any{"canceled": true}, e
			}
			e = service.Export(file, o)
			return map[string]any{"canceled": false, "path": file}, e
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
		case "setTheme":
			dark, e := arg[bool](r, 0)
			if e != nil {
				return nil, e
			}
			done := make(chan struct{})
			w.Dispatch(func() {
				setImmersiveDark(uintptr(w.Window()), dark)
				close(done)
			})
			<-done
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
	html := string(htmlBytes)
	start := strings.Index(html, "<meta http-equiv=\"Content-Security-Policy\"")
	end := strings.Index(html[start:], ">") + start
	policy := `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-` + nonce + `'; style-src 'unsafe-inline'; font-src data:; img-src data:; connect-src 'none'; base-uri 'none'; form-action 'none'">`
	html = html[:start] + policy + html[end+1:]
	cssText := string(css)
	if wall, err := web.Assets.ReadFile("wallpaper.jpg"); err == nil && len(wall) > 0 {
		cssText = strings.ReplaceAll(cssText, "wallpaper.jpg", "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString(wall))
	}
	cssText = strings.ReplaceAll(cssText, "OPPOSans-Regular.woff2", "data:font/woff2;base64,"+base64.StdEncoding.EncodeToString(font))
	html = strings.Replace(html, `<link rel="stylesheet" href="app.css">`, "<style>"+cssText+"</style>", 1)
	html = strings.Replace(html, `<script src="app.js"></script>`, `<script nonce="`+nonce+`">`+strings.Replace(string(bridge), "__BRIDGE_TOKEN__", bridgeToken, 1)+"\n"+strings.ReplaceAll(string(js), "</script", "<\\/script")+`</script>`, 1)
	if smoke {
		log.Print("setting HTML")
	}
	w.SetHtml(html)
	// The controller exists only after the environment callback. Apply the
	// backdrop again so Acrylic/Mica is not lost behind the first navigation.
	glassOn.Store(applyGlass(uintptr(w.Window())))
	subclassTree(uintptr(w.Window()))
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
