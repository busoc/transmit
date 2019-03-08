package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"github.com/midbel/toml"
)

func main() {
	flag.Usage = func() {
		fmt.Printf("%s [-c] [-p] [-i] <src> <dst,...>", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	settings := struct {
		Config  bool     `toml:"-"`
		Proto   string   `toml:"protocol"`
		Nic     string   `toml:"nic"`
		Local   string   `toml:"local"`
		Remotes []string `toml:"remote"`
	}{}
	flag.BoolVar(&settings.Config, "c", false, "use configuration file")
	flag.StringVar(&settings.Nic, "i", "", "interface")
	flag.StringVar(&settings.Proto, "p", "udp", "protocol")
	flag.Parse()

	if settings.Config {
		r, err := os.Open(flag.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer r.Close()
		if err := toml.NewDecoder(r).Decode(&settings); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
	} else {
		if flag.NArg() <= 1 {
			flag.Usage()
		}
		settings.Local = flag.Arg(0)
		settings.Remotes = flag.Args()
	}

	var ws []io.Writer
	for i := 1; i < len(settings.Remotes); i++ {
		scheme, addr := settings.Proto, settings.Remotes[i]
		if u, err := url.Parse(addr); err == nil {
			scheme, addr = u.Scheme, u.Host
		}
		c, err := net.Dial(scheme, addr)
		if err != nil {
			log.Fatalf("fail to connect to %s (%s): %s", addr, scheme, err)
		}
		defer c.Close()
		ws = append(ws, c)
	}
	c, err := Listen(settings.Local, settings.Nic)
	if err != nil {
		log.Fatalf("fail to listen to %s: %s", settings.Local, err)
	}
	defer c.Close()

	var w io.Writer
	switch len(ws) {
	case 0:
		fmt.Fprintln(os.Stderr, "no remote host")
		return
	case 1:
		w = ws[0]
	default:
		w = io.MultiWriter(ws...)
	}
	_, err = io.Copy(w, c)
	if err != nil {
		log.Fatalln(err)
	}
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
