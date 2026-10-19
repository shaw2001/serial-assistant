//go:build windows

package core

import (
	"os/exec"
	"syscall"
)

func init() {
	// CREATE_NO_WINDOW: the logger gets a hidden console, so no window flashes
	// and its "press any key" check never sees a stray key press.
	prepareRTTCmd = func(c *exec.Cmd) {
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	}
}

// OpenRTT starts a J-Link RTT session through SEGGER's JLinkRTTLogger.exe.
func OpenRTT(o RTTOptions) (Port, error) { return openRTT(o) }
