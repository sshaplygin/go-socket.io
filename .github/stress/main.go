// Command stress summarizes the go test -json stream of make test-stress-load as Markdown:
// failing tests with failure counts and first failure output, package-level failures, tests
// that never ended, race reports by package and the shuffle seed of each package. It writes
// to standard output and, with -summary, to a file; it exits non-zero only when it cannot
// read or write, since the test result is the exit status of go test.
//
//	go run ./.github/stress -json stress.json -summary stress-summary.md
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("stress", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	in := flags.String("json", "-", "go test -json stream; - reads standard input")
	out := flags.String("summary", "", "write the Markdown summary to this file as well")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return fmt.Errorf("usage: go run ./.github/stress [-json FILE] [-summary FILE]")
	}
	var r io.Reader = stdin
	if *in != "-" {
		f, err := os.Open(*in)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	md, err := summarize(r)
	if err != nil {
		return err
	}
	if *out != "" {
		if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
			return err
		}
	}
	_, err = io.WriteString(stdout, md)
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "stress:", err)
		os.Exit(1)
	}
}
