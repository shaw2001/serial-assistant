package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

var configMu sync.Mutex

func DefaultConfig() map[string]any {
	commands := map[string][]map[string]any{"rtt": {}, "at": {}, "binary": {}, "custom": {}}
	lists := map[string][][2]string{"rtt": {{"线程列表", "ps"}, {"定时器列表", "list_timer"}, {"低功耗状态", "pm_dump"}, {"系统信息", "sysinfo"}, {"CPU 使用率", "cpu"}}, "at": {{"连通测试", "AT"}, {"设备信息", "ATI"}, {"信号强度", "AT+CSQ"}, {"注册状态", "AT+CREG?"}, {"查询卡号", "AT+CCID"}}, "binary": {{"读取寄存器", "01 03 00 00 00 02 C4 0B"}}}
	for group, list := range lists {
		for i, c := range list {
			mode, ending := "text", "crlf"
			if group == "binary" {
				mode, ending = "hex", "none"
			}
			commands[group] = append(commands[group], map[string]any{"id": group + "-" + strconv.Itoa(i), "name": c[0], "content": c[1], "mode": mode, "encoding": "utf-8", "ending": ending})
		}
	}
	return map[string]any{"schema": 1, "devices": []any{}, "commands": commands}
}
func ValidateConfig(data []byte) error {
	if len(data) > 2*1024*1024 {
		return errors.New("配置文件不能超过 2 MiB。")
	}
	var c struct {
		Schema  int
		Devices []struct {
			ID              string
			Path            string
			Baud            string
			ReceiveEncoding string
			ReceiveNewline  string
			Params          struct {
				Data   int
				Parity string
				Stop   float64
				Flow   string
			}
			Draft   Payload
			History []Payload
		}
		Commands map[string][]struct {
			ID, Name string
			Payload
		}
	}
	if json.Unmarshal(data, &c) != nil || c.Schema != 1 || c.Devices == nil || len(c.Devices) > 4 {
		return errors.New("配置版本或设备列表无效。")
	}
	ids := map[string]bool{}
	for _, d := range c.Devices {
		baud, e := strconv.Atoi(d.Baud)
		if !idPattern.MatchString(d.ID) || ids[d.ID] || len(d.Path) > 256 || e != nil || baud < 1 || baud > 12000000 || !oneOf(d.ReceiveEncoding, "utf-8", "gbk", "ascii") {
			return errors.New("设备配置无效。")
		}
		ids[d.ID] = true
		if d.Params.Data < 5 || d.Params.Data > 8 || !oneOf(d.Params.Parity, "none", "even", "odd", "mark", "space") || !oneOf(d.Params.Flow, "none", "rtscts", "xonxoff") || (d.Params.Stop != 1 && d.Params.Stop != 1.5 && d.Params.Stop != 2) {
			return errors.New("串口参数无效。")
		}
		if d.ReceiveNewline != "" && !oneOf(d.ReceiveNewline, "auto", "crlf", "lf", "cr", "none", "batch") {
			return errors.New("接收换行设置无效。")
		}
		if len(d.History) > 100 || !validDraft(d.Draft) {
			return errors.New("发送草稿或历史无效。")
		}
		for _, p := range d.History {
			if !validDraft(p) {
				return errors.New("发送历史无效。")
			}
		}
	}
	for _, g := range []string{"rtt", "at", "binary", "custom"} {
		list, ok := c.Commands[g]
		if !ok || list == nil || len(list) > 200 {
			return errors.New("快捷指令分组无效。")
		}
		for _, p := range list {
			if len(p.ID) > 128 || len(p.Name) > 320 || !validDraft(p.Payload) {
				return errors.New("快捷指令格式无效。")
			}
		}
	}
	return nil
}
func validDraft(p Payload) bool {
	return len(p.Content) <= 196608 && oneOf(p.Mode, "text", "hex") && oneOf(p.Encoding, "utf-8", "ascii", "gbk") && oneOf(p.Ending, "none", "cr", "lf", "crlf")
}
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if s == v {
			return true
		}
	}
	return false
}
func LoadConfig(file string) (any, string) {
	data, e := os.ReadFile(file)
	if e == nil {
		e = ValidateConfig(data)
	}
	if e != nil {
		if os.IsNotExist(e) {
			return DefaultConfig(), ""
		}
		return DefaultConfig(), "本机配置读取失败，原文件保留，已使用默认配置。"
	}
	var c any
	_ = json.Unmarshal(data, &c)
	return c, ""
}
func SaveConfig(file string, data []byte) error {
	if e := ValidateConfig(data); e != nil {
		return e
	}
	configMu.Lock()
	defer configMu.Unlock()
	return AtomicWrite(file, data)
}
func AtomicWrite(file string, data []byte) error {
	if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(file), ".serial-*.tmp")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(name, file)
}
