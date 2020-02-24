package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

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

var commands = []*cli.Command{
	{
		Usage: "relay <config>",
		Short: "forwards packets from multicast groups to a remote network via TCP",
		// Desc:  ``,
		Run: runRelay,
	},
	{
		Usage: "gateway <config>",
		Short: "forwards packets incoming packets to a set of multicast groups",
		// Desc:  ``,
		Run: runGate,
	},
	{
		Usage: "feed [-z] [-p] [-c] [-s] <addr>",
		Alias: []string{"sim", "play", "test"},
		Short: "create and send dummy packets to a UDP service",
		// Desc:  ``,
		Run: runFeed,
	},
}

const help = `{{.Name}} allows to send packets from multicast groups to another network via TCP.

Usage:

  {{.Name}} command [arguments]

The commands are:

{{range .Commands}}{{printf "  %-9s %s" .String .Short}}
{{end}}

Use {{.Name}} [command] -h for more information about its usage.
`

type Route struct {
	Port uint16 `toml:"id"`
	Addr string `toml:"ip"`
}

type Certificate struct {
	Pem      string   `toml:"cert-file"`
	Key      string   `toml:"key-file"`
	CertAuth []string `toml:"cert-auth"`
	Policy   string   `toml:"policy"`
	Insecure bool     `toml:"insecure"`
}

func (c Certificate) Client(inner net.Conn) (net.Conn, error) {
	if c.Pem == "" && c.Key == "" {
		return inner, nil
	}
	pool, err := c.buildCertPool()
	if err != nil {
		return nil, err
	}

	cert, err := tls.LoadX509KeyPair(c.Pem, c.Key)
	if err != nil {
		return nil, err
	}

	cfg := tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		InsecureSkipVerify: c.Insecure,
	}
	return tls.Client(inner, &cfg), nil
}

func (c Certificate) Listen(inner net.Listener) (net.Listener, error) {
	if c.Pem == "" && c.Key == "" {
		return inner, nil
	}

	pool, err := c.buildCertPool()
	if err != nil {
		return nil, err
	}

	cert, err := tls.LoadX509KeyPair(c.Pem, c.Key)
	if err != nil {
		return nil, err
	}

	cfg := tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
	}

	switch strings.ToLower(c.Policy) {
	case "request":
		cfg.ClientAuth = tls.RequestClientCert
	case "require", "any":
		cfg.ClientAuth = tls.RequireAnyClientCert
	case "verify":
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	case "none":
		cfg.ClientAuth = tls.NoClientCert
	case "", "require+verify":
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	default:
		return nil, fmt.Errorf("%s: unknown policy", c.Policy)
	}

	return tls.NewListener(inner, &cfg), nil
}

func (c Certificate) buildCertPool() (*x509.CertPool, error) {
	if len(c.CertAuth) == 0 {
		return x509.SystemCertPool()
	}
	pool := x509.NewCertPool()
	for _, f := range c.CertAuth {
		pem, err := ioutil.ReadFile(f)
		if err != nil {
			return nil, err
		}
		if ok := pool.AppendCertsFromPEM(pem); !ok {
			return nil, fmt.Errorf("fail to append certificate %s", f)
		}
	}
	return pool, nil
}

func main() {
	cli.RunAndExit(commands, cli.Usage("transmit", help, commands))
}

func runRelay(cmd *cli.Command, args []string) error {
	if err := cmd.Flag.Parse(args); err != nil {
		return err
	}

	c := struct {
		Remote string
		Cert   Certificate `toml:"certificate"`
		Routes []Route     `toml:"route"`
	}{}
	if err := toml.DecodeFile(cmd.Flag.Arg(0), &c); err != nil {
		return err
	}
	var group errgroup.Group
	for _, r := range c.Routes {
		_, port, err := net.SplitHostPort(r.Addr)
		if err == nil && r.Port == 0 {
			p, err := strconv.ParseUint(port, 10, 16)
			if err != nil {
				return err
			}
			r.Port = uint16(p)
		}
		fn, err := relay(c.Remote, r.Addr, r.Port, c.Cert)
		if err != nil {
			return err
		}
		group.Go(fn)
	}
	return group.Wait()
}

func relay(remote, local string, port uint16, cert Certificate) (func() error, error) {
	w, err := Dial(remote, cert)
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

type Conn struct {
	net.Conn
	cert Certificate

	mu     sync.Mutex
	writer io.Writer
}

func Dial(addr string, cert Certificate) (net.Conn, error) {
	x, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	x, err = cert.Client(x)
	if err != nil {
		return nil, err
	}
	c := Conn{
		Conn:   x,
		writer: x,
	}
	return &c, nil
}

func (c *Conn) Write(bs []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.writer.Write(bs)
	if err != nil {
		switch c.Conn.(type) {
		case *net.UDPConn:
		case *net.TCPConn, *tls.Conn:
			c.writer = ioutil.Discard

			go c.reconnect()
		}
	}
	return len(bs), nil
}

func (c *Conn) reconnect() {
	c.Conn.Close()

	addr := c.RemoteAddr().String()
	for {
		x, err := net.DialTimeout("tcp", addr, time.Second*5)
		if err == nil {
			c.mu.Lock()
			defer c.mu.Unlock()
			x, _ = c.cert.Client(x)

			c.writer, c.Conn = x, x
			break
		}
	}
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

func runGate(cmd *cli.Command, args []string) error {
	if err := cmd.Flag.Parse(args); err != nil {
		return err
	}
	c := struct {
		Local   string
		Clients uint16
		Cert    Certificate
		Routes  []Route `toml:"route"`
	}{}
	if err := toml.DecodeFile(cmd.Flag.Arg(0), &c); err != nil {
		return err
	}
	mx, err := Listen(c.Local, c.Cert, c.Routes)
	if err != nil {
		return err
	}
	return mx.Listen(int64(c.Clients))
}

type mux struct {
	srv    net.Listener
	routes map[uint16]net.Conn
}

func Listen(addr string, cert Certificate, routes []Route) (*mux, error) {
	s, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s, err = cert.Listen(s)
	if err != nil {
		return nil, err
	}
	rs := make(map[uint16]net.Conn)
	for _, r := range routes {
		_, port, err := net.SplitHostPort(r.Addr)
		if err == nil && r.Port == 0 {
			p, err := strconv.ParseUint(port, 10, 16)
			if err != nil {
				return nil, err
			}
			r.Port = uint16(p)
		}
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

		size   uint16
		port   uint16
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

func runFeed(cmd *cli.Command, args []string) error {
	var (
		zero  = cmd.Flag.Bool("z", false, "zero")
		size  = cmd.Flag.Int("s", 1024, "size")
		count = cmd.Flag.Int("c", 0, "count")
		sleep = cmd.Flag.Duration("p", 0, "sleep")
	)
	if err := cmd.Flag.Parse(args); err != nil {
		return err
	}
	r := Dummy(*size, *zero)
	w, err := net.Dial("udp", cmd.Flag.Arg(0))
	if err != nil {
		return err
	}
	defer w.Close()
	for i := 0; *count <= 0 || i < *count; i++ {
		if _, err := io.Copy(w, r); err != nil {
			return err
		}
		if *sleep > 0 {
			time.Sleep(*sleep)
		}
	}
	return nil
}

func Dummy(z int, zero bool) io.Reader {
	xs := make([]byte, z)
	if !zero {
		for i := 0; i < z; i += 4 {
			binary.BigEndian.PutUint32(xs[i:], rand.Uint32())
		}
	}
	return empty{alea: !zero, buffer: xs}
}

type empty struct {
	alea   bool
	buffer []byte
}

func (e empty) Read(xs []byte) (int, error) {
	if e.alea {
		rand.Shuffle(len(e.buffer), func(i, j int) {
			e.buffer[i], e.buffer[j] = e.buffer[j], e.buffer[i]
		})
	}
	n := copy(xs, e.buffer)
	return n, nil
}
