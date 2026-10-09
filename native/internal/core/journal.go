package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type Journal struct {
	mu            sync.Mutex
	queue         chan []byte
	done          chan struct{}
	bytes         int
	closed        bool
	files         []string
	err           error
	written       atomic.Uint64
	dropped       atomic.Uint64
	directory, id string
}

func NewJournal(dir, id string) (*Journal, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := refreshJournalQuota(dir); e != nil {
		return nil, e
	}
	j := &Journal{queue: make(chan []byte, 2048), done: make(chan struct{}), directory: dir, id: id}
	go j.run()
	return j, nil
}
func (j *Journal) Append(r Record) bool {
	data, _ := json.Marshal(r)
	data = append(data, '\n')
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.err != nil || j.bytes+len(data) > 4*1024*1024 {
		j.dropped.Add(uint64(r.Length))
		return false
	}
	select {
	case j.queue <- data:
		j.bytes += len(data)
		return true
	default:
		j.dropped.Add(uint64(r.Length))
		return false
	}
}
func (j *Journal) run() {
	defer close(j.done)
	defer releaseJournalQuota(j.directory)
	var f *os.File
	var w *bufio.Writer
	var total int
	part := 0
	closeFile := func() {
		if w != nil {
			if e := w.Flush(); e != nil {
				j.fail(e)
			}
		}
		if f != nil {
			if e := f.Close(); e != nil {
				j.fail(e)
			}
		}
		w = nil
		f = nil
	}
	defer closeFile()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case data, ok := <-j.queue:
			if !ok {
				return
			}
			j.mu.Lock()
			j.bytes -= len(data)
			failed := j.err != nil
			j.mu.Unlock()
			if failed {
				continue
			}
			if f == nil || total+len(data) > 100*1024*1024 {
				closeFile()
				part++
				name := filepath.Join(j.directory, fmt.Sprintf("%s-%s-%03d.jsonl", time.Now().Format("20060102-150405.000000000"), j.id, part))
				var e error
				f, e = os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if e != nil {
					j.fail(e)
					continue
				}
				w = bufio.NewWriterSize(f, 65536)
				total = 0
				j.mu.Lock()
				j.files = append(j.files, name)
				j.mu.Unlock()
			}
			if e := writeJournalBytes(j.directory, data, w.Write); e != nil {
				j.fail(e)
				continue
			}
			total += len(data)
			j.written.Add(1)
		case <-ticker.C:
			if w != nil {
				if e := w.Flush(); e != nil {
					j.fail(e)
				}
			}
		}
	}
}
func (j *Journal) fail(e error) {
	j.mu.Lock()
	if j.err == nil {
		j.err = e
	}
	j.mu.Unlock()
}
func (j *Journal) Close() {
	j.mu.Lock()
	if !j.closed {
		j.closed = true
		close(j.queue)
	}
	j.mu.Unlock()
	<-j.done
}
func (j *Journal) Stats() (uint64, uint64, error, []string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.written.Load(), j.dropped.Load(), j.err, append([]string{}, j.files...)
}
