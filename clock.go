package transmit

import (
	"io"
	"sync"
	"syscall"
	"time"
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

	wait chan struct{}

	mu        sync.Mutex
	available int64
}

func NewBucket(n int64, e time.Duration) *Bucket {
	b := &Bucket{capacity: n, available: n, wait: make(chan struct{})}
	go b.refill(e)
	return b
}

func (b *Bucket) Take(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for {
		if d := b.available - n; b.available > 0 && d >= n {
			b.available = d
			break
		}
		<-b.wait
	}
}

func (b *Bucket) refill(e time.Duration) {
	c := float64(b.capacity*int64(e/time.Millisecond)) / 1000
	c *= 1.01

	ns := e.Nanoseconds()
	sleep := func() {
		i := syscall.NsecToTimespec(ns)
		syscall.Nanosleep(&i, nil)
	}

	for {
		// time.Sleep(e)
		sleep()
		if b.available > b.capacity {
			continue
		}
		b.available = b.available + int64(c)
		select {
		case b.wait <- struct{}{}:
		default:
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

func now() time.Time {
	var t syscall.Timeval
	if err := syscall.Gettimeofday(&t); err != nil {
		return time.Now()
	}
	return time.Unix(int64(t.Sec), int64(t.Usec*1000))
}
