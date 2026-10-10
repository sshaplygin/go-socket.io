package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

// TestOracleBridge keeps testdata/oracle, the Go side of the Node check in
// testdata/oracle/verify.mjs, building and answering. Go tooling skips testdata
// directories, so vet and lint do not see it. The Node comparison itself is run by
// hand (docs/ROADMAP.md, Prepared components: the oracle needs npm).
func TestOracleBridge(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	in := `[{"envelope":"51-/x,3[\"e\",{\"_placeholder\":true,\"num\":0}]","attachments":["AP8="]},` +
		`{"envelope":"2[]"},` +
		`{"packet":{"type":3,"namespace":"/go","id":42,"data":[null,{"_placeholder":true,"num":0}]},"attachments":["AP8="]}]`
	cmd := exec.CommandContext(ctx, goTool, "run", "./testdata/oracle")
	cmd.Stdin = bytes.NewReader([]byte(in))
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go run: %v\n%s", err, stderr.String())
	}
	var got []struct {
		Envelope    string
		Attachments []string
		Data        json.RawMessage
		Error       string
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 3 {
		t.Fatalf("%v %s", err, out.String())
	}
	if got[0].Error != "" || got[0].Envelope != `51-/x,3["e",{"_placeholder":true,"num":0}]` ||
		string(got[0].Data) != `["e",{"$binary":"AP8="}]` {
		t.Errorf("binary event: %+v", got[0])
	}
	if got[1].Error == "" {
		t.Errorf("an EVENT without a name was accepted: %+v", got[1])
	}
	if got[2].Error != "" || got[2].Envelope != `61-/go,42[null,{"_placeholder":true,"num":0}]` {
		t.Errorf("go-origin ack: %+v", got[2])
	}
}
