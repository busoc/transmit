package transmit

import (
	"io"
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
	available chan int64
	done      chan struct{}
}

func NewBucket(n int64, e time.Duration) *Bucket {
	b := &Bucket{
		available: make(chan int64),
		done:      make(chan struct{}),
	}
	go b.refill(n, e)
	return b
}

func (b *Bucket) Take(n int64) {
	b.available <- n
	<-b.done
}

func (b *Bucket) refill(limit int64, every time.Duration) {
	queue := make(chan int64)
	go func() {
		for {
			ns := sleepAtLeast(every.Nanoseconds())
			queue <- (limit * int64(ns/time.Millisecond)) / 1000
		}
	}()
	available, empty := limit, struct{}{}
	for {
		select {
		case n := <-queue:
			if d := available + n; d > limit {
				available = limit
			} else {
				available = d
			}
		case n := <-b.available:
			d := available - n
			if d >= 0 {
				available = d
			} else {
				c := <-queue
				if d := available - n + c; d > limit {
					available = limit
				} else {
					available = d
				}
			}
			b.done <- empty
		}
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

func sleepAtLeast(ns int64) time.Duration {
	b := now()
	i := unix.NsecToTimespec(ns)
	unix.Nanosleep(&i, nil)
	a := now()

	return time.Duration(a.Nano() - b.Nano())
}
