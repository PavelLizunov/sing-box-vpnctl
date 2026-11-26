package exit

import (
	"encoding/binary"
	"sync"
	"time"
)

type udpFlow struct {
	id      uint16
	session string
	peer    string
}

// prependSeq/splitSeq carry the per-flow sequence in a 4-byte prefix on the
// UDP-FEC pipe, so the frame codec (EncodeUDPFrame) stays unchanged.
func prependSeq(seq uint32, frame []byte) []byte {
	out := make([]byte, 4+len(frame))
	binary.BigEndian.PutUint32(out[:4], seq)
	copy(out[4:], frame)
	return out
}

func splitSeq(b []byte) (uint32, []byte, bool) {
	if len(b) < 4 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint32(b[:4]), b[4:], true
}

// Per-flow reordering for the striped UDP path. Datagrams of one flow leave the
// sender in order, but the N-worker relay pipe reorders them; uncorrected, the
// app sees jitter and counts reordered datagrams as loss. The sender stamps a
// per-flow sequence (carried in the UDP frame); the receiver holds an
// out-of-order datagram until the gap ahead of it fills or releaseAfter
// elapses, so a genuinely lost datagram stalls only its own flow, and only
// briefly. This is an independent implementation of a standard reorder buffer.

const (
	defaultReleaseAfter = 12 * time.Millisecond
	defaultMaxPerFlow   = 128
	defaultMaxFlows     = 4096
	flowIdleTimeout     = 30 * time.Second
)

type flowSequencer struct {
	mu     sync.Mutex
	seq    map[udpFlow]sendSequence
	pruned time.Time
}

type sendSequence struct {
	next     uint32
	lastSeen time.Time
}

func newFlowSequencer() *flowSequencer { return &flowSequencer{seq: map[udpFlow]sendSequence{}} }

func (s *flowSequencer) next(flow udpFlow) (uint32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.pruned) >= flowIdleTimeout {
		for key, seq := range s.seq {
			if now.Sub(seq.lastSeen) >= 2*flowIdleTimeout {
				delete(s.seq, key)
			}
		}
		s.pruned = now
	}
	state, ok := s.seq[flow]
	if !ok && len(s.seq) >= defaultMaxFlows {
		return 0, false
	}
	n := state.next
	s.seq[flow] = sendSequence{next: n + 1, lastSeen: now}
	return n, true
}

type reorderFlow struct {
	expected uint32
	gapAt    time.Time
	pending  map[uint32][]byte
	lastSeen time.Time
	deliver  func([]byte)
}

// reorderBuffer reorders datagrams per flow. Concurrency-safe.
type reorderBuffer struct {
	mu           sync.Mutex
	flows        map[udpFlow]*reorderFlow
	releaseAfter time.Duration
	maxPerFlow   int
	maxFlows     int
}

func newReorderBuffer(releaseAfter time.Duration) *reorderBuffer {
	if releaseAfter <= 0 {
		releaseAfter = defaultReleaseAfter
	}
	return &reorderBuffer{
		flows:        map[udpFlow]*reorderFlow{},
		releaseAfter: releaseAfter,
		maxPerFlow:   defaultMaxPerFlow,
		maxFlows:     defaultMaxFlows,
	}
}

// forward reports whether seq is ahead of expected in wrapping u32 space.
func forwardSeq(seq, expected uint32) bool {
	return seq != expected && seq-expected < 1<<31
}

// push ingests one datagram and returns this flow's payloads now deliverable in
// order (possibly none, or several when a datagram fills a gap).
func (r *reorderBuffer) push(flow udpFlow, seq uint32, payload []byte, now time.Time, deliver func([]byte)) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	f := r.flows[flow]
	if f == nil {
		if len(r.flows) >= r.maxFlows {
			r.evictIdleLocked(now)
			if len(r.flows) >= r.maxFlows {
				return nil
			}
		}
		f = &reorderFlow{expected: seq, pending: map[uint32][]byte{}}
		r.flows[flow] = f
	}
	f.lastSeen = now
	f.deliver = deliver

	cp := append([]byte(nil), payload...)

	if seq == f.expected {
		out := [][]byte{cp}
		f.expected++
		out = drainContiguous(f, out)
		if len(f.pending) == 0 {
			f.gapAt = time.Time{}
		}
		return out
	}
	if !forwardSeq(seq, f.expected) {
		return nil // old or duplicate: already delivered or released past
	}
	if _, dup := f.pending[seq]; dup {
		return nil
	}
	f.pending[seq] = cp
	if f.gapAt.IsZero() {
		f.gapAt = now
	}

	var out [][]byte
	if len(f.pending) > r.maxPerFlow {
		out = releaseLowest(f, now, out)
	}
	if !f.gapAt.IsZero() && now.Sub(f.gapAt) >= r.releaseAfter {
		out = releaseLowest(f, now, out)
	}
	return out
}

// flush releases gaps that have waited past releaseAfter and evicts idle flows.
// Call it on a timer so a flow that goes quiet right after a loss still drains.
func (r *reorderBuffer) flush(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, f := range r.flows {
		if !f.gapAt.IsZero() && now.Sub(f.gapAt) >= r.releaseAfter {
			for _, payload := range releaseLowest(f, now, nil) {
				f.deliver(payload)
			}
		}
	}
	r.evictIdleLocked(now)
}

func (r *reorderBuffer) remove(assoc udpFlow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key := range r.flows {
		if key.session == assoc.session && key.id == assoc.id {
			delete(r.flows, key)
		}
	}
}

func (s *flowSequencer) remove(assoc udpFlow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.seq {
		if key.session == assoc.session && key.id == assoc.id {
			delete(s.seq, key)
		}
	}
}

func (r *reorderBuffer) evictIdleLocked(now time.Time) {
	for id, f := range r.flows {
		if len(f.pending) == 0 && now.Sub(f.lastSeen) >= flowIdleTimeout {
			delete(r.flows, id)
		}
	}
}

// drainContiguous delivers pending datagrams that now sit right at expected.
func drainContiguous(f *reorderFlow, out [][]byte) [][]byte {
	for {
		p, ok := f.pending[f.expected]
		if !ok {
			return out
		}
		delete(f.pending, f.expected)
		out = append(out, p)
		f.expected++
	}
}

// releaseLowest gives up waiting for the missing datagram at expected: it
// advances to the lowest buffered sequence, delivers it and any now-contiguous
// run, and re-arms the gap timer if datagrams still wait behind a later hole.
func releaseLowest(f *reorderFlow, now time.Time, out [][]byte) [][]byte {
	var lowest uint32
	found := false
	for s := range f.pending {
		if !found || forwardSeq(lowest, s) {
			lowest, found = s, true
		}
	}
	if !found {
		f.gapAt = time.Time{}
		return out
	}
	p := f.pending[lowest]
	delete(f.pending, lowest)
	f.expected = lowest + 1
	out = append(out, p)
	out = drainContiguous(f, out)
	if len(f.pending) == 0 {
		f.gapAt = time.Time{}
	} else {
		f.gapAt = now
	}
	return out
}
