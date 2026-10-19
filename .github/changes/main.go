// Command changes decides which CI jobs a pull request needs. It reads the diff between
// two revisions, selects gates for their Markdown inputs, sends a change of Go comments or formatting only to the
// lint job of its module, maps the other Go changes to packages and their reverse
// dependencies through `go list -deps -test`, and prints the job inputs as GitHub Actions
// outputs (vN starts lint-vN, vN_packages the test jobs):
//
//	go run ./.github/changes BASE HEAD
//	go run ./.github/changes -full
//
// The working tree must contain HEAD: the head revision or the merge of it into the base
// branch, as a pull_request checkout gives. -full prints the decision of a push or a
// scheduled run: every job, every package, every benchmark. Reasons go to stderr.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// pattern is what a package list may contain: it reaches a shell as words.
var pattern = regexp.MustCompile(`^\.(/[A-Za-z0-9_.\-]+)*(/\.\.\.)?$`)

type output struct{ key, value string }

func join(list []string) (string, error) {
	for _, p := range list {
		if !pattern.MatchString(p) {
			return "", fmt.Errorf("unexpected package pattern %q", p)
		}
	}
	return strings.Join(list, " "), nil
}

// outputs renders the decision. dir is the repository root.
func outputs(dir string, d *decision) ([]output, error) {
	var out []output
	add := func(key string, value any) { out = append(out, output{key, fmt.Sprint(value)}) }
	bench := false
	for _, mod := range []string{v1, v2} {
		root := dir
		if mod == v2 {
			root = filepath.Join(dir, "v2")
		}
		r, err := d.result(mod, root)
		if err != nil {
			return nil, err
		}
		packages, err := join(r.packages)
		if err != nil {
			return nil, err
		}
		benchList, err := join(r.bench)
		if err != nil {
			return nil, err
		}
		add(mod, r.run)
		add(mod+"_packages", packages)
		add("gates_"+mod, r.gates)
		add("examples_"+mod, r.examples)
		if mod == v1 {
			add("conformance", r.conformance)
		} else {
			add("experiments_v2", r.experiments)
		}
		add("bench_"+mod, benchList)
		bench = bench || benchList != ""
	}
	add("sync", d.sync)
	add("changed", bench)
	return out, nil
}

func fullDecision() *decision {
	d := newDecision()
	for _, mod := range []string{v1, v2} {
		d.fullRun(mod, true, "push or scheduled run")
	}
	d.sync = true
	return d
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("changes", flag.ContinueOnError)
	flags.SetOutput(stderr)
	full := flags.Bool("full", false, "run every job (push and schedule)")
	dir := flags.String("C", ".", "repository root; its working tree contains HEAD")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var d *decision
	switch {
	case *full && flags.NArg() == 0:
		d = fullDecision()
	case !*full && flags.NArg() == 2:
		var err error
		if d, err = classify(*dir, flags.Arg(0), flags.Arg(1)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("usage: go run ./.github/changes [-C dir] BASE HEAD | -full")
	}
	out, err := outputs(*dir, d)
	if err != nil {
		return err
	}
	for _, reason := range d.reasons {
		if _, err := fmt.Fprintln(stderr, reason); err != nil {
			return err
		}
	}
	for _, o := range out {
		if _, err := fmt.Fprintf(stdout, "%s=%s\n", o.key, o.value); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "changes:", err)
		os.Exit(1)
	}
}
