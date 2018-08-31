package main

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/busoc/transmit"
	"github.com/midbel/cli"
)

func runDuplicate(cmd *cli.Command, args []string) error {
	var rate cli.Size
	cmd.Flag.Var(&rate, "r", "rate limit bucket size")
	every := cmd.Flag.Duration("e", time.Millisecond*4, "rate limit fill interval")
	global := cmd.Flag.Bool("g", false, "rate limit global")
	if err := cmd.Flag.Parse(args); err != nil {
		return err
	}
	var b *transmit.Bucket
	if r := rate.Int(); r > 0 && *global {
		b = transmit.NewBucket(r, *every)
	}
	var ws []io.Writer
	for i := 1; i < cmd.Flag.NArg(); i++ {
		c, err := net.Dial("udp", cmd.Flag.Arg(i))
		if err != nil {
			return err
		}
		defer c.Close()
		var buck = b
		if r := rate.Int(); !*global && r > 0 {
			buck = transmit.NewBucket(r, *every)
		}
		var w io.Writer = c
		if buck != nil {
			w = transmit.Writer(c, buck)
		}
		ws = append(ws, w)
	}
	if len(ws) == 0 {
		return fmt.Errorf("no remote address given")
	}
	a, err := net.ResolveUDPAddr("udp", cmd.Flag.Arg(0))
	if err != nil {
		return err
	}
	r, err := net.ListenUDP("udp", a)
	if err != nil {
		return err
	}
	w := io.MultiWriter(ws...)
	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	return nil
}
