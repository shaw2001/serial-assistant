package core

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type ExportOptions struct {
	ID       string   `json:"id"`
	Scope    string   `json:"scope"`
	Format   string   `json:"format"`
	Encoding string   `json:"encoding"`
	IDs      []uint64 `json:"ids"`
}

func (s *Service) Export(file string, o ExportOptions) error {
	if !oneOf(o.Scope, "all", "cache", "visible") || !oneOf(o.Format, "txt", "json", "jsonl") || !oneOf(o.Encoding, "utf-8", "gbk", "ascii") || len(o.IDs) > 10000 {
		return errors.New("日志导出参数无效。")
	}
	records, files, e := s.ExportSnapshot(o.ID, o.Scope, o.IDs)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(file), ".export-*.tmp")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	w := bufio.NewWriterSize(f, 65536)
	snapshot, _ := s.Snapshot(o.ID)
	meta := map[string]any{"type": "metadata", "version": "0.1.1", "session": snapshot, "scope": o.Scope}
	first := true
	if o.Format == "json" {
		data, _ := json.Marshal(meta)
		fmt.Fprintf(w, "%s,\"records\":[\n", strings.TrimSuffix(string(data), "}"))
	} else if o.Format == "jsonl" {
		data, _ := json.Marshal(meta)
		w.Write(data)
		w.WriteByte('\n')
	} else {
		fmt.Fprintln(w, "# 串口助手 v0.1.1 · TX 表示已提交驱动。")
	}
	writeRecord := func(r Record) error {
		if o.Format != "txt" {
			data, _ := json.Marshal(r)
			if o.Format == "json" && !first {
				if _, e = w.WriteString(",\n"); e != nil {
					return e
				}
			}
			first = false
			if _, e = w.Write(data); e != nil {
				return e
			}
			if o.Format == "jsonl" {
				return w.WriteByte('\n')
			}
			return nil
		}
		data, _ := base64.StdEncoding.DecodeString(r.DataBase64)
		text := r.Text
		if r.Direction != "SYS" {
			if o.Encoding == "gbk" {
				data, _, _ = transform.Bytes(simplifiedchinese.GBK.NewDecoder(), data)
			}
			text = string(data)
		}
		_, e := fmt.Fprintf(w, "%s  %s  %s\n", r.Time, r.Direction, strings.ReplaceAll(text, "\x00", "␀"))
		return e
	}
	if o.Scope == "all" {
		for _, name := range files {
			input, e := os.Open(name)
			if e != nil {
				return e
			}
			scan := bufio.NewScanner(input)
			scan.Buffer(make([]byte, 65536), 256*1024)
			for scan.Scan() {
				var r Record
				if e = json.Unmarshal(scan.Bytes(), &r); e != nil {
					input.Close()
					return e
				}
				if e = writeRecord(r); e != nil {
					input.Close()
					return e
				}
			}
			e = scan.Err()
			input.Close()
			if e != nil {
				return e
			}
		}
	} else {
		for _, r := range records {
			if e = writeRecord(r); e != nil {
				return e
			}
		}
	}
	if o.Format == "json" {
		if _, e = io.WriteString(w, "\n]}\n"); e != nil {
			return e
		}
	}
	if e = w.Flush(); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), file)
}

// Freeze disk segments at the export boundary while holding the session operation lock.
// A new segment resumes capture before the potentially long export writes begin.
func (s *Service) ExportSnapshot(id, scope string, ids []uint64) ([]Record, []string, error) {
	x, e := s.get(id)
	if e != nil {
		return nil, nil, e
	}
	if scope != "all" {
		return s.Records(id, scope, ids)
	}
	x.op.Lock()
	defer x.op.Unlock()
	s.mu.Lock()
	old := x.journal
	capture := x.capture
	var next *Journal
	if old != nil && capture {
		next, e = NewJournal(s.directory, id)
		if e != nil {
			s.mu.Unlock()
			return nil, nil, e
		}
	}
	x.journal = next
	s.mu.Unlock()
	if old != nil {
		old.Close()
		s.mu.Lock()
		x.oldJournals = append(x.oldJournals, old)
		s.mu.Unlock()
	}
	s.mu.Lock()
	files := []string{}
	for _, j := range x.oldJournals {
		_, _, err, f := j.Stats()
		if err != nil {
			s.mu.Unlock()
			return nil, nil, err
		}
		files = append(files, f...)
	}
	s.mu.Unlock()
	return nil, files, nil
}
