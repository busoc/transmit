package main

import (
	"bytes"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"time"
	"math/rand"

	"github.com/busoc/transmit"
	"github.com/midbel/cli"
	"golang.org/x/sync/errgroup"
)

func main() {
	var rate cli.Size
	flag.Var(&rate, "r", "rate")
	global := flag.Bool("g", false, "use the same bucket for all connections")
	parallel := flag.Int("p", 4, "parallel")
	count := flag.Int("n", 4, "count")
	size := flag.Int("s", 1024, "size")
	buffer := flag.Int("b", 1024, "size")
	listen := flag.Bool("l", false, "listen mode")
	test := flag.Bool("t", false, "test mode")
	every := flag.Duration("e", 8*time.Millisecond, "every")
	wait := flag.Duration("w", 250*time.Millisecond, "wait")
	flag.Parse()

	var err error
	switch {
	case *test:
		err = runTest(*count, *every)
	case *listen:
		err = runServer(flag.Arg(0), *size)
	default:
		if *count <= 0 {
			*count = 1
		}
		var b *transmit.Bucket
		if r := rate.Int(); r > 0 && *global {
			b = transmit.NewBucket(r, *every)
		}
		var g errgroup.Group
		for i := 0; i < *count; i++ {
			g.Go(func() error {
				var buck = b
				if r := rate.Int(); r > 0 && !*global {
					buck = transmit.NewBucket(r, *every)
				}
				return runClientWithRate(flag.Arg(0), *size, *buffer, *parallel, *wait, buck)
			})
		}
		err = g.Wait()
	}
	if err != nil {
		log.Fatalln(err)
	}
}

func runClientWithRate(a string, z, b, p int, e time.Duration, buck *transmit.Bucket) error {
	defer log.Println("done client")
	cs := make([]net.Conn, p)
	ws := make([]io.Writer, p)

	var as []string
	for i := 0; i < len(cs); i++ {
		c, err := net.Dial("tcp", a)
		if err != nil {
			return err
		}
		defer c.Close()
		as = append(as, c.LocalAddr().String())
		cs[i], ws[i] = c, c
		if buck != nil {
			ws[i] = transmit.Writer(c, buck)
		}
	}
	log.Printf("start client (%v)", as)
	var curr uint16

	bs := make([]byte, z)
	for {
		z := rand.Intn(len(bs))
		if z == 0 {
			continue
		}
		time.Sleep(e)
		var g errgroup.Group
		buf := bytes.NewBuffer(bs[:z])
		for buf.Len() > 0 {
			curr++
			j := int(curr) % p
			g.Go(copyBuffer(buf.Next(b), ws[j]))
		}
		if err := g.Wait(); err != nil {
			log.Println("exiting client", err)
			return err
		}
	}
	return nil
}

func runTest(c int, e time.Duration) error {
	k := transmit.SystemClock()
	a := k.Now()
	for i := 0; c <= 0 || i < c; i++ {
		k.Sleep(e)
	}
	b := k.Now()
	log.Printf("%s - %s - %s", a, b, b.Sub(a))
	return nil
}

func runServer(a string, z int) error {
	s, err := net.Listen("tcp", a)
	if err != nil {
		return err
	}
	defer s.Close()
	for {
		c, err := s.Accept()
		if err != nil {
			return err
		}
		go func(r net.Conn) {
			defer r.Close()

			var bs []byte
			if z > 0 {
				bs = make([]byte, z)
			}

			var total float64
			w := time.Now()

			defer func() {
				d := time.Since(w)
				t := total / (1024 * 1024)
				log.Printf("done with %s: %.2fMB (%.2fMBs)", c.RemoteAddr(), t, t/d.Seconds())
			}()
			for {
				r.SetReadDeadline(time.Now().Add(time.Second))
				c, err := io.CopyBuffer(ioutil.Discard, r, bs)
				if err, ok := err.(net.Error); ok && err.Timeout() {
					total += float64(c)
					offset := time.Since(w)
					t := total / (1024 * 1024)
					log.Printf("%.2fMB | %.2fMB | %.2fMB/s | %s", float64(c)/(1024*1024), t, t/offset.Seconds(), offset)
				} else {
					return
				}
			}
		}(c)
	}
}

func copyBuffer(bs []byte, c io.Writer) func() error {
	return func() error {
		_, err := c.Write(bs)
		return err
	}
}
