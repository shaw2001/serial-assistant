package core

import (
	"bytes"
	"errors"
	"os"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// maxTextImport bounds quick-command import files.
const maxTextImport = 2 * 1024 * 1024

// ReadTextFile reads a small UTF-8 (with or without BOM) or GBK/GB18030 text
// file, as saved by Notepad or Excel ("CSV" in the ANSI code page).
func ReadTextFile(file string) (string, error) {
	info, e := os.Stat(file)
	if e != nil {
		return "", e
	}
	if info.Size() > maxTextImport {
		return "", errors.New("文件不能超过 2 MiB。")
	}
	data, e := os.ReadFile(file)
	if e != nil {
		return "", e
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(data) {
		return string(data), nil
	}
	out, e := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	if e != nil {
		return "", errors.New("无法识别文件编码，请另存为 UTF-8。")
	}
	return string(out), nil
}

// WriteTextFile writes text atomically; CSV gets a UTF-8 BOM so Excel opens
// Chinese correctly.
func WriteTextFile(file, text string, bom bool) error {
	if len(text) > maxTextImport*4 {
		return errors.New("内容过大。")
	}
	data := []byte(text)
	if bom {
		data = append([]byte{0xEF, 0xBB, 0xBF}, data...)
	}
	return AtomicWrite(file, data)
}
