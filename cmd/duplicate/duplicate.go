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
	"path/filepath"
	"time"
)

type Counter interface {
	Stats() (uint64, uint64, uint64)
}

func main() {
	flag.Usage = func() {
		fmt.Printf("%s [-p] [-v] <src> <dst,...>", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	ifi := flag.String("i", "", "interface")
	proto := flag.String("p", "udp", "protocol")
	verbose := flag.Bool("v", false, "verbose")
	flag.Parse()
	if flag.NArg() <= 1 {
		flag.Usage()
	}

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
			c = Log(c)
		}
		defer c.Close()
		ws = append(ws, c)
	}
	c, err := Listen(flag.Arg(0), *ifi)
	if err != nil {
		log.Fatalf("fail to listen to %s: %s", flag.Arg(0), err)
	}
	if *verbose {
		c = Log(c)
	}
	defer c.Close()

	if err := duplicate(c, ws); err != nil {
		log.Fatalln(err)
	}
}

func duplicate(r io.Reader, ws []io.Writer) error {
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

type statConn struct {
	net.Conn

	logger                *log.Logger
	count, size, errcount uint64
}

func (c *statConn) Stats() (uint64, uint64, uint64) {
	return c.count, c.size, c.errcount
}

func (c *statConn) Read(bs []byte) (int, error) {
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

func (c *statConn) Write(bs []byte) (int, error) {
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

func Listen(a, ifi string) (net.Conn, error) {
	addr, err := net.ResolveUDPAddr("udp", a)
	if err != nil {
		return nil, err
	}
	var conn *net.UDPConn
	if addr.IP.IsMulticast() {
		var i *net.Interface
		if ifi, err := net.InterfaceByName(ifi); err == nil {
			i = ifi
		}
		conn, err = net.ListenMulticastUDP("udp", i, addr)
	} else {
		conn, err = net.ListenUDP("udp", addr)
	}
	return conn, err
}

func Log(c net.Conn) net.Conn {
	addr := c.RemoteAddr()
	if addr == nil {
		addr = c.LocalAddr()
	}
	logger := log.New(os.Stdout, fmt.Sprintf("[%s] ", addr), log.LstdFlags)
	return &statConn{Conn: c, logger: logger}
}

type sleepConn struct {
	net.Conn
	sleep time.Duration
}

func Sleep(c net.Conn, s time.Duration) net.Conn {
	if s <= 0 {
		return c
	}
	return &sleepConn{Conn: c, sleep: s}
}

func (c *sleepConn) Read(bs []byte) (int, error) {
	time.Sleep(c.sleep)
	return c.Conn.Read(bs)
}

func (c *sleepConn) Write(bs []byte) (int, error) {
	time.Sleep(c.sleep)
	return c.Conn.Write(bs)
}
