package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxJournalBytes int64 = 1024 * 1024 * 1024

var journalQuota = struct {
	sync.Mutex
	used   map[string]int64
	active map[string]int
}{used: map[string]int64{}, active: map[string]int{}}

func refreshJournalQuota(dir string) error {
	journalQuota.Lock()
	defer journalQuota.Unlock()
	entries, e := os.ReadDir(dir)
	if e != nil {
		return e
	}
	var total int64
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		total += info.Size()
	}
	key := filepath.Clean(dir)
	if journalQuota.active[key] == 0 || total > journalQuota.used[key] {
		journalQuota.used[key] = total
	}
	journalQuota.active[key]++
	return nil
}
func writeJournalBytes(dir string, data []byte, write func([]byte) (int, error)) error {
	journalQuota.Lock()
	defer journalQuota.Unlock()
	key := filepath.Clean(dir)
	if journalQuota.used[key]+int64(len(data)) > maxJournalBytes {
		return errors.New("原始日志已达到 1 GiB 总上限。请在日志目录归档或删除旧文件后重新开启记录。")
	}
	n, e := write(data)
	journalQuota.used[key] += int64(n)
	return e
}

func releaseJournalQuota(dir string) {
	journalQuota.Lock()
	defer journalQuota.Unlock()
	journalQuota.active[filepath.Clean(dir)]--
}
