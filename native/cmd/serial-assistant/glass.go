package main

// glassBackdropSupported reports whether this Windows build can host the
// DWM system backdrop used for the transparent glass window.
// DWMWA_SYSTEMBACKDROP_TYPE (Acrylic / Mica) is a Windows 11 API. Windows 10
// and older builds keep the bundled wallpaper behind the glass chrome.
func glassBackdropSupported(build uint32) bool {
	return build >= 22000
}
