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
)

func main() {
	flag.Usage = func() {
		fmt.Printf("%s [-p] [-i] <src> <dst,...>", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
	ifi := flag.String("i", "", "interface")
	proto := flag.String("p", "udp", "protocol")
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
		defer c.Close()
		ws = append(ws, c)
	}
	c, err := Listen(flag.Arg(0), *ifi)
	if err != nil {
		log.Fatalf("fail to listen to %s: %s", flag.Arg(0), err)
	}
	defer c.Close()

	_, err = io.Copy(io.MultiWriter(ws...), c)
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
