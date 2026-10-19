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

// AppVersion is written into exported logs; main sets it to the build version.
var AppVersion = "dev"

// ErrNothingToExport is returned instead of writing a file with no records.
var ErrNothingToExport = errors.New("当前会话没有可导出的记录。请先连接并收发数据后再导出。")

// Export writes the session log to file and returns how many records it holds.
// Scope "all" is the complete session: the in-memory full history (every
// record since the session started, kept whether or not 落盘 is on and not
// affected by 清屏) merged with any on-disk 落盘 journal, in sequence order
// without duplicates.
func (s *Service) Export(file string, o ExportOptions) (int, error) {
	if !oneOf(o.Scope, "all", "cache", "visible") || !oneOf(o.Format, "txt", "json", "jsonl") || !oneOf(o.Encoding, "utf-8", "gbk", "ascii") || len(o.IDs) > 10000 {
		return 0, errors.New("日志导出参数无效。")
	}
	var records []Record
	var history []historyEntry
	var files []string
	var historyTrimmed uint64
	var e error
	if o.Scope == "all" {
		history, files, historyTrimmed, e = s.snapshotAll(o.ID)
	} else {
		records, files, e = s.Records(o.ID, o.Scope, o.IDs)
	}
	if e != nil {
		return 0, e
	}
	if o.Scope == "all" && len(files) == 0 && len(history) == 0 {
		return 0, ErrNothingToExport
	}
	if o.Scope != "all" && len(records) == 0 {
		return 0, ErrNothingToExport
	}
	f, e := os.CreateTemp(filepath.Dir(file), ".export-*.tmp")
	if e != nil {
		return 0, e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	w := bufio.NewWriterSize(f, 65536)
	snapshot, _ := s.Snapshot(o.ID)
	meta := map[string]any{"type": "metadata", "version": AppVersion, "session": snapshot, "scope": o.Scope}
	if historyTrimmed > 0 {
		meta["historyTrimmed"] = historyTrimmed
	}
	count := 0
	first := true
	if o.Format == "json" {
		data, _ := json.Marshal(meta)
		fmt.Fprintf(w, "%s,\"records\":[\n", strings.TrimSuffix(string(data), "}"))
	} else if o.Format == "jsonl" {
		data, _ := json.Marshal(meta)
		w.Write(data)
		w.WriteByte('\n')
	} else {
		path := ""
		if m, ok := snapshot.(map[string]any); ok {
			path, _ = m["path"].(string)
		}
		fmt.Fprintf(w, "# 串口助手 v%s · %s\n# 主机记录时间；TX 表示已提交驱动，设备应答请查看 RX。\n", AppVersion, path)
		if historyTrimmed > 0 && len(files) == 0 {
			fmt.Fprintf(w, "# 注意：最早的 %d 条记录超过 %d MiB 内存上限，未保留（开启“落盘”可完整写入磁盘）。\n", historyTrimmed, historyMaxBytes>>20)
		}
	}
	writeRecord := func(r Record) error {
		count++
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
		// history is taken at the same freeze point as the journal files; both
		// are in ascending sequence order.
		next := 0
		flushCacheBefore := func(seq uint64) error {
			for next < len(history) && history[next].rec.Seq < seq {
				if e := writeRecord(history[next].record()); e != nil {
					return e
				}
				next++
			}
			for next < len(history) && history[next].rec.Seq == seq {
				next++ // already written from the journal
			}
			return nil
		}
		for _, name := range files {
			input, e := os.Open(name)
			if e != nil {
				return 0, e
			}
			scan := bufio.NewScanner(input)
			scan.Buffer(make([]byte, 65536), 256*1024)
			for scan.Scan() {
				var r Record
				if e = json.Unmarshal(scan.Bytes(), &r); e != nil {
					input.Close()
					return 0, e
				}
				if e = flushCacheBefore(r.Seq); e != nil {
					input.Close()
					return 0, e
				}
				if e = writeRecord(r); e != nil {
					input.Close()
					return 0, e
				}
			}
			e = scan.Err()
			input.Close()
			if e != nil {
				return 0, e
			}
		}
		if e = flushCacheBefore(^uint64(0)); e != nil {
			return 0, e
		}
	} else {
		for _, r := range records {
			if e = writeRecord(r); e != nil {
				return 0, e
			}
		}
	}
	if o.Format == "json" {
		if _, e = io.WriteString(w, "\n]}\n"); e != nil {
			return 0, e
		}
	}
	if e = w.Flush(); e != nil {
		return 0, e
	}
	if e = f.Sync(); e != nil {
		return 0, e
	}
	if e = f.Close(); e != nil {
		return 0, e
	}
	if count == 0 {
		return 0, ErrNothingToExport
	}
	return count, os.Rename(f.Name(), file)
}

// snapshotAll freezes the 落盘 journal segments at the export boundary while
// holding the session operation lock, and takes the full history at the same
// point. A new segment resumes capture before the long export writes begin.
// The history slice is copied; its entries are immutable.
func (s *Service) snapshotAll(id string) ([]historyEntry, []string, uint64, error) {
	x, e := s.get(id)
	if e != nil {
		return nil, nil, 0, e
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
			return nil, nil, 0, e
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
	history := append([]historyEntry{}, x.history...)
	trimmed := x.historyTrimmed
	files := []string{}
	for _, j := range x.oldJournals {
		_, _, err, f := j.Stats()
		if err != nil {
			s.mu.Unlock()
			return nil, nil, 0, err
		}
		files = append(files, f...)
	}
	s.mu.Unlock()
	return history, files, trimmed, nil
}

// HasExportable reports whether Export would find at least one record, so the
// save dialog is not shown for an empty session.
func (s *Service) HasExportable(id, scope string, ids []uint64) (bool, error) {
	if scope != "all" {
		records, _, e := s.Records(id, scope, ids)
		return len(records) > 0, e
	}
	x, e := s.get(id)
	if e != nil {
		return false, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(x.history) > 0 {
		return true, nil
	}
	for _, j := range append(append([]*Journal{}, x.oldJournals...), x.journal) {
		if j != nil {
			if n, _, _, _ := j.Stats(); n > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}
