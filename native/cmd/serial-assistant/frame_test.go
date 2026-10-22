package main

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowIsOpaqueBackdropColour(t *testing.T) {
	if webviewBackgroundEnv != "FFEEF2FB" || webviewBackgroundARGB != 0xFFEEF2FB || hostBrushColor != 0x00FBF2EE {
		t.Fatal("window and WebView2 background must be the opaque backdrop colour #eef2fb")
	}
	if !backgroundIsOpaque(webviewBackgroundARGB) || backgroundIsOpaque(0) || backgroundIsOpaque(0x80FFFFFF) {
		t.Fatal("only alpha 0xFF counts as opaque")
	}
}

func TestOpaqueFrameMarginsHaveNoGlassSheetOrCaptionStrip(t *testing.T) {
	m := opaqueFrameMargins()
	for i, v := range m {
		if v < 0 {
			t.Fatalf("margin %d is %d: -1 would extend a glass sheet over the client", i, v)
		}
	}
	if m[2] != 0 {
		t.Fatal("top margin must stay 0 so DWM cannot draw caption buttons")
	}
	if m[3] != 1 {
		t.Fatal("one bottom pixel keeps the DWM shadow and rounded corners")
	}
}

func TestHostActionForRestoreAndActivation(t *testing.T) {
	cases := []struct {
		name  string
		msg   uint32
		wp    uintptr
		flags uint32
		want  hostAction
	}{
		{"minimize is a no-op (no hide, no 160x28 bounds)", wmSize, sizeMinimized, 0, hostNone},
		{"restore refreshes", wmSize, 0, 0, hostRefreshNow},
		{"maximize refreshes", wmSize, 2, 0, hostRefreshNow},
		{"SIZE_MAXSHOW ignored", wmSize, 3, 0, hostNone},
		{"shown", wmShowWindow, 1, 0, hostRefreshLater},
		{"hidden", wmShowWindow, 0, 0, hostNone},
		{"taskbar click activate", wmActivate, 2, 0, hostRefreshLater},
		{"activate", wmActivate, 1, 0, hostRefreshLater},
		{"activate while still minimized", wmActivate, 1 | 1<<16, 0, hostNone},
		{"deactivate", wmActivate, 0, 0, hostNone},
		{"SC_RESTORE from taskbar", wmSysCommand, scRestore, 0, hostRefreshLater},
		{"SC_MAXIMIZE", wmSysCommand, scMaximize | 2, 0, hostRefreshLater},
		{"SC_MINIMIZE", wmSysCommand, 0xF020, 0, hostNone},
		{"window pos with show", wmWindowPosChanged, 0, swpShowWindow | swpNoSize, hostRefreshLater},
		{"window pos with new size", wmWindowPosChanged, 0, 0x2, hostRefreshLater},
		{"plain move", wmWindowPosChanged, 0, swpNoSize | 0x2, hostNone},
		{"dpi change", wmDpiChanged, 0, 0, hostRefreshLater},
		{"unrelated", 0x0200, 0, 0, hostNone},
	}
	for _, c := range cases {
		if got := hostActionFor(c.msg, c.wp, c.flags); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

type fakeSurface struct{ calls []string }

func (f *fakeSurface) SetVisible(v bool) {
	if v {
		f.calls = append(f.calls, "visible")
	} else {
		f.calls = append(f.calls, "hidden")
	}
}
func (f *fakeSurface) ResizeToClient() { f.calls = append(f.calls, "bounds") }
func (f *fakeSurface) NotifyMoved()    { f.calls = append(f.calls, "notify") }
func (f *fakeSurface) Redraw()         { f.calls = append(f.calls, "redraw") }

func TestRefreshMakesVisibleBeforeBoundsAndNeverHides(t *testing.T) {
	// Restore, maximize and activation all go through the same refresh.
	for _, client := range []rect32{{0, 0, 1440, 900}, {0, 0, 1920, 1032}} {
		f := &fakeSurface{}
		if !refreshHostSurface(f, client) {
			t.Fatal("non-empty client must apply bounds")
		}
		if got := strings.Join(f.calls, ","); got != "visible,bounds,notify,redraw" {
			t.Fatalf("refresh order %s", got)
		}
	}
	f := &fakeSurface{}
	if refreshHostSurface(f, rect32{}) || strings.Join(f.calls, ",") != "visible" {
		t.Fatalf("empty client keeps the last bounds and only re-shows: %v", f.calls)
	}
}

func TestMaximizedClientRectUsesTheWindowsOwnMonitor(t *testing.T) {
	// Values from the user's diag log: maximized on the second monitor.
	proposed := rect32{1913, -7, 3847, 1087}
	second := rect32{1920, 0, 3840, 1032}
	primary := rect32{0, 0, 1920, 1032}
	if r, src := maximizedClientRect(proposed, second, true, 7, 7); r != second || src != "work-area" {
		t.Fatalf("second-monitor work area must be used: %v %s", r, src)
	}
	// The v0.2.5 bug: restoring from the taskbar asked for the iconic window's
	// monitor and got the primary one; the client ended up outside the window.
	r, src := maximizedClientRect(proposed, primary, true, 7, 7)
	if src != "frame-inset" || !rectContains(proposed, r) || r != (rect32{1920, 0, 3840, 1080}) {
		t.Fatalf("a work area outside the window must be rejected: %v %s", r, src)
	}
	if r, src := maximizedClientRect(proposed, rect32{}, false, 8, 8); src != "frame-inset" || !rectContains(proposed, r) {
		t.Fatalf("no work area: %v %s", r, src)
	}
	if r, _ := maximizedClientRect(rect32{0, 0, 10, 10}, rect32{}, false, 8, 8); rectEmpty(r) {
		t.Fatal("never produce an empty client")
	}
}

func TestFirstShowGateShowsExactlyOnce(t *testing.T) {
	var g firstShowGate
	if g.shownBy() != "" {
		t.Fatal("hidden until claimed")
	}
	var wg sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reason := "page-ready"
			if i%2 == 1 {
				reason = "timeout-fallback"
			}
			if g.claim(reason) {
				wins.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("window shown %d times", wins.Load())
	}
	if r := g.shownBy(); r != "page-ready" && r != "timeout-fallback" {
		t.Fatalf("reason %q", r)
	}
	if g.claim("page-ready") {
		t.Fatal("a later page-ready must not show again")
	}
}

func TestStartupShowTimeoutIsShortButNotInstant(t *testing.T) {
	if startupShowTimeout < 1500*time.Millisecond || startupShowTimeout > 2000*time.Millisecond {
		t.Fatalf("timeout %v outside 1.5–2 s", startupShowTimeout)
	}
	if swShowDefault != 10 {
		t.Fatal("first show must use SW_SHOWDEFAULT")
	}
}
