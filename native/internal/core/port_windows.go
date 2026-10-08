//go:build windows

package core

import (
	"errors"
	"golang.org/x/sys/windows"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

type windowsPort struct {
	h      windows.Handle
	mu     sync.Mutex
	closed bool
	ops    sync.WaitGroup
}

func OpenSerial(o Options) (Port, error) {
	name := o.Path
	if !strings.HasPrefix(name, `\\.\`) {
		name = `\\.\` + name
	}
	p, e := windows.UTF16PtrFromString(name)
	if e != nil {
		return nil, e
	}
	h, e := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if e != nil {
		return nil, e
	}
	ok := false
	defer func() {
		if !ok {
			windows.CloseHandle(h)
		}
	}()
	d := &windows.DCB{DCBlength: uint32(unsafe.Sizeof(windows.DCB{}))}
	if e = windows.GetCommState(h, d); e != nil {
		return nil, e
	}
	d.BaudRate = uint32(o.BaudRate)
	d.ByteSize = byte(o.DataBits)
	d.Parity = map[string]byte{"none": 0, "odd": 1, "even": 2, "mark": 3, "space": 4}[o.Parity]
	d.StopBits = map[float64]byte{1: 0, 1.5: 1, 2: 2}[o.StopBits]
	d.Flags = 1 | (1 << 4) | (1 << 12)
	if o.Parity != "none" {
		d.Flags |= 1 << 1
	}
	switch o.Flow {
	case "rtscts":
		d.Flags &^= 3 << 12
		d.Flags |= (1 << 2) | (2 << 12)
	case "xonxoff":
		d.Flags |= (1 << 8) | (1 << 9)
	}
	d.XonChar = 17
	d.XoffChar = 19
	d.XonLim = 2048
	d.XoffLim = 512
	if e = windows.SetCommState(h, d); e != nil {
		return nil, e
	}
	timeouts := windows.CommTimeouts{ReadIntervalTimeout: 0xffffffff, ReadTotalTimeoutMultiplier: 0xffffffff, ReadTotalTimeoutConstant: 100, WriteTotalTimeoutConstant: 10000}
	if e = windows.SetCommTimeouts(h, &timeouts); e != nil {
		return nil, e
	}
	ok = true
	return &windowsPort{h: h}, nil
}
func (p *windowsPort) io(data []byte, read bool) (int, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return 0, errors.New("串口已关闭。")
	}
	p.ops.Add(1)
	p.mu.Unlock()
	defer p.ops.Done()
	event, e := windows.CreateEvent(nil, 1, 0, nil)
	if e != nil {
		return 0, e
	}
	defer windows.CloseHandle(event)
	over := windows.Overlapped{HEvent: event}
	var n uint32
	if read {
		e = windows.ReadFile(p.h, data, &n, &over)
	} else {
		e = windows.WriteFile(p.h, data, &n, &over)
	}
	if e == windows.ERROR_IO_PENDING {
		wait, err := windows.WaitForSingleObject(event, 12000)
		if err != nil || wait != windows.WAIT_OBJECT_0 {
			_ = windows.CancelIoEx(p.h, &over)
			_ = windows.GetOverlappedResult(p.h, &over, &n, true)
			if err != nil {
				e = err
			} else {
				e = errors.New("串口操作超时。")
			}
		} else {
			e = windows.GetOverlappedResult(p.h, &over, &n, false)
		}
	}
	runtime.KeepAlive(data)
	return int(n), e
}
func (p *windowsPort) Read(b []byte) (int, error)  { return p.io(b, true) }
func (p *windowsPort) Write(b []byte) (int, error) { return p.io(b, false) }

// Write completion means accepted by the serial driver, not a device acknowledgement.
func (p *windowsPort) Drain() error { return nil }
func (p *windowsPort) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	_ = windows.CancelIoEx(p.h, nil)
	p.mu.Unlock()
	p.ops.Wait()
	return windows.CloseHandle(p.h)
}
