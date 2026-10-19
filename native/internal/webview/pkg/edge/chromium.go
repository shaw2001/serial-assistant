//go:build windows
// +build windows

package edge

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
	"io.github.shaw2001/serialassistant/internal/webview/internal/w32"
)

type Chromium struct {
	blockedNavigations      atomic.Uint64
	navigationStarting      *securityNavigationHandler
	frameNavigationStarting *securityNavigationHandler
	initialDocumentURI      string
	newWindowRequested      *securityWindowHandler
	allowDocument           bool
	backgroundHR            uint32
	backgroundARGB          uint32
	documentLoaded          bool

	hwnd                  uintptr
	focusOnInit           bool
	controller            *ICoreWebView2Controller
	webview               *ICoreWebView2
	inited                uintptr
	envCompleted          *iCoreWebView2CreateCoreWebView2EnvironmentCompletedHandler
	controllerCompleted   *iCoreWebView2CreateCoreWebView2ControllerCompletedHandler
	webMessageReceived    *iCoreWebView2WebMessageReceivedEventHandler
	permissionRequested   *iCoreWebView2PermissionRequestedEventHandler
	webResourceRequested  *iCoreWebView2WebResourceRequestedEventHandler
	acceleratorKeyPressed *ICoreWebView2AcceleratorKeyPressedEventHandler
	navigationCompleted   *ICoreWebView2NavigationCompletedEventHandler

	environment *ICoreWebView2Environment

	// Settings
	DataPath                string
	BrowserExecutableFolder string

	// permissions
	permissions      map[CoreWebView2PermissionKind]CoreWebView2PermissionState
	globalPermission *CoreWebView2PermissionState

	// Callbacks
	MessageCallback              func(string)
	WebResourceRequestedCallback func(request *ICoreWebView2WebResourceRequest, args *ICoreWebView2WebResourceRequestedEventArgs)
	NavigationCompletedCallback  func(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs)
	AcceleratorKeyCallback       func(uint) bool
}

func NewChromium() *Chromium {
	e := &Chromium{}
	e.navigationStarting = newSecurityNavigationHandler(e, true)
	e.frameNavigationStarting = newSecurityNavigationHandler(e, false)
	e.newWindowRequested = newSecurityWindowHandler()
	/*
	 All these handlers are passed to native code through syscalls with 'uintptr(unsafe.Pointer(handler))' and we know
	 that a pointer to those will be kept in the native code. Furthermore these handlers als contain pointer to other Go
	 structs like the vtable.
	 This violates the unsafe.Pointer rule '(4) Conversion of a Pointer to a uintptr when calling syscall.Syscall.' because
	 theres no guarantee that Go doesn't move these objects.
	 AFAIK currently the Go runtime doesn't move HEAP objects, so we should be safe with these handlers. But they don't
	 guarantee it, because in the future Go might use a compacting GC.
	 There's a proposal to add a runtime.Pin function, to prevent moving pinned objects, which would allow to easily fix
	 this issue by just pinning the handlers. The https://go-review.googlesource.com/c/go/+/367296/ should land in Go 1.19.
	*/
	e.envCompleted = newICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler(e)
	e.controllerCompleted = newICoreWebView2CreateCoreWebView2ControllerCompletedHandler(e)
	e.webMessageReceived = newICoreWebView2WebMessageReceivedEventHandler(e)
	e.permissionRequested = newICoreWebView2PermissionRequestedEventHandler(e)
	e.webResourceRequested = newICoreWebView2WebResourceRequestedEventHandler(e)
	e.acceleratorKeyPressed = newICoreWebView2AcceleratorKeyPressedEventHandler(e)
	e.navigationCompleted = newICoreWebView2NavigationCompletedEventHandler(e)
	e.permissions = make(map[CoreWebView2PermissionKind]CoreWebView2PermissionState)

	return e
}

func (e *Chromium) Embed(hwnd uintptr) bool {
	e.hwnd = hwnd

	dataPath := e.DataPath
	if dataPath == "" {
		currentExePath := make([]uint16, windows.MAX_PATH)
		_, err := windows.GetModuleFileName(windows.Handle(0), &currentExePath[0], windows.MAX_PATH)
		if err != nil {
			// What to do here?
			return false
		}
		currentExeName := filepath.Base(windows.UTF16ToString(currentExePath))
		dataPath = filepath.Join(os.Getenv("AppData"), currentExeName)
	}

	var browserFolder *uint16
	if e.BrowserExecutableFolder != "" {
		browserFolder = windows.StringToUTF16Ptr(e.BrowserExecutableFolder)
	}
	res, err := createCoreWebView2EnvironmentWithOptions(browserFolder, windows.StringToUTF16Ptr(dataPath), 0, e.envCompleted)
	if err != nil {
		log.Printf("Error calling Webview2Loader: %v", err)
		return false
	} else if res != 0 {
		log.Printf("Result: %08x", res)
		return false
	}
	var msg w32.Msg
	for {
		if atomic.LoadUintptr(&e.inited) != 0 {
			break
		}
		r, _, _ := w32.User32GetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)
		if r == 0 {
			break
		}
		_, _, _ = w32.User32TranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		_, _, _ = w32.User32DispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	e.Init("window.external={invoke:s=>window.chrome.webview.postMessage(s)}")
	return true
}

func (e *Chromium) Navigate(url string) {
	_, _, _ = e.webview.vtbl.Navigate.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(url))),
	)
}

func (e *Chromium) NavigateToString(htmlContent string) {
	_ = e.NavigateToStringChecked(htmlContent)
}

// NavigateToStringChecked returns the HRESULT of NavigateToString. WebView2
// answers E_INVALIDARG when the page's data: URL is over Chromium's 2 MiB cap.
func (e *Chromium) NavigateToStringChecked(htmlContent string) error {
	securityTrace("NavigateToString loaded=%t length=%d", e.documentLoaded, len(htmlContent))
	if e.documentLoaded {
		return nil
	}
	if e.webview == nil {
		return errors.New("WebView2 is not ready")
	}
	content, err := windows.UTF16PtrFromString(htmlContent)
	if err != nil {
		return err
	}
	e.initialDocumentURI = "data:text/html;charset=utf-8;base64," + base64.StdEncoding.EncodeToString([]byte(htmlContent))
	e.allowDocument = true
	hr, _, _ := e.webview.vtbl.NavigateToString.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(content)),
	)
	if int32(hr) < 0 {
		e.allowDocument = false
		e.initialDocumentURI = ""
		return fmt.Errorf("NavigateToString HRESULT 0x%08x (page %d bytes)", uint32(hr), len(htmlContent))
	}
	return nil
}

func (e *Chromium) Init(script string) {
	_, _, _ = e.webview.vtbl.AddScriptToExecuteOnDocumentCreated.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(script))),
		0,
	)
}

func (e *Chromium) Eval(script string) {
	if !e.trustedDocument() {
		return
	}
	_script, err := windows.UTF16PtrFromString(script)
	if err != nil {
		log.Fatal(err)
	}

	_, _, _ = e.webview.vtbl.ExecuteScript.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(_script)),
		0,
	)
}

func (e *Chromium) Show() error {
	return e.controller.PutIsVisible(true)
}

func (e *Chromium) Hide() error {
	return e.controller.PutIsVisible(false)
}

func (e *Chromium) QueryInterface(_, _ uintptr) uintptr {
	return 0
}

func (e *Chromium) AddRef() uintptr {
	return 1
}

func (e *Chromium) Release() uintptr {
	return 1
}

func (e *Chromium) EnvironmentCompleted(res uintptr, env *ICoreWebView2Environment) uintptr {
	if int64(res) < 0 {
		log.Fatalf("Creating environment failed with %08x", res)
	}
	_, _, _ = env.vtbl.AddRef.Call(uintptr(unsafe.Pointer(env)))
	e.environment = env

	_, _, _ = env.vtbl.CreateCoreWebView2Controller.Call(
		uintptr(unsafe.Pointer(env)),
		e.hwnd,
		uintptr(unsafe.Pointer(e.controllerCompleted)),
	)
	return 0
}

func (e *Chromium) CreateCoreWebView2ControllerCompleted(res uintptr, controller *ICoreWebView2Controller) uintptr {
	if int64(res) < 0 {
		log.Fatalf("Creating controller failed with %08x", res)
	}
	_, _, _ = controller.vtbl.AddRef.Call(uintptr(unsafe.Pointer(controller)))
	e.controller = controller
	// Opaque white: the window never shows the desktop through the page.
	e.backgroundHR = 0xFFFFFFFF
	e.backgroundARGB = 0xFFFFFFFF
	if ctrl2 := controller.GetICoreWebView2Controller2(); ctrl2 != nil {
		// COREWEBVIEW2_COLOR {A,R,G,B} is passed by value; all 0xFF is opaque white.
		hr, _, _ := ctrl2.vtbl.PutDefaultBackgroundColor.Call(uintptr(unsafe.Pointer(ctrl2)), 0xFFFFFFFF)
		e.backgroundHR = uint32(hr)
		var c COREWEBVIEW2_COLOR
		if hr, _, _ := ctrl2.vtbl.GetDefaultBackgroundColor.Call(uintptr(unsafe.Pointer(ctrl2)), uintptr(unsafe.Pointer(&c))); int32(hr) >= 0 {
			e.backgroundARGB = uint32(c.A)<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		}
	}

	var token _EventRegistrationToken
	_, _, _ = controller.vtbl.GetCoreWebView2.Call(
		uintptr(unsafe.Pointer(controller)),
		uintptr(unsafe.Pointer(&e.webview)),
	)
	_, _, _ = e.webview.vtbl.AddRef.Call(
		uintptr(unsafe.Pointer(e.webview)),
	)
	_, _, _ = e.webview.vtbl.AddWebMessageReceived.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.webMessageReceived)),
		uintptr(unsafe.Pointer(&token)),
	)
	_, _, _ = e.webview.vtbl.AddPermissionRequested.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.permissionRequested)),
		uintptr(unsafe.Pointer(&token)),
	)
	_, _, _ = e.webview.vtbl.AddWebResourceRequested.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.webResourceRequested)),
		uintptr(unsafe.Pointer(&token)),
	)
	_, _, _ = e.webview.vtbl.AddNavigationCompleted.Call(
		uintptr(unsafe.Pointer(e.webview)),
		uintptr(unsafe.Pointer(e.navigationCompleted)),
		uintptr(unsafe.Pointer(&token)),
	)

	for _, registration := range []struct {
		proc    ComProc
		handler uintptr
	}{
		{e.webview.vtbl.AddNavigationStarting, uintptr(unsafe.Pointer(e.navigationStarting))},
		{e.webview.vtbl.AddFrameNavigationStarting, uintptr(unsafe.Pointer(e.frameNavigationStarting))},
		{e.webview.vtbl.AddNewWindowRequested, uintptr(unsafe.Pointer(e.newWindowRequested))},
	} {
		hr, _, _ := registration.proc.Call(uintptr(unsafe.Pointer(e.webview)), registration.handler, uintptr(unsafe.Pointer(&token)))
		if int32(hr) < 0 {
			log.Fatal("Unable to install WebView security policy")
		}
	}
	_ = e.controller.AddAcceleratorKeyPressed(e.acceleratorKeyPressed, &token)

	atomic.StoreUintptr(&e.inited, 1)

	if e.focusOnInit {
		e.Focus()
	}

	return 0
}

func (e *Chromium) MessageReceived(sender *ICoreWebView2, args *iCoreWebView2WebMessageReceivedEventArgs) uintptr {
	securityTrace("web message received loaded=%t", e.documentLoaded)
	if !e.trustedDocument() {
		return 0
	}
	var source *uint16
	hr, _, _ := args.vtbl.GetSource.Call(uintptr(unsafe.Pointer(args)), uintptr(unsafe.Pointer(&source)))
	origin := w32.Utf16PtrToString(source)
	securityTrace("message sourceLength=%d trusted=%t hr=%x", len(origin), e.isDocumentSource(origin), hr)
	windows.CoTaskMemFree(unsafe.Pointer(source))
	if int32(hr) < 0 || !e.isDocumentSource(origin) {
		return 0
	}
	var message *uint16
	_, _, _ = args.vtbl.TryGetWebMessageAsString.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(&message)),
	)
	value := w32.Utf16PtrToString(message)
	if len(value) <= 3*1024*1024 && e.MessageCallback != nil {
		e.MessageCallback(value)
	}
	windows.CoTaskMemFree(unsafe.Pointer(message))
	return 0
}

func (e *Chromium) SetPermission(kind CoreWebView2PermissionKind, state CoreWebView2PermissionState) {
	e.permissions[kind] = state
}

func (e *Chromium) SetGlobalPermission(state CoreWebView2PermissionState) {
	e.globalPermission = &state
}

func (e *Chromium) PermissionRequested(_ *ICoreWebView2, args *iCoreWebView2PermissionRequestedEventArgs) uintptr {
	var kind CoreWebView2PermissionKind
	_, _, _ = args.vtbl.GetPermissionKind.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(unsafe.Pointer(&kind)),
	)
	var result CoreWebView2PermissionState
	if e.globalPermission != nil {
		result = *e.globalPermission
	} else {
		var ok bool
		result, ok = e.permissions[kind]
		if !ok {
			result = CoreWebView2PermissionStateDefault
		}
	}
	_, _, _ = args.vtbl.PutState.Call(
		uintptr(unsafe.Pointer(args)),
		uintptr(result),
	)
	return 0
}

func (e *Chromium) WebResourceRequested(sender *ICoreWebView2, args *ICoreWebView2WebResourceRequestedEventArgs) uintptr {
	req, err := args.GetRequest()
	if err != nil {
		log.Fatal(err)
	}
	if e.WebResourceRequestedCallback != nil {
		e.WebResourceRequestedCallback(req, args)
	}
	return 0
}

func (e *Chromium) AddWebResourceRequestedFilter(filter string, ctx COREWEBVIEW2_WEB_RESOURCE_CONTEXT) {
	err := e.webview.AddWebResourceRequestedFilter(filter, ctx)
	if err != nil {
		log.Fatal(err)
	}
}

func (e *Chromium) Environment() *ICoreWebView2Environment {
	return e.environment
}

// AcceleratorKeyPressed is called when an accelerator key is pressed.
// If the AcceleratorKeyCallback method has been set, it will defer handling of the keypress
// to the callback. That callback returns a bool indicating if the event was handled.
func (e *Chromium) AcceleratorKeyPressed(sender *ICoreWebView2Controller, args *ICoreWebView2AcceleratorKeyPressedEventArgs) uintptr {
	if e.AcceleratorKeyCallback == nil {
		return 0
	}
	eventKind, _ := args.GetKeyEventKind()
	if eventKind == COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN ||
		eventKind == COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN {
		virtualKey, _ := args.GetVirtualKey()
		status, _ := args.GetPhysicalKeyStatus()
		if !status.WasKeyDown {
			_ = args.PutHandled(e.AcceleratorKeyCallback(virtualKey))
			return 0
		}
	}
	_ = args.PutHandled(false)
	return 0
}

func (e *Chromium) GetSettings() (*ICoreWebViewSettings, error) {
	return e.webview.GetSettings()
}

func (e *Chromium) GetController() *ICoreWebView2Controller {
	return e.controller
}

func boolToInt(input bool) int {
	if input {
		return 1
	}
	return 0
}

func (e *Chromium) NavigationCompleted(sender *ICoreWebView2, args *ICoreWebView2NavigationCompletedEventArgs) uintptr {
	if e.NavigationCompletedCallback != nil {
		e.NavigationCompletedCallback(sender, args)
	}
	return 0
}

func (e *Chromium) NotifyParentWindowPositionChanged() error {
	//It looks like the wndproc function is called before the controller initialization is complete.
	//Because of this the controller is nil
	if e.controller == nil {
		return nil
	}
	return e.controller.NotifyParentWindowPositionChanged()
}

func (e *Chromium) Focus() {
	if e.controller == nil {
		e.focusOnInit = true
		return
	}
	_ = e.controller.MoveFocus(COREWEBVIEW2_MOVE_FOCUS_REASON_PROGRAMMATIC)
}

// BackgroundState reports the HRESULT of put_DefaultBackgroundColor and the
// colour WebView2 reads back (0x00000000 means transparent). 0xFFFFFFFF in
// both means ICoreWebView2Controller2 was unavailable.
func (e *Chromium) BackgroundState() (hr uint32, argb uint32) {
	return e.backgroundHR, e.backgroundARGB
}

// RefreshHost re-applies the controller bounds and visibility after the host
// window changed size or state (maximize, restore, snap), and returns the
// client rectangle that was applied.
// SetHostVisible toggles WebView2 visibility without touching its bounds, so a
// minimize never collapses the controller to the 0x0 iconic client rect.
func (e *Chromium) SetHostVisible(visible bool) {
	if e.controller == nil {
		return
	}
	_ = e.controller.PutIsVisible(visible)
}

// NotifyMoved tells WebView2 that the host window moved (monitor/DPI changes).
func (e *Chromium) NotifyMoved() {
	if e.controller == nil {
		return
	}
	_ = e.controller.NotifyParentWindowPositionChanged()
}

func (e *Chromium) RefreshHost(visible bool) w32.Rect {
	var bounds w32.Rect
	if e.controller == nil {
		return bounds
	}
	_, _, _ = w32.User32GetClientRect.Call(e.hwnd, uintptr(unsafe.Pointer(&bounds)))
	e.Resize()
	_ = e.controller.PutIsVisible(visible)
	_ = e.controller.NotifyParentWindowPositionChanged()
	return bounds
}
