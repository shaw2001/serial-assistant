//go:build windows

package main

import (
	"fmt"
	webview "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"unicode/utf16"
	"unsafe"
)

type openFileName struct {
	Size             uint32
	Owner            uintptr
	Instance         uintptr
	Filter           *uint16
	CustomFilter     *uint16
	MaxCustomFilter  uint32
	FilterIndex      uint32
	File             *uint16
	MaxFile          uint32
	FileTitle        *uint16
	MaxFileTitle     uint32
	InitialDir       *uint16
	Title            *uint16
	Flags            uint32
	FileOffset       uint16
	FileExtension    uint16
	DefaultExtension *uint16
	CustomData       uintptr
	Hook             uintptr
	TemplateName     *uint16
	Reserved         uintptr
	ReservedDW       uint32
	FlagsEx          uint32
}

func fileDialog(w webview.WebView, save bool, title, name, extension string) (string, error) {
	var result string
	var failure error
	done := make(chan struct{})
	w.Dispatch(func() {
		defer close(done)
		buffer := make([]uint16, 32768)
		copy(buffer, windows.StringToUTF16(name))
		filter := utf16.Encode([]rune("文件 (*." + extension + ")\x00*." + extension + "\x00所有文件\x00*.*\x00\x00"))
		o := openFileName{Owner: uintptr(w.Window()), Filter: &filter[0], FilterIndex: 1, File: &buffer[0], MaxFile: uint32(len(buffer)), Title: wide(title), DefaultExtension: wide(extension), Flags: 0x80000 | 0x800 | 0x8}
		o.Size = uint32(unsafe.Sizeof(o))
		dll := windows.NewLazySystemDLL("comdlg32.dll")
		proc := dll.NewProc("GetOpenFileNameW")
		if save {
			proc = dll.NewProc("GetSaveFileNameW")
			o.Flags |= 2
		} else {
			o.Flags |= 0x1000
		}
		ok, _, _ := proc.Call(uintptr(unsafe.Pointer(&o)))
		if ok != 0 {
			result = windows.UTF16ToString(buffer)
		} else {
			code, _, _ := dll.NewProc("CommDlgExtendedError").Call()
			if code != 0 {
				failure = fmt.Errorf("文件对话框失败：0x%x", code)
			}
		}
	})
	<-done
	return result, failure
}
