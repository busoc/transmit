package transmit

import (
	"io"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type writer struct {
	inner  io.Writer
	bucket *Bucket
}

func Writer(w io.Writer, b *Bucket) io.Writer {
	if b == nil {
		return w
	}
	return &writer{w, b}
}

func (w *writer) Write(bs []byte) (int, error) {
	w.bucket.Take(int64(len(bs)))
	return w.inner.Write(bs)
}

type Bucket struct {
	capacity int64

	mu        sync.Mutex
	interval  time.Duration
	available int64
}

func NewBucket(n int64, e time.Duration) *Bucket {
	b := &Bucket{
		capacity:  n,
		available: n,
		interval:  e,
	}
	return b
}

func (b *Bucket) Take(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for {
		d := b.available - n
		if d > 0 {
			b.available = d
			break
		}
		b.wait()
	}
}

func (b *Bucket) wait() {
	ns := sleepAtLeast(b.interval.Nanoseconds())
	if b.available > b.capacity {
		return
	}
	c := float64(b.capacity*ns) / 1000
	d := b.available + int64(c*1.1)
	if d > b.capacity {
		b.available = b.capacity
	} else {
		b.available = d
	}
}

type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

func SystemClock() Clock {
	return &sysClock{threshold: guessThreshold()}
}

func RealClock() Clock {
	return &realClock{}
}

type realClock struct{}

func (r realClock) Now() time.Time {
	return time.Now()
}

func (r realClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

func Now() time.Time {
	t := now()
	if t == nil {
		return time.Now()
	}
	return time.Unix(int64(t.Sec), int64(t.Nsec))
}

func now() *unix.Timespec {
	var t unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_REALTIME, &t); err != nil {
		return nil
	}
	return &t
}

func sleepAtLeast(ns int64) int64 {
	b := now()
	i := unix.NsecToTimespec(ns)
	unix.Nanosleep(&i, nil)
	a := now()

	return (a.Nano() - b.Nano()) / 1e6
}
