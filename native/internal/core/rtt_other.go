//go:build !windows

package core

import "errors"

// OpenRTT is only available in the Windows build.
func OpenRTT(RTTOptions) (Port, error) {
	return nil, errors.New("J-Link RTT 仅 Windows 版支持。")
}
