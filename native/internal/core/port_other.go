//go:build !windows

package core

import (
	"errors"
	"go.bug.st/serial"
	"time"
)

func OpenSerial(o Options) (Port, error) {
	if o.Flow != "none" {
		return nil, errors.New("该测试平台只支持无流控，Windows 支持全部流控选项。")
	}
	parity := map[string]serial.Parity{"none": serial.NoParity, "odd": serial.OddParity, "even": serial.EvenParity, "mark": serial.MarkParity, "space": serial.SpaceParity}
	stop := map[float64]serial.StopBits{1: serial.OneStopBit, 1.5: serial.OnePointFiveStopBits, 2: serial.TwoStopBits}
	p, e := serial.Open(o.Path, &serial.Mode{BaudRate: o.BaudRate, DataBits: o.DataBits, Parity: parity[o.Parity], StopBits: stop[o.StopBits]})
	if e != nil {
		return nil, e
	}
	if e = p.SetReadTimeout(100 * time.Millisecond); e != nil {
		p.Close()
		return nil, e
	}
	return p, nil
}
