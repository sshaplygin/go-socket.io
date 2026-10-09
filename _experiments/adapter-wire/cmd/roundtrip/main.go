// Command roundtrip emits Go-encoded counterparts of the Node wire fixtures.
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sshaplygin/go-socket.io/experiments/adapter-wire/internal/fixture"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	f, err := fixture.Load("testdata/publications.json")
	if err != nil {
		return err
	}
	for i := range f.Cases {
		for j := range f.Cases[i].Publications {
			p := &f.Cases[i].Publications[j]
			value, err := p.Decode()
			if err != nil {
				return err
			}
			wire, err := fixture.Encode(p.Encoding, value)
			if err != nil {
				return err
			}
			p.WireBase64 = base64.StdEncoding.EncodeToString(wire)
		}
	}
	return json.NewEncoder(os.Stdout).Encode(f)
}
