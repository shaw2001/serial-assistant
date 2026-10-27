package core

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// statsMock adds ClearCommError-style status and queue sizes to mockPort.
type statsMock struct {
	*mockPort
	smu  sync.Mutex
	errs []uint32
	inQ  uint32
}

func (p *statsMock) CommStatus() (uint32, uint32, error) {
	p.smu.Lock()
	defer p.smu.Unlock()
	var e uint32
	if len(p.errs) > 0 {
		e, p.errs = p.errs[0], p.errs[1:]
	}
	return e, p.inQ, nil
}
func (p *statsMock) QueueSizes() (uint32, uint32) { return 1 << 20, 64 << 10 }

func sysTexts(s *Service, id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for _, h := range s.sessions[id].history {
		if h.rec.Direction == "SYS" {
			out = append(out, h.rec.Text)
		}
	}
	return out
}

func TestDriverOverrunsAreCountedNoticedAndShownInSnapshot(t *testing.T) {
	old := commNoticeEvery
	commNoticeEvery = 0
	defer func() { commNoticeEvery = old }()
	p := &statsMock{mockPort: newMock(), errs: []uint32{ceRxOver, ceOverrun | ceFrame, ceRxParity}, inQ: 3000}
	s := NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	defer s.Shutdown()
	if _, e := s.Open("session-one", opts("COM1")); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		p.input <- []byte("x")
	}
	wait(t, func() bool {
		v, _ := s.Snapshot("session-one")
		m := v.(map[string]any)
		return m["overruns"] == uint64(2) && m["frameErrors"] == uint64(1) && m["parityErrors"] == uint64(1)
	})
	v, _ := s.Snapshot("session-one")
	m := v.(map[string]any)
	if m["inQueueMax"] != uint32(3000) || m["rxQueue"] != uint32(1<<20) || m["txQueue"] != uint32(64<<10) {
		t.Fatalf("queue stats: %v %v %v", m["inQueueMax"], m["rxQueue"], m["txQueue"])
	}
	all := strings.Join(sysTexts(s, "session-one"), "\n")
	for _, want := range []string{"驱动接收缓冲 1024 KiB", "驱动缓冲溢出，可能丢失数据", "硬件 FIFO 溢出", "帧错误", "校验错误"} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in SYS log:\n%s", want, all)
		}
	}
}

func TestDriverErrorNoticesAreRateLimitedButAllCounted(t *testing.T) {
	old := commNoticeEvery
	commNoticeEvery = time.Hour
	defer func() { commNoticeEvery = old }()
	p := &statsMock{mockPort: newMock(), errs: []uint32{ceRxOver, ceRxOver, ceRxOver, ceRxOver}}
	s := NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	defer s.Shutdown()
	s.Open("session-one", opts("COM1"))
	for i := 0; i < 4; i++ {
		p.input <- []byte("y")
	}
	wait(t, func() bool { v, _ := s.Snapshot("session-one"); return v.(map[string]any)["overruns"] == uint64(4) })
	n := 0
	for _, x := range sysTexts(s, "session-one") {
		if strings.Contains(x, "溢出") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want one notice per interval, got %d", n)
	}
}

// The reader must keep draining the port while the service lock is held
// (export, batch, other sessions), and nothing may be lost or reordered.
func TestReaderKeepsReadingWhileServiceIsLocked(t *testing.T) {
	p := newMock()
	s := NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	defer s.Shutdown()
	s.Open("session-one", opts("COM1"))
	s.mu.Lock()
	var want []byte
	for i := 0; i < 50; i++ {
		chunk := bytes.Repeat([]byte{byte('a' + i%26)}, 100+i)
		want = append(want, chunk...)
		p.input <- chunk
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(p.input) > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	drained := len(p.input) == 0
	s.mu.Unlock()
	if !drained {
		t.Fatal("reader stopped reading while the service lock was held")
	}
	wait(t, func() bool { v, _ := s.Snapshot("session-one"); return v.(map[string]any)["rx"] == uint64(len(want)) })
	s.mu.Lock()
	var got []byte
	for _, h := range s.sessions["session-one"].history {
		if h.rec.Direction == "RX" {
			got = append(got, h.data...)
		}
	}
	s.mu.Unlock()
	if !bytes.Equal(got, want) {
		t.Fatal("RX bytes lost or reordered")
	}
}

// Export snapshots are O(1) and stay intact while new data arrives and old
// history is trimmed.
func TestExportSnapshotIsStableWhileReceivingAndTrimming(t *testing.T) {
	old := historyMaxBytes
	historyMaxBytes = 64 * 1024
	defer func() { historyMaxBytes = old }()
	p := newMock()
	s := NewService(t.TempDir(), func(Options) (Port, error) { return p, nil })
	defer s.Shutdown()
	s.Open("session-one", opts("COM1"))
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case p.input <- bytes.Repeat([]byte{byte(i)}, 300):
			}
		}
	}()
	for k := 0; k < 50; k++ {
		h, _, _, e := s.snapshotAll("session-one")
		if e != nil {
			t.Fatal(e)
		}
		for i := 1; i < len(h); i++ {
			if h[i].rec.Seq <= h[i-1].rec.Seq {
				t.Fatal("snapshot order broken")
			}
		}
		for _, x := range h {
			if x.rec.Direction == "RX" && (len(x.data) == 0 || x.rec.Length != len(x.data)) {
				t.Fatal("snapshot entry was modified")
			}
		}
		time.Sleep(time.Millisecond)
	}
	close(stop)
	wg.Wait()
}
