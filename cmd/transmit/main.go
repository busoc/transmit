package main

import (
  "fmt"
	"context"
	"encoding/binary"
	"io"
	"net"

	"github.com/midbel/cli"
	"github.com/midbel/toml"
	"github.com/midbel/xxh"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

const (
	DefaultClient = 256
	DefaultBuffer = 32 << 10
)

type Route struct {
	Port uint16
	Addr string `toml:"ip"`
}

var commands = []*cli.Command{
	{
		Usage: "relay <config>",
		Run:   runRelay,
	},
	{
		Usage: "gateway <config>",
		Run:   runGate,
	},
}

func main() {
	cli.RunAndExit(commands, nil)
}

func runRelay(cmd *cli.Command, args []string) error {
	if err := cmd.Flag.Parse(args); err != nil {
		return err
	}

	c := struct {
		Remote string
		Routes []Route `toml:"route"`
	}{}
	if err := toml.DecodeFile(cmd.Flag.Arg(0), &c); err != nil {
		return err
	}
	var group errgroup.Group
	for _, r := range c.Routes {
		fn, err := relay(r.Addr, c.Remote, r.Port)
		if err != nil {
			return err
		}
		group.Go(fn)
	}
	return group.Wait()
}

func runGate(cmd *cli.Command, args []string) error {
	if err := cmd.Flag.Parse(args); err != nil {
		return err
	}
	c := struct {
		Local   string
		Clients uint16
		Routes  []Route `toml:"route"`
	}{}
	if err := toml.DecodeFile(cmd.Flag.Arg(0), &c); err != nil {
		return err
	}
	mx, err := Listen(c.Local, c.Routes)
	if err != nil {
		return err
	}
	return mx.Listen(int64(c.Clients))
}

func relay(local, remote string, port uint16) (func() error, error) {
	w, err := net.Dial("tcp", remote)
	if err != nil {
		return nil, err
	}
	r, err := subscribe(local)
	if err != nil {
		return nil, err
	}
	return func() error {
		defer func() {
			w.Close()
			r.Close()
		}()

		var (
			sum = xxh.New64(0)
			rs  = io.TeeReader(r, sum)
			buf = make([]byte, DefaultBuffer)
		)

		for {
			n, err := rs.Read(buf[4:])
			if err != nil {
				return err
			}
			binary.BigEndian.PutUint16(buf, uint16(n))
			binary.BigEndian.PutUint16(buf[2:], port)
			binary.BigEndian.PutUint64(buf[4+n:], sum.Sum64())

			if _, err := w.Write(buf[:12+n]); err != nil {
				return err
			}

			sum.Reset()
		}
		return nil
	}, nil
}

func subscribe(addr string) (net.Conn, error) {
	a, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	var c net.Conn
	if a.IP.IsMulticast() {
		c, err = net.ListenMulticastUDP("udp", nil, a)
	} else {
		c, err = net.ListenUDP("udp", a)
	}
	return c, err
}

type mux struct {
	srv    net.Listener
	routes map[uint16]net.Conn
}

func Listen(addr string, routes []Route) (*mux, error) {
	s, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	rs := make(map[uint16]net.Conn)
	for _, r := range routes {
		c, err := net.Dial("udp", r.Addr)
		if err != nil {
			return nil, err
		}
		rs[r.Port] = c
	}
	return &mux{
		srv:    s,
		routes: rs,
	}, nil
}

func (m *mux) Listen(conn int64) error {
	defer m.Close()
	if conn == 0 {
		conn = DefaultClient
	}

	var (
		ctx  = context.TODO()
		sema = semaphore.NewWeighted(int64(conn))
	)
	for {
		if err := sema.Acquire(ctx, 1); err != nil {
			return err
		}
		c, err := m.srv.Accept()
		if err != nil {
			break
		}

		go func(c net.Conn) {
			defer c.Close()
      if c, ok := c.(*net.TCPConn); ok {
        c.SetKeepAlive(true)
      }
			m.Recv(c, sema)
		}(c)
	}
	return sema.Acquire(ctx, conn)
}

func (m *mux) Recv(rs io.Reader, sema *semaphore.Weighted) error {
	defer sema.Release(1)

	var (
		buf = make([]byte, DefaultBuffer)
    sum = xxh.New64(0)

		size uint16
		port uint16
    digest uint64
	)
	for {
		binary.Read(rs, binary.BigEndian, &size)
		binary.Read(rs, binary.BigEndian, &port)

		if _, err := io.ReadFull(io.TeeReader(rs, sum), buf[:int(size)]); err != nil {
			return err
		}
		binary.Read(rs, binary.BigEndian, &digest)
		if s := sum.Sum64(); digest != s {
      fmt.Println(digest, s)
			continue
		}
		if err := m.Write(port, buf[:int(size)]); err != nil {
			return err
		}
    sum.Reset()
	}
	return nil
}

func (m *mux) Write(port uint16, buf []byte) error {
	c, ok := m.routes[port]
	if !ok {
		return nil
	}
	_, err := c.Write(buf)
	return err
}

func (m *mux) Close() error {
	for _, c := range m.routes {
		c.Close()
	}
	return nil
}
