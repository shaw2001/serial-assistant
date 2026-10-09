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
	"sync/atomic"
	"time"
	"unsafe"
)

// version is injected at build time: -ldflags "-X main.version=1.2.3"
var version = "0.1.0"

const releaseRepo = "shaw2001/serial-assistant"

var user32 = windows.NewLazySystemDLL("user32.dll")
var messageBox = user32.NewProc("MessageBoxW")
var setWindowPos = user32.NewProc("SetWindowPos")
var setWindowLong = user32.NewProc("SetWindowLongPtrW")
var callWindowProc = user32.NewProc("CallWindowProcW")
var postMessage = user32.NewProc("PostMessageW")
var shellExecute = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW")

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
	w := webview.NewWithOptions(webview.WebViewOptions{DataPath: filepath.Join(dir, "WebView2"), AutoFocus: true, WindowOptions: webview.WindowOptions{Title: "串口助手 · v" + version, Width: 1220, Height: 860}})
	if w == nil {
		alert("窗口初始化失败。")
		if smoke {
			os.Exit(1)
		}
		return
	}
	defer w.Destroy()
	w.SetSize(760, 620, webview.HintMin)
	service := core.NewService(filepath.Join(dir, "logs"), core.OpenSerial)
	defer service.Shutdown()
	var quitting, closing atomic.Bool
	var smokeDone atomic.Bool
	var original uintptr
	callback := windows.NewCallback(func(hwnd, msg, wp, lp uintptr) uintptr {
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
	original, _, _ = setWindowLong.Call(uintptr(w.Window()), ^uintptr(3), callback)
	invoke := func(r request) (any, error) {
		id, _ := arg[string](r, 0)
		switch r.Method {
		case "licenses":
			bytes, e := web.Assets.ReadFile("THIRD_PARTY_NOTICES.txt")
			return string(bytes), e
		case "initialize":
			config, warning := core.LoadConfig(filepath.Join(dir, "config.json"))
			return map[string]any{"config": config, "warning": warning, "version": version, "platform": "win32", "logsDirectory": filepath.Join(dir, "logs")}, nil
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
	cssText := strings.ReplaceAll(string(css), "OPPOSans-Regular.woff2", "data:font/woff2;base64,"+base64.StdEncoding.EncodeToString(font))
	html = strings.Replace(html, `<link rel="stylesheet" href="app.css">`, "<style>"+cssText+"</style>", 1)
	html = strings.Replace(html, `<script src="app.js"></script>`, `<script nonce="`+nonce+`">`+strings.Replace(string(bridge), "__BRIDGE_TOKEN__", bridgeToken, 1)+"\n"+strings.ReplaceAll(string(js), "</script", "<\\/script")+`</script>`, 1)
	if smoke {
		log.Print("setting HTML")
	}
	w.SetHtml(html)
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
