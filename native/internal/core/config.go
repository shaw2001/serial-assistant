package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

type Config struct {
	Schema   int                        `json:"schema"`
	ActiveID string                     `json:"activeId,omitempty"`
	Devices  []DeviceConfig             `json:"devices"`
	Commands map[string][]CommandConfig `json:"commands"`
	UI       UIConfig                   `json:"ui"`
}
type UIConfig struct {
	AlwaysTop      bool `json:"alwaysTop"`
	RememberInput  bool `json:"rememberInput"`
	AutoUpdate     bool `json:"autoUpdate"`
	PrivacyVersion int  `json:"privacyVersion"`
}
type HistoryPayload struct {
	Payload
	Time string `json:"time,omitempty"`
}
type CommandConfig struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Payload
}
type DeviceConfig struct {
	ID              string `json:"id"`
	Path            string `json:"path"`
	Baud            string `json:"baud"`
	CustomBaud      string `json:"customBaud,omitempty"`
	ReceiveEncoding string `json:"receiveEncoding"`
	ReceiveNewline  string `json:"receiveNewline,omitempty"`
	Params          struct {
		Data   int     `json:"data"`
		Parity string  `json:"parity"`
		Stop   float64 `json:"stop"`
		Flow   string  `json:"flow"`
	} `json:"params"`
	Draft          Payload          `json:"draft"`
	History        []HistoryPayload `json:"history"`
	Group          string           `json:"group,omitempty"`
	Mode           string           `json:"mode,omitempty"`
	LogFontSize    string           `json:"logFontSize,omitempty"`
	Interval       string           `json:"interval,omitempty"`
	InputUncertain bool             `json:"inputUncertain"`
	WrapLines      bool             `json:"wrapLines"`
	ShowMeta       bool             `json:"showMeta"`
	Capture        bool             `json:"capture"`
	Highlight      bool             `json:"highlight"`
	OnlyMatches    bool             `json:"onlyMatches"`
	Autoscroll     bool             `json:"autoscroll"`
}

// Validate canonical JSON keys (including duplicates), then decode to typed data.
// Re-encoding typed data prevents Go/JS case-folding differences at the boundary.
func DecodeConfig(data []byte) (Config, error) {
	var c Config
	if len(data) > 2*1024*1024 {
		return c, errors.New("配置文件不能超过 2 MiB。")
	}
	if e := checkJSONKeys(data); e != nil {
		return c, e
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&c); e != nil {
		return c, errors.New("配置包含无效或未知字段。")
	}
	if c.Schema != 1 || c.Devices == nil || len(c.Devices) > 4 || len(c.Commands) != 4 {
		return c, errors.New("配置版本、设备或分组无效。")
	}
	ids := map[string]bool{}
	for i := range c.Devices {
		d := &c.Devices[i]
		if d.History == nil {
			d.History = []HistoryPayload{}
		}
		baud, e := strconv.Atoi(d.Baud)
		if !idPattern.MatchString(d.ID) || ids[d.ID] || len(d.Path) > 256 || e != nil || baud < 1 || baud > 12000000 || !oneOf(d.ReceiveEncoding, "utf-8", "gbk", "ascii") {
			return c, errors.New("设备配置无效。")
		}
		ids[d.ID] = true
		if d.Params.Data < 5 || d.Params.Data > 8 || !oneOf(d.Params.Parity, "none", "even", "odd", "mark", "space") || !oneOf(d.Params.Flow, "none", "rtscts", "xonxoff") || (d.Params.Stop != 1 && d.Params.Stop != 1.5 && d.Params.Stop != 2) {
			return c, errors.New("串口参数无效。")
		}
		if d.ReceiveNewline != "" && !oneOf(d.ReceiveNewline, "auto", "crlf", "lf", "cr", "none", "batch") {
			return c, errors.New("接收换行设置无效。")
		}
		if d.Group == "" {
			d.Group = "rtt"
		}
		if !oneOf(d.Group, "rtt", "at", "binary", "custom") {
			return c, errors.New("指令分组无效。")
		}
		if d.Mode != "" && !oneOf(d.Mode, "text", "hex", "mixed") {
			return c, errors.New("显示模式无效。")
		}
		if d.LogFontSize != "" && !oneOf(d.LogFontSize, "12", "13", "14", "16", "18", "20", "22", "24") {
			return c, errors.New("字号无效。")
		}
		if len(d.CustomBaud) > 16 || len(d.Interval) > 16 || len(d.History) > 100 || !validDraft(d.Draft) {
			return c, errors.New("草稿或历史无效。")
		}
		for _, p := range d.History {
			if !validDraft(p.Payload) || len(p.Time) > 64 {
				return c, errors.New("历史记录无效。")
			}
		}
		if c.UI.PrivacyVersion != 1 {
			d.Capture = false
		}
	}
	for _, g := range []string{"rtt", "at", "binary", "custom"} {
		list, ok := c.Commands[g]
		if !ok || list == nil || len(list) > 200 {
			return c, errors.New("指令分组无效。")
		}
		for _, p := range list {
			if len(p.ID) > 128 || len(p.Name) > 320 || !validDraft(p.Payload) {
				return c, errors.New("快捷指令无效。")
			}
		}
	}
	if c.ActiveID != "" && !ids[c.ActiveID] {
		return c, errors.New("活动会话无效。")
	}
	if c.UI.PrivacyVersion != 1 {
		c.UI.RememberInput = false
		c.UI.AutoUpdate = false
	}
	c.UI.PrivacyVersion = 1
	return c, nil
}
func ValidateConfig(data []byte) error { _, e := DecodeConfig(data); return e }
func ConfigBytes(data []byte, sharing bool) ([]byte, error) {
	c, e := DecodeConfig(data)
	if e != nil {
		return nil, e
	}
	if sharing || !c.UI.RememberInput {
		for i := range c.Devices {
			c.Devices[i].Draft.Content = ""
			c.Devices[i].History = []HistoryPayload{}
		}
	}
	if sharing {
		c.UI.RememberInput = false
		for i := range c.Devices {
			c.Devices[i].Capture = false
		}
	}
	return json.Marshal(c)
}
func checkJSONKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 12 {
			return errors.New("配置嵌套过深。")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		if delim == '{' {
			seen := map[string]bool{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := key.(string)
				if !ok || s == "" || s[0] < 'a' || s[0] > 'z' || seen[strings.ToLower(s)] || oneOf(s, "__proto__", "prototype", "constructor") {
					return errors.New("配置包含重复或非法字段。")
				}
				seen[strings.ToLower(s)] = true
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		} else if delim == '[' {
			for d.More() {
				if e = walk(depth + 1); e != nil {
					return e
				}
			}
		} else {
			return errors.New("配置格式无效。")
		}
		_, e = d.Token()
		return e
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("配置包含多余内容。")
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
	c, _ := DecodeConfig(data)
	return c, ""
}
func SaveConfig(file string, data []byte) error {
	safe, e := ConfigBytes(data, false)
	if e != nil {
		return e
	}
	configMu.Lock()
	defer configMu.Unlock()
	return AtomicWrite(file, safe)
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
