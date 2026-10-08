package core

import (
	"bytes"
	"encoding/hex"
	"errors"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Payload struct {
	Content  string `json:"content"`
	Mode     string `json:"mode"`
	Encoding string `json:"encoding"`
	Ending   string `json:"ending"`
}

func (p Payload) Bytes() ([]byte, error) {
	if p.Content == "" {
		return nil, errors.New("请输入发送内容。")
	}
	if len(p.Content) > 196608 {
		return nil, errors.New("单次发送上限为 64 KiB。")
	}
	var data []byte
	var err error
	switch p.Mode {
	case "hex":
		data, err = hex.DecodeString(strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, p.Content))
		if err != nil {
			return nil, errors.New("HEX 只能包含完整的 0–9、A–F 字节和空白。")
		}
	case "text":
		switch p.Encoding {
		case "utf-8":
			if !utf8.ValidString(p.Content) {
				return nil, errors.New("UTF-8 文本无效。")
			}
			data = []byte(p.Content)
		case "ascii":
			for _, r := range p.Content {
				if r > 127 {
					return nil, errors.New("ASCII 无法表示中文，请使用 UTF-8 或 GBK。")
				}
			}
			data = []byte(p.Content)
		case "gbk":
			data, _, err = transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(p.Content))
			if err != nil {
				return nil, errors.New("内容包含 GBK 无法表示的字符。")
			}
			back, _, e := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), data)
			if e != nil || !bytes.Equal(back, []byte(p.Content)) {
				return nil, errors.New("内容包含 GBK 无法表示的字符。")
			}
		default:
			return nil, errors.New("不支持的发送编码。")
		}
	default:
		return nil, errors.New("无效的发送格式。")
	}
	switch p.Ending {
	case "none":
	case "cr":
		data = append(data, 13)
	case "lf":
		data = append(data, 10)
	case "crlf":
		data = append(data, 13, 10)
	default:
		return nil, errors.New("无效的结尾符。")
	}
	if len(data) == 0 || len(data) > 65536 {
		return nil, errors.New("单次发送应为 1–65536 字节。")
	}
	return data, nil
}
