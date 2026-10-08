package core

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"
)

var idPattern = regexp.MustCompile(`^session-[a-zA-Z0-9-]{1,64}$`)
var pathPattern = regexp.MustCompile(`(?i)^(COM[0-9]+|\\\\\.\\COM[0-9]+|/dev/[a-zA-Z0-9_./-]+)$`)

type Options struct {
	Path     string  `json:"path"`
	BaudRate int     `json:"baudRate"`
	DataBits int     `json:"dataBits"`
	Parity   string  `json:"parity"`
	StopBits float64 `json:"stopBits"`
	Flow     string  `json:"flow"`
	Capture  bool    `json:"capture"`
}

func (o Options) Validate() error {
	if len(o.Path) > 256 || strings.Contains(o.Path, "..") || !pathPattern.MatchString(o.Path) {
		return errors.New("请输入有效的串口名称，如 COM5。")
	}
	if o.BaudRate < 1 || o.BaudRate > 12000000 {
		return errors.New("波特率应为 1–12000000 的整数。")
	}
	if o.DataBits < 5 || o.DataBits > 8 || !oneOf(o.Parity, "none", "even", "odd", "mark", "space") || !oneOf(o.Flow, "none", "rtscts", "xonxoff") || (o.StopBits != 1 && o.StopBits != 1.5 && o.StopBits != 2) {
		return errors.New("串口参数无效。")
	}
	return nil
}

type Port interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Drain() error
	Close() error
}
type Record struct {
	Type           string `json:"type"`
	Seq            uint64 `json:"seq"`
	Time           string `json:"time"`
	TimezoneOffset int    `json:"timezoneOffset"`
	MonotonicMs    int64  `json:"monotonicMs"`
	Direction      string `json:"direction"`
	Length         int    `json:"length"`
	DataBase64     string `json:"dataBase64"`
	Text           string `json:"text,omitempty"`
}
type Event struct {
	Type   string  `json:"type"`
	ID     string  `json:"id"`
	Record *Record `json:"record,omitempty"`
	Text   string  `json:"text,omitempty"`
}
type periodic struct {
	interval, count, skipped int
	cancel                   chan struct{}
}
type session struct {
	op                                        sync.Mutex
	send                                      sync.Mutex
	id, status                                string
	options                                   Options
	port                                      Port
	generation                                uint64
	records                                   []Record
	cacheBytes                                int
	rx, tx, seq, errors, trimmed, viewDropped uint64
	started                                   time.Time
	journal                                   *Journal
	oldJournals                               []*Journal
	periodic                                  *periodic
	capture                                   bool
}
type Service struct {
	mu         sync.Mutex
	sessions   map[string]*session
	events     []Event
	eventBytes int
	directory  string
	OpenPort   func(Options) (Port, error)
}

func NewService(dir string, open func(Options) (Port, error)) *Service {
	return &Service{sessions: map[string]*session{}, events: []Event{}, directory: dir, OpenPort: open}
}
func (s *Service) get(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.sessions[id]
	if x == nil {
		return nil, errors.New("串口会话不存在。")
	}
	return x, nil
}
func (s *Service) emitLocked(e Event) {
	n := 128
	if e.Record != nil {
		n += e.Record.Length
	}
	if s.eventBytes+n > 4*1024*1024 || len(s.events) >= 20000 {
		if x := s.sessions[e.ID]; x != nil && e.Record != nil {
			x.viewDropped += uint64(e.Record.Length)
		}
		return
	}
	s.events = append(s.events, e)
	s.eventBytes += n
}
func (s *Service) recordLocked(x *session, direction string, data []byte, text string) {
	x.seq++
	now := time.Now()
	_, zone := now.Zone()
	r := Record{Type: "record", Seq: x.seq, Time: now.Format(time.RFC3339Nano), TimezoneOffset: zone / 60, MonotonicMs: time.Since(x.started).Milliseconds(), Direction: direction, Length: len(data), DataBase64: base64.StdEncoding.EncodeToString(data), Text: text}
	x.records = append(x.records, r)
	x.cacheBytes += len(data) + len(text) + 256
	for len(x.records) > 10000 || x.cacheBytes > 16*1024*1024 {
		a := x.records[0]
		x.records = x.records[1:]
		x.cacheBytes -= a.Length + len(a.Text) + 256
		x.trimmed++
	}
	if direction == "RX" {
		x.rx += uint64(len(data))
	}
	if direction == "TX" {
		x.tx += uint64(len(data))
	}
	if x.capture && x.journal != nil {
		x.journal.Append(r)
	}
	s.emitLocked(Event{Type: "record", ID: x.id, Record: &r})
}
func (s *Service) snapshotLocked(x *session) map[string]any {
	recorded, dropped := uint64(0), uint64(0)
	files := []string{}
	for _, j := range append(append([]*Journal{}, x.oldJournals...), x.journal) {
		if j == nil {
			continue
		}
		n, d, e, f := j.Stats()
		recorded += n
		dropped += d
		files = append(files, f...)
		if e != nil && j == x.journal && x.capture {
			x.capture = false
			x.errors++
			s.emitLocked(Event{Type: "warning", ID: x.id, Text: "会话记录失败：" + e.Error()})
		}
	}
	var p any
	if x.periodic != nil {
		p = map[string]any{"interval": x.periodic.interval, "count": x.periodic.count, "skipped": x.periodic.skipped}
	}
	return map[string]any{"id": x.id, "path": x.options.Path, "baudRate": x.options.BaudRate, "config": x.options, "status": x.status, "rx": x.rx, "tx": x.tx, "errors": x.errors, "trimmed": x.trimmed, "viewDropped": x.viewDropped, "captureDropped": dropped, "capture": x.capture, "recorded": recorded, "logFiles": files, "periodic": p}
}
func (s *Service) Snapshot(id string) (any, error) {
	x, e := s.get(id)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked(x), nil
}
func (s *Service) Batch() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	states := []any{}
	for _, x := range s.sessions {
		states = append(states, s.snapshotLocked(x))
	}
	events := s.events
	s.events = []Event{}
	s.eventBytes = 0
	return map[string]any{"events": events, "states": states}
}
func (s *Service) Open(id string, o Options) (any, error) {
	if !idPattern.MatchString(id) {
		return nil, errors.New("无效的会话标识。")
	}
	if e := o.Validate(); e != nil {
		return nil, e
	}
	s.mu.Lock()
	x := s.sessions[id]
	if x == nil {
		if len(s.sessions) >= 4 {
			s.mu.Unlock()
			return nil, errors.New("最多支持 4 个串口会话。")
		}
		x = &session{id: id, status: "disconnected", started: time.Now(), records: []Record{}}
		s.sessions[id] = x
	}
	s.mu.Unlock()
	x.op.Lock()
	defer x.op.Unlock()
	s.mu.Lock()
	if x.status != "disconnected" && x.status != "error" {
		s.mu.Unlock()
		return nil, errors.New("串口正在使用。")
	}
	for _, a := range s.sessions {
		if a != x && strings.EqualFold(a.options.Path, o.Path) && oneOf(a.status, "opening", "connected", "closing") {
			s.mu.Unlock()
			return nil, errors.New("串口已在另一个会话中打开。")
		}
	}
	oldPort := x.port
	x.port = nil
	x.options = o
	x.status = "opening"
	x.capture = o.Capture
	x.generation++
	generation := x.generation
	s.mu.Unlock()
	if oldPort != nil {
		_ = oldPort.Close()
	}
	s.finishJournal(x)
	if o.Capture {
		j, e := NewJournal(s.directory, id)
		s.mu.Lock()
		if e != nil {
			x.capture = false
			x.errors++
			s.emitLocked(Event{Type: "warning", ID: id, Text: "会话记录失败：" + e.Error()})
		} else {
			x.journal = j
		}
		s.mu.Unlock()
	}
	port, e := s.OpenPort(o)
	s.mu.Lock()
	if e != nil {
		x.status = "disconnected"
		x.errors++
		s.recordLocked(x, "SYS", nil, "连接失败："+e.Error())
		s.mu.Unlock()
		s.finishJournal(x)
		return nil, e
	}
	x.port = port
	x.status = "connected"
	s.recordLocked(x, "SYS", nil, fmt.Sprintf("已连接 %s · %d bps", o.Path, o.BaudRate))
	result := s.snapshotLocked(x)
	s.mu.Unlock()
	go s.read(x, port, generation)
	return result, nil
}
func (s *Service) read(x *session, p Port, generation uint64) {
	buf := make([]byte, 8192)
	for {
		n, e := p.Read(buf)
		s.mu.Lock()
		if x.generation != generation || x.status != "connected" {
			s.mu.Unlock()
			return
		}
		if n > 0 {
			s.recordLocked(x, "RX", buf[:n], "")
		}
		if e != nil {
			s.stopPeriodicLocked(x)
			x.status = "error"
			x.generation++
			x.errors++
			s.recordLocked(x, "SYS", nil, "设备断开："+e.Error())
			s.emitLocked(Event{Type: "warning", ID: x.id, Text: e.Error()})
			s.mu.Unlock()
			go func() {
				x.op.Lock()
				defer x.op.Unlock()
				s.mu.Lock()
				same := x.port == p
				if same {
					x.port = nil
				}
				s.mu.Unlock()
				p.Close()
				if same {
					s.finishJournal(x)
				}
			}()
			return
		}
		s.mu.Unlock()
		if n == 0 {
			time.Sleep(time.Millisecond)
		}
	}
}
func (s *Service) finishJournal(x *session) {
	s.mu.Lock()
	j := x.journal
	x.journal = nil
	s.mu.Unlock()
	if j != nil {
		j.Close()
		s.mu.Lock()
		x.oldJournals = append(x.oldJournals, j)
		s.mu.Unlock()
	}
}
func (s *Service) stopPeriodicLocked(x *session) {
	if x.periodic != nil {
		close(x.periodic.cancel)
		x.periodic = nil
	}
}
func (s *Service) Close(id string) (any, error) {
	x, e := s.get(id)
	if e != nil {
		return nil, nil
	}
	x.op.Lock()
	defer x.op.Unlock()
	s.mu.Lock()
	s.stopPeriodicLocked(x)
	x.status = "closing"
	x.generation++
	p := x.port
	x.port = nil
	s.mu.Unlock()
	if p != nil {
		_ = p.Close()
	}
	x.send.Lock()
	x.send.Unlock()
	s.mu.Lock()
	x.status = "disconnected"
	s.recordLocked(x, "SYS", nil, "串口已断开，周期发送已停止。")
	s.mu.Unlock()
	s.finishJournal(x)
	return s.Snapshot(id)
}
func (s *Service) Remove(id string) error {
	_, e := s.Close(id)
	if e != nil {
		return e
	}
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	return nil
}
func (s *Service) Send(id string, payload Payload) (any, error) {
	data, e := payload.Bytes()
	if e != nil {
		return nil, e
	}
	x, e := s.get(id)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	p, gen := x.port, x.generation
	connected := x.status == "connected"
	s.mu.Unlock()
	if !connected || p == nil {
		return nil, errors.New("请先连接当前串口。")
	}
	if !x.send.TryLock() {
		return nil, errors.New("串口正在发送，请稍后重试。")
	}
	defer x.send.Unlock()
	s.mu.Lock()
	valid := x.generation == gen && x.status == "connected"
	s.mu.Unlock()
	if !valid {
		return nil, errors.New("串口已关闭。")
	}
	remaining := data
	for len(remaining) > 0 {
		var n int
		n, e = p.Write(remaining)
		if e != nil {
			break
		}
		if n == 0 {
			e = io.ErrShortWrite
			break
		}
		remaining = remaining[n:]
	}
	if e == nil {
		e = p.Drain()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if x.generation != gen || x.status != "connected" {
		return nil, errors.New("设备已断开，发送未确认。")
	}
	if e != nil {
		x.errors++
		s.stopPeriodicLocked(x)
		s.recordLocked(x, "SYS", nil, "发送失败："+e.Error())
		return nil, e
	}
	s.recordLocked(x, "TX", data, "")
	return map[string]any{"length": len(data), "status": "submitted"}, nil
}
func (s *Service) StartPeriodic(id string, payload Payload, interval int) (any, error) {
	if interval < 10 || interval > 3600000 {
		return nil, errors.New("间隔应为 10–3600000 ms。")
	}
	if _, e := payload.Bytes(); e != nil {
		return nil, e
	}
	x, e := s.get(id)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	if x.status != "connected" {
		s.mu.Unlock()
		return nil, errors.New("请先连接当前串口。")
	}
	s.stopPeriodicLocked(x)
	p := &periodic{interval: interval, cancel: make(chan struct{})}
	x.periodic = p
	s.recordLocked(x, "SYS", nil, fmt.Sprintf("周期发送开始 · %d ms", interval))
	s.mu.Unlock()
	go func() {
		ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
		defer ticker.Stop()
		next := time.Now()
		for {
			select {
			case <-p.cancel:
				return
			default:
			}
			s.mu.Lock()
			if x.periodic != p || x.status != "connected" {
				s.mu.Unlock()
				return
			}
			now := time.Now()
			if now.After(next) {
				p.skipped += int(now.Sub(next) / (time.Duration(interval) * time.Millisecond))
			}
			next = now.Add(time.Duration(interval) * time.Millisecond)
			s.mu.Unlock()
			_, err := s.Send(id, payload)
			s.mu.Lock()
			if x.periodic == p {
				if err == nil {
					p.count++
				} else if strings.Contains(err.Error(), "正在发送") {
					p.skipped++
				} else {
					s.stopPeriodicLocked(x)
					s.emitLocked(Event{Type: "warning", ID: id, Text: "周期发送已停止：" + err.Error()})
				}
			}
			s.mu.Unlock()
			select {
			case <-p.cancel:
				return
			case <-ticker.C:
			}
		}
	}()
	return s.Snapshot(id)
}
func (s *Service) StopPeriodic(id string) error {
	x, e := s.get(id)
	if e != nil {
		return e
	}
	s.mu.Lock()
	s.stopPeriodicLocked(x)
	s.mu.Unlock()
	return nil
}
func (s *Service) Capture(id string, on bool) (any, error) {
	x, e := s.get(id)
	if e != nil {
		return nil, e
	}
	x.op.Lock()
	defer x.op.Unlock()
	s.mu.Lock()
	needs := on && x.journal == nil
	s.mu.Unlock()
	if needs {
		j, e := NewJournal(s.directory, id)
		if e != nil {
			return nil, e
		}
		s.mu.Lock()
		x.journal = j
		s.mu.Unlock()
	}
	s.mu.Lock()
	x.capture = on
	s.mu.Unlock()
	return s.Snapshot(id)
}
func (s *Service) Clear(id string) (any, error) {
	x, e := s.get(id)
	if e != nil {
		return nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	x.records = []Record{}
	x.cacheBytes = 0
	return map[string]any{"cutoff": x.seq}, nil
}
func (s *Service) Records(id, scope string, ids []uint64) ([]Record, []string, error) {
	x, e := s.get(id)
	if e != nil {
		return nil, nil, e
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := append([]Record{}, x.records...)
	files := []string{}
	for _, j := range append(append([]*Journal{}, x.oldJournals...), x.journal) {
		if j != nil {
			_, _, _, f := j.Stats()
			files = append(files, f...)
		}
	}
	if scope == "visible" {
		set := map[uint64]bool{}
		for _, i := range ids {
			set[i] = true
		}
		out := []Record{}
		for _, r := range records {
			if set[r.Seq] {
				out = append(out, r)
			}
		}
		records = out
	}
	return records, files, nil
}
func (s *Service) Shutdown() {
	s.mu.Lock()
	ids := []string{}
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) { defer wg.Done(); s.Close(id) }(id)
	}
	wg.Wait()
}
