package transmit

import (
	"syscall"
	"time"
)

type sysClock struct {
	delay, threshold time.Duration
}

func (s *sysClock) Now() time.Time {
	return Now()
}

func (s *sysClock) Sleep(d time.Duration) {
	if d < s.threshold {
		s.delay += d
		// s.delay = s.threshold
	} else {
		s.delay = d
	}
	if s.delay < s.threshold {
		return
	}
	var sec, nsec time.Duration
	if s.delay > time.Second {
		sec = s.delay.Truncate(time.Second)
		nsec = s.delay - sec
	} else {
		nsec = s.delay
	}
	t := syscall.Timespec{
		Sec:  int32(sec.Seconds()),
		Nsec: int32(nsec.Nanoseconds()),
	}
	syscall.Nanosleep(&t, nil)
	s.delay = 0
}

func guessThreshold() time.Duration {
	// e := sleepAtLeast(time.Millisecond.Nanoseconds())
	// return e.Truncate(time.Millisecond)
	return time.Millisecond
}
