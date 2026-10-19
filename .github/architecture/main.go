package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	root := flag.String("root", ".", "v1 module directory")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	count, err := inspect(ctx, *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "architecture-check:", err)
		os.Exit(1)
	}
	fmt.Printf("architecture: %d runtime packages; direct edges, role/codec closures and unsupported paths: PASS\n", count)
}
