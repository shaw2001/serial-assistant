package main

import (
	"sync/atomic"
	"time"
)

// The window is an opaque surface painted with the page backdrop colour
// (#eef2fb, the top of the in-app glass backdrop): no DWM Acrylic/Mica, no
// blur-behind, no transparent WebView2 background. The v0.4.0 glass look is
// drawn inside the page over its own backdrop, never over the desktop.
const (
	// webviewBackgroundEnv is WEBVIEW2_DEFAULT_BACKGROUND_COLOR (AARRGGBB hex).
	webviewBackgroundEnv = "FFEEF2FB"
	// webviewBackgroundARGB is the controller's DefaultBackgroundColor.
	webviewBackgroundARGB uint32 = 0xFFEEF2FB
	// hostBrushColor is the GDI COLORREF (0x00BBGGRR) used to paint the host
	// window before and behind WebView2.
	hostBrushColor uint32 = 0x00FBF2EE
)

// opaqueFrameMargins are the DWM frame margins (left, right, top, bottom) for
// the borderless window. A single bottom pixel keeps the DWM shadow and Win11
// rounded corners. Nothing is extended at the top, so DWM never has a strip to
// draw native caption buttons on, and nothing is a full -1 glass sheet.
func opaqueFrameMargins() [4]int32 { return [4]int32{0, 0, 0, 1} }

// backgroundIsOpaque reports whether WebView2 accepted an opaque default
// background (alpha 0xFF), i.e. the page can never show through to the desktop.
func backgroundIsOpaque(argb uint32) bool { return argb>>24 == 0xFF }

// rect32 is a Win32 RECT.
type rect32 = struct{ Left, Top, Right, Bottom int32 }

func rectEmpty(r rect32) bool { return r.Right <= r.Left || r.Bottom <= r.Top }

func rectContains(outer, inner rect32) bool {
	return inner.Left >= outer.Left && inner.Top >= outer.Top && inner.Right <= outer.Right && inner.Bottom <= outer.Bottom
}

// maximizedClientRect is the WM_NCCALCSIZE client rectangle (screen
// coordinates) for the borderless window while maximized. proposed is the new
// window rectangle (NCCALCSIZE_PARAMS.rgrc[0]), which overhangs its monitor by
// the resize frame; work is the work area of the monitor that rectangle is on.
//
// The work area is only trusted when it lies inside proposed. When the window
// comes back from the taskbar straight into the maximized state, the window
// itself is still parked at the iconic position (-32000,-32000), so asking for
// "the window's monitor" returns the primary monitor. Its work area does not
// overlap a window maximized on the second monitor; using it put the whole
// client area outside the window, which then showed only a native title bar
// over white. In that case (or without a work area) inset proposed by the frame.
func maximizedClientRect(proposed, work rect32, workOK bool, frameX, frameY int32) (rect32, string) {
	if workOK && !rectEmpty(work) && rectContains(proposed, work) {
		return work, "work-area"
	}
	r := rect32{proposed.Left + frameX, proposed.Top + frameY, proposed.Right - frameX, proposed.Bottom - frameY}
	if rectEmpty(r) {
		return proposed, "proposed"
	}
	return r, "frame-inset"
}

// hostAction is how the WebView2 host reacts to a top-level window message.
type hostAction int

const (
	// hostNone: leave WebView2 alone. This includes SIZE_MINIMIZED: the
	// controller is neither hidden nor shrunk to the iconic 160x28/0x0 client.
	hostNone hostAction = iota
	// hostRefreshNow: make WebView2 visible, put its bounds to the client
	// rectangle, notify it, repaint; and again once the queue has settled.
	hostRefreshNow
	// hostRefreshLater: the window is (re)appearing or moved/resized; refresh
	// once the restore, show or activation has finished.
	hostRefreshLater
)

const (
	wmActivate          = 0x0006
	wmSize              = 0x0005
	wmShowWindow        = 0x0018
	wmWindowPosChanged  = 0x0047
	wmDisplayChange     = 0x007E
	wmSysCommand        = 0x0112
	wmDpiChanged        = 0x02E0
	wmAppRefreshHost    = 0x8000 + 0x51 // WM_APP-based private message
	refreshHostTimerID  = 0x5A51
	refreshHostDelayMs  = 250
	sizeMinimized       = 1
	scRestore           = 0xF120
	scMaximize          = 0xF030
	swpNoSize           = 0x0001
	swpShowWindow       = 0x0040
	windowPosFlagOffset = 32 // WINDOWPOS.flags on 64-bit Windows
)

// hostActionFor decides what a message means for the WebView2 controller.
// posFlags is WINDOWPOS.flags for WM_WINDOWPOSCHANGED and ignored otherwise.
func hostActionFor(msg uint32, wp uintptr, posFlags uint32) hostAction {
	switch msg {
	case wmSize:
		switch wp {
		case 0, 2: // SIZE_RESTORED, SIZE_MAXIMIZED
			return hostRefreshNow
		}
		// SIZE_MINIMIZED and the SIZE_MAXSHOW/MAXHIDE notifications change nothing.
	case wmShowWindow:
		if wp != 0 {
			return hostRefreshLater
		}
	case wmActivate:
		// LOWORD: WA_ACTIVE/WA_CLICKACTIVE; HIWORD: non-zero while minimized.
		if wp&0xFFFF != 0 && (wp>>16)&0xFFFF == 0 {
			return hostRefreshLater
		}
	case wmSysCommand:
		if c := wp & 0xFFF0; c == scRestore || c == scMaximize {
			return hostRefreshLater
		}
	case wmWindowPosChanged:
		if posFlags&swpShowWindow != 0 || posFlags&swpNoSize == 0 {
			return hostRefreshLater
		}
	case wmDisplayChange, wmDpiChanged:
		return hostRefreshLater
	}
	return hostNone
}

// hostSurface is the part of the WebView2 controller a refresh drives.
type hostSurface interface {
	SetVisible(visible bool) // put_IsVisible
	ResizeToClient()         // put_Bounds(GetClientRect)
	NotifyMoved()            // NotifyParentWindowPositionChanged
	Redraw()                 // RedrawWindow on the host and all child HWNDs
}

// refreshHostSurface makes WebView2 visible first, then (only for a non-empty
// client rectangle) puts its bounds, notifies it and repaints the host and the
// controller's child windows. An empty client (minimized) keeps the last good
// bounds. It reports whether bounds were applied.
func refreshHostSurface(s hostSurface, client rect32) bool {
	s.SetVisible(true)
	if rectEmpty(client) {
		return false
	}
	s.ResizeToClient()
	s.NotifyMoved()
	s.Redraw()
	return true
}

// startupShowTimeout is the longest the window stays hidden at startup when
// the page never reports ready (script error, slow runtime): it is then shown
// anyway so it can never stay invisible.
const startupShowTimeout = 1800 * time.Millisecond

// swShowDefault honours the launch's show state (e.g. a shortcut set to
// "最大化") for the first ShowWindow call.
const swShowDefault = 10

// firstShowGate lets exactly one caller perform the first ShowWindow: the
// page-ready signal or the timeout fallback, whichever comes first.
type firstShowGate struct {
	done   atomic.Bool
	reason atomic.Value
}

// claim reports whether this caller shows the window; later callers get false.
func (g *firstShowGate) claim(reason string) bool {
	if !g.done.CompareAndSwap(false, true) {
		return false
	}
	g.reason.Store(reason)
	return true
}

// shownBy is the reason the window was first shown, or "" while still hidden.
func (g *firstShowGate) shownBy() string {
	if v, ok := g.reason.Load().(string); ok {
		return v
	}
	return ""
}
