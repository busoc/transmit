package main

import (
	"flag"
	"fmt"
	"hash/adler32"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path"
	"time"
)

type Counter interface {
	Stats() (uint64, uint64, uint64)
}

func main() {
	flag.Usage = func() {
		fmt.Printf("%s [-p] [-v] [-sleep-tx] [-sleep-rx] <src> <dst,...>", path.Base(os.Args[0]))
		os.Exit(2)
	}
	rxsleep := flag.Duration("sleep-rx", 0, "sleep recv side")
	txsleep := flag.Duration("sleep-tx", 0, "sleep send side")
	proto := flag.String("p", "udp", "protocol")
	verbose := flag.Bool("v", false, "verbose")
	flag.Parse()
	if flag.NArg() <= 1 {
		flag.Usage()
	}

	r, err := Listen(flag.Arg(0))
	if err != nil {
		log.Fatalf("fail to listen to %s: %s", flag.Arg(0), err)
	}
	if *verbose {
		r = Log(r, *rxsleep)
	}
	defer r.Close()

	var ws []io.Writer
	for i := 1; i < flag.NArg(); i++ {
		scheme, addr := *proto, flag.Arg(i)
		if u, err := url.Parse(flag.Arg(i)); err == nil {
			scheme, addr = u.Scheme, u.Host
		}
		c, err := net.Dial(scheme, addr)
		if err != nil {
			log.Fatalf("fail to connect to %s (%s): %s", flag.Arg(i), *proto, err)
		}
		if *verbose {
			c = Log(c, *txsleep)
		}
		defer c.Close()
		ws = append(ws, c)
	}
	if err := duplicate(r, ws...); err != nil {
		log.Fatalln(err)
	}
}

func duplicate(r io.Reader, ws ...io.Writer) error {
	now := time.Now()
	n, err := io.Copy(io.MultiWriter(ws...), r)
	if err != nil {
		return err
	}
	log.Printf("%dMBytes duplicated (%s)", n>>20, time.Since(now))
	if c, ok := r.(Counter); ok {
		t, s, e := c.Stats()
		log.Printf("%d packets recv from %s (%dMB) with %d error(s)", t, flag.Arg(0), s>>20, e)
	}
	for i := 0; i < len(ws); i++ {
		if c, ok := ws[i].(Counter); ok {
			t, s, e := c.Stats()
			log.Printf("%d packets send to %s (%dMB) with %d error(s)", t, flag.Arg(i+1), s>>20, e)
		}
	}
	return nil
}

type conn struct {
	net.Conn

	count, size, errcount uint64

	sleep  time.Duration
	logger *log.Logger
}

func (c *conn) Stats() (uint64, uint64, uint64) {
	return c.count, c.size, c.errcount
}

func (c *conn) Read(bs []byte) (int, error) {
	if c.sleep > 0 {
		time.Sleep(c.sleep)
	}
	n, err := c.Conn.Read(bs)
	msg := "ok"
	if err != nil {
		c.errcount++
		msg = err.Error()
	} else {
		c.count++
		c.size += uint64(n)
	}
	c.logger.Printf("recv %d bytes (%x): %s", n, adler32.Checksum(bs[:n]), msg)
	return n, err
}

func (c *conn) Write(bs []byte) (int, error) {
	if c.sleep > 0 {
		time.Sleep(c.sleep)
	}
	n, err := c.Conn.Write(bs)
	msg := "ok"
	if err != nil {
		c.errcount++
		msg = err.Error()
	} else {
		c.count++
		c.size += uint64(n)
	}
	c.logger.Printf("send %d/%d bytes (%x): %s", n, len(bs), adler32.Checksum(bs[:n]), msg)
	return n, err
}

func Listen(a string) (net.Conn, error) {
	addr, err := net.ResolveUDPAddr("udp", a)
	if err != nil {
		return nil, err
	}
	var conn *net.UDPConn
	if addr.IP.IsMulticast() {
		conn, err = net.ListenMulticastUDP("udp", nil, addr)
	} else {
		conn, err = net.ListenUDP("udp", addr)
	}
	return conn, err
}

func Log(c net.Conn, s time.Duration) net.Conn {
	addr := c.RemoteAddr()
	if addr == nil {
		addr = c.LocalAddr()
	}
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", addr), log.LstdFlags)
	return &conn{Conn: c, logger: logger, sleep: s}
}
