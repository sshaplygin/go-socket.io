// Command ws-bench compares complete-message echo implementations in separate
// server processes. It does not run Engine.IO or Socket.IO sessions.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	if err := command(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func command(args []string) error {
	fs := flag.NewFlagSet("ws-bench", flag.ContinueOnError)
	var c config
	serve := fs.Bool("serve", false, "internal subprocess mode")
	fs.StringVar(&c.Backend, "backend", "gorilla", "gorilla or gobwas-prototype")
	fs.StringVar(&c.Mode, "mode", "echo", "echo or idle")
	fs.IntVar(&c.Connections, "connections", 1, "open client connections (idle target may be 10000)")
	fs.IntVar(&c.Messages, "messages", 1000, "measured round trips per connection")
	fs.IntVar(&c.Size, "size", 1024, "binary payload bytes")
	fs.DurationVar(&c.Timeout, "timeout", 60*time.Second, "whole run deadline")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if err := c.validate(); err != nil {
		return err
	}
	if *serve {
		return serveBackend(c.Backend, c.Timeout)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	c.ServerCommand = []string{executable}
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout)
	defer cancel()
	r, err := run(ctx, c)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
