package polling

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckContentType(t *testing.T) {
	at := assert.New(t)

	tests := []struct {
		mime string
		ok   bool
	}{
		{"text/plain; charset=utf-8", true},
		{"text/plain;charset=UTF-8", true},

		// v3 carried binary payloads in this type; v4 polling has no such body.
		{"application/octet-stream", false},
		{"text/plain;charset=gbk", false},
		{"text/plain charset=U;TF-8", false},
		{"text/html", false},
		{"", false},
	}

	for _, test := range tests {
		at.Equal(test.ok, checkContentType(test.mime) == nil, test.mime)
	}
}
