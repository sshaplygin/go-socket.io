package parser

import (
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// TestSharedInputConcurrent encodes one Packet, one Arguments value and one value of
// a typed codec from many goroutines, as a broadcast to several connections does. Run
// under -race it fails if any of them writes to its input; afterwards the inputs must
// equal their pre-call copies and every output must be identical and independent.
func TestSharedInputConcurrent(t *testing.T) {
	p := Packet{
		Type: Event, Namespace: "/room", ID: id(3),
		Data:        json.RawMessage(`["m",{"_placeholder":true,"num":0},{"k":[1,2,3]}]`),
		Attachments: [][]byte{{1, 2, 3, 4}},
	}
	args := Arguments{Values: raws(`{"a":1}`, string(Placeholder(0))), Attachments: [][]byte{{5, 6}}}
	typed := upload{Name: "n", Data: bin{7}, Parts: []bin{{8}, {9}}, ByKey: map[string]bin{"x": {1}, "y": {2}}}
	wantP := Packet{Type: p.Type, Namespace: p.Namespace, ID: id(3), Data: append(json.RawMessage{}, p.Data...), Attachments: [][]byte{{1, 2, 3, 4}}}
	wantArgs := Arguments{Values: raws(`{"a":1}`, string(Placeholder(0))), Attachments: [][]byte{{5, 6}}}
	wantTyped := upload{Name: "n", Data: bin{7}, Parts: []bin{{8}, {9}}, ByKey: map[string]bin{"x": {1}, "y": {2}}}

	codec := JSON[upload](Limits{})
	refText, refAtts, err := Encode(p, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	refTyped, err := codec.Encode(typed)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	errDiffers := errors.New("encoded output differs from the reference")
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				text, atts, err := Encode(p, Limits{})
				if err != nil || string(text) != string(refText) || !reflect.DeepEqual(atts, refAtts) {
					errs <- errors.Join(errDiffers, err)
					return
				}
				// Each caller owns its output and may change it.
				text[0] = 'X'
				atts[0][0] = 0

				if _, _, err := EventArguments(p); err != nil {
					errs <- err
					return
				}
				if _, err := Concat(args, args); err != nil {
					errs <- err
					return
				}
				if _, err := args.Slice(1, 2); err != nil {
					errs <- err
					return
				}
				if err := args.Validate(Limits{}); err != nil {
					errs <- err
					return
				}
				if _, err := EventPacket("/", nil, "e", args); err != nil {
					errs <- err
					return
				}
				enc, err := codec.Encode(typed)
				if err != nil || !reflect.DeepEqual(enc, refTyped) {
					errs <- errors.Join(errDiffers, err)
					return
				}
				if _, err := codec.Decode(refTyped); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("worker failed: %v", err)
	}
	if !reflect.DeepEqual(p, wantP) || !reflect.DeepEqual(args, wantArgs) || !reflect.DeepEqual(typed, wantTyped) {
		t.Fatalf("a shared input was modified:\n%#v\n%#v\n%#v", p, args, typed)
	}
	if !reflect.DeepEqual(refTyped.Attachments, [][]byte{{7}, {8}, {9}, {1}, {2}}) {
		t.Fatalf("reference attachments %v", refTyped.Attachments)
	}
}
