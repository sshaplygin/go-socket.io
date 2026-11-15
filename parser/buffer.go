package parser

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Buffer carries binary data in event arguments. A Buffer included in the
// encoded JSON is sent as a binary attachment. Omitted fields and values inside
// custom JSON or text marshalers do not produce attachments.
type Buffer struct {
	num      uint64
	isBinary bool

	// Data is the binary content. A decoded Buffer receives the bytes of its
	// attachment, or of the data array of a {"type":"Buffer","data":[...]}
	// object sent as JSON.
	Data []byte
}

// BufferData is the JSON object that Buffer.UnmarshalJSON accepts: either an
// attachment placeholder {"_placeholder":true,"num":N} or a Node.js Buffer
// object {"type":"Buffer","data":[...]}.
type BufferData struct {
	// Num is the zero-based index of the attachment that a placeholder refers to.
	Num uint64
	// PlaceHolder reports whether the object is an attachment placeholder.
	PlaceHolder bool `json:"_placeholder"`
	// Data is the byte array of a Node.js Buffer object.
	Data []byte
}

// MarshalJSON returns the JSON form of a: the Node.js Buffer object
// {"type":"Buffer","data":[...]} with the bytes as numbers, or the attachment
// placeholder of a Buffer decoded from a placeholder that was not replaced by
// an attachment. Prepare does not call it for a Buffer it reaches in a payload,
// which it sends as an attachment; encoding/json calls it, for example for a
// Buffer inside a value with a custom marshaler.
func (a Buffer) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	if err := a.marshalJSONBuf(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (a *Buffer) marshalJSONBuf(buf *bytes.Buffer) error {
	encode := a.encodeText
	if a.isBinary {
		encode = a.encodeBinary
	}

	return encode(buf)
}

func (a *Buffer) encodeText(buf *bytes.Buffer) error {
	buf.WriteString(`{"type":"Buffer","data":[`)
	for i, d := range a.Data {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(strconv.Itoa(int(d)))
	}
	buf.WriteString("]}")

	return nil
}

func (a *Buffer) encodeBinary(buf *bytes.Buffer) error {
	buf.WriteString(`{"_placeholder":true,"num":`)
	buf.WriteString(strconv.FormatUint(a.num, 10))
	buf.WriteString("}")

	return nil
}

// UnmarshalJSON decodes a BufferData object into a. A placeholder records the
// attachment index, which DecodeArgs replaces with the attachment bytes.
func (a *Buffer) UnmarshalJSON(b []byte) error {
	var data BufferData
	if err := json.Unmarshal(b, &data); err != nil {
		return err
	}

	a.isBinary = data.PlaceHolder
	a.Data = data.Data
	a.num = data.Num

	return nil
}
