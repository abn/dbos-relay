package liveness

import (
	"container/heap"
	"sync"
	"time"
)

// Clock provides time operations, abstracting standard time for testability.
type Clock interface {
	Now() time.Time
	NewTimer(d time.Duration) Timer
	After(d time.Duration) <-chan time.Time
}

// Timer represents an active timer.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// RealClock implements Clock using the standard time package.
type RealClock struct{}

func NewRealClock() *RealClock {
	return &RealClock{}
}

func (r *RealClock) Now() time.Time {
	return time.Now()
}

func (r *RealClock) NewTimer(d time.Duration) Timer {
	t := time.NewTimer(d)
	return &realTimer{t: t}
}

func (r *RealClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

type realTimer struct {
	t *time.Timer
}

func (rt *realTimer) C() <-chan time.Time {
	return rt.t.C
}

func (rt *realTimer) Stop() bool {
	return rt.t.Stop()
}

func (rt *realTimer) Reset(d time.Duration) bool {
	return rt.t.Reset(d)
}

// VirtualClock provides a mock clock for deterministic, zero-sleep testing.
type VirtualClock struct {
	mu     sync.Mutex
	now    time.Time
	timers timerHeap
	nextID uint64
}

// NewVirtualClock creates a VirtualClock initialized to the given time.
func NewVirtualClock(initTime time.Time) *VirtualClock {
	if initTime.IsZero() {
		initTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	vc := &VirtualClock{
		now:    initTime,
		timers: make(timerHeap, 0),
	}
	heap.Init(&vc.timers)
	return vc
}

func (vc *VirtualClock) Now() time.Time {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return vc.now
}

func (vc *VirtualClock) NewTimer(d time.Duration) Timer {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.nextID++
	triggerAt := vc.now.Add(d)
	vt := &virtualTimer{
		vc:        vc,
		id:        vc.nextID,
		triggerAt: triggerAt,
		ch:        make(chan time.Time, 1),
		active:    true,
	}

	if d <= 0 {
		vt.ch <- triggerAt
		vt.active = false
		return vt
	}

	heap.Push(&vc.timers, vt)
	return vt
}

func (vc *VirtualClock) After(d time.Duration) <-chan time.Time {
	return vc.NewTimer(d).C()
}

// Advance moves virtual time forward by duration d, firing expired timers in order.
func (vc *VirtualClock) Advance(d time.Duration) {
	vc.mu.Lock()
	targetTime := vc.now.Add(d)
	vc.now = targetTime

	var expired []*virtualTimer
	for len(vc.timers) > 0 {
		earliest := vc.timers[0]
		if earliest.triggerAt.After(targetTime) {
			break
		}
		heap.Pop(&vc.timers)
		if earliest.active {
			earliest.active = false
			expired = append(expired, earliest)
		}
	}
	vc.mu.Unlock()

	for _, vt := range expired {
		select {
		case vt.ch <- vt.triggerAt:
		default:
		}
	}
}

type virtualTimer struct {
	vc        *VirtualClock
	id        uint64
	triggerAt time.Time
	ch        chan time.Time
	active    bool
	index     int
}

func (vt *virtualTimer) C() <-chan time.Time {
	return vt.ch
}

func (vt *virtualTimer) Stop() bool {
	vt.vc.mu.Lock()
	defer vt.vc.mu.Unlock()

	if !vt.active {
		return false
	}
	vt.active = false
	if vt.index >= 0 && vt.index < len(vt.vc.timers) {
		heap.Remove(&vt.vc.timers, vt.index)
	}
	return true
}

func (vt *virtualTimer) Reset(d time.Duration) bool {
	vt.vc.mu.Lock()
	defer vt.vc.mu.Unlock()

	wasActive := vt.active
	if wasActive && vt.index >= 0 && vt.index < len(vt.vc.timers) {
		heap.Remove(&vt.vc.timers, vt.index)
	}

	vt.triggerAt = vt.vc.now.Add(d)
	vt.active = true
	vt.index = -1

	// Drain channel if any
	select {
	case <-vt.ch:
	default:
	}

	if d <= 0 {
		vt.ch <- vt.triggerAt
		vt.active = false
		return wasActive
	}

	heap.Push(&vt.vc.timers, vt)
	return wasActive
}

type timerHeap []*virtualTimer

func (h timerHeap) Len() int           { return len(h) }
func (h timerHeap) Less(i, j int) bool { return h[i].triggerAt.Before(h[j].triggerAt) }
func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *timerHeap) Push(x any) {
	n := len(*h)
	item := x.(*virtualTimer)
	item.index = n
	*h = append(*h, item)
}

func (h *timerHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}
