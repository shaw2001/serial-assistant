//go:build windows

package edge

import (
	"golang.org/x/sys/windows"
	"io.github.shaw2001/serialassistant/internal/webview/internal/w32"
	"unsafe"
)

var SecurityTrace func(string, ...any)

func securityTrace(format string, args ...any) {
	if SecurityTrace != nil {
		SecurityTrace(format, args...)
	}
}

// COM layout matches ICoreWebView2NavigationStartingEventArgs.
type securityNavigationArgs struct {
	vtbl *struct {
		_IUnknownVtbl
		GetURI, GetIsUserInitiated, GetIsRedirected, GetRequestHeaders, GetCancel, PutCancel, GetNavigationID ComProc
	}
}
type securityNavigationHandler struct {
	vtbl *struct {
		_IUnknownVtbl
		Invoke ComProc
	}
	browser  *Chromium
	topLevel bool
}

func newSecurityNavigationHandler(e *Chromium, topLevel bool) *securityNavigationHandler {
	h := &securityNavigationHandler{browser: e, topLevel: topLevel}
	h.vtbl = &struct {
		_IUnknownVtbl
		Invoke ComProc
	}{_IUnknownVtbl{
		NewComProc(func(h *securityNavigationHandler, _ uintptr, out *uintptr) uintptr {
			*out = uintptr(unsafe.Pointer(h))
			return 0
		}),
		NewComProc(func(h *securityNavigationHandler) uintptr { return 1 }),
		NewComProc(func(h *securityNavigationHandler) uintptr { return 1 }),
	}, NewComProc(func(h *securityNavigationHandler, _ *ICoreWebView2, a *securityNavigationArgs) uintptr {
		var uri *uint16
		hr, _, _ := a.vtbl.GetURI.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&uri)))
		target := w32.Utf16PtrToString(uri)
		securityTrace("navigation uriLength=%d expected=%t blank=%t hr=%x allow=%t loaded=%t top=%t", len(target), target == h.browser.initialDocumentURI, target == "about:blank", hr, h.browser.allowDocument, h.browser.documentLoaded, h.topLevel)
		windows.CoTaskMemFree(unsafe.Pointer(uri))
		if int32(hr) >= 0 && h.topLevel && h.browser.allowDocument && !h.browser.documentLoaded && h.browser.isDocumentSource(target) {
			h.browser.allowDocument = false
			h.browser.documentLoaded = true
			return 0
		}
		h.browser.blockedNavigations.Add(1)
		a.vtbl.PutCancel.Call(uintptr(unsafe.Pointer(a)), 1)
		return 0
	})}
	return h
}

// WebView2 may report the host-provided HTML as a data URI during navigation.
// Trust only this exact per-launch document, never arbitrary data: pages.
func (e *Chromium) isDocumentSource(source string) bool {
	return source == "about:blank" || (e.initialDocumentURI != "" && source == e.initialDocumentURI)
}
func (e *Chromium) trustedDocument() bool {
	if !e.documentLoaded || e.webview == nil {
		return false
	}
	var uri *uint16
	hr, _, _ := e.webview.vtbl.GetSource.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(&uri)))
	source := w32.Utf16PtrToString(uri)
	securityTrace("sourceLength=%d trusted=%t hr=%x loaded=%t", len(source), e.isDocumentSource(source), hr, e.documentLoaded)
	windows.CoTaskMemFree(unsafe.Pointer(uri))
	return int32(hr) >= 0 && e.isDocumentSource(source)
}

// ICoreWebView2NewWindowRequestedEventArgs starts with these methods.
type securityWindowArgs struct {
	vtbl *struct {
		_IUnknownVtbl
		GetURI, PutNewWindow, GetNewWindow, PutHandled ComProc
	}
}
type securityWindowHandler struct {
	vtbl *struct {
		_IUnknownVtbl
		Invoke ComProc
	}
}

func newSecurityWindowHandler() *securityWindowHandler {
	h := &securityWindowHandler{}
	h.vtbl = &struct {
		_IUnknownVtbl
		Invoke ComProc
	}{_IUnknownVtbl{
		NewComProc(func(h *securityWindowHandler, _ uintptr, out *uintptr) uintptr {
			*out = uintptr(unsafe.Pointer(h))
			return 0
		}),
		NewComProc(func(h *securityWindowHandler) uintptr { return 1 }), NewComProc(func(h *securityWindowHandler) uintptr { return 1 }),
	}, NewComProc(func(h *securityWindowHandler, _ *ICoreWebView2, a *securityWindowArgs) uintptr {
		a.vtbl.PutHandled.Call(uintptr(unsafe.Pointer(a)), 1)
		return 0
	})}
	return h
}

func (e *Chromium) BlockedNavigations() uint64 { return e.blockedNavigations.Load() }
