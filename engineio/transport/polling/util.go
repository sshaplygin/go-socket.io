package polling

import (
	"errors"
	"mime"
	"strings"
)

type Addr struct {
	Host string
}

func (a Addr) Network() string {
	return "tcp"
}

func (a Addr) String() string {
	return a.Host
}

// contentType is the type of every polling body of Engine.IO v4 in both directions.
const contentType = "text/plain; charset=UTF-8"

// checkContentType accepts text/plain in UTF-8, the only type v4 polling uses.
// application/octet-stream, which v3 used for binary payloads, is invalid.
func checkContentType(m string) error {
	typ, params, err := mime.ParseMediaType(m)
	if err != nil {
		return err
	}
	if typ != "text/plain" {
		return errors.New("invalid content-type")
	}
	if strings.ToLower(params["charset"]) != "utf-8" {
		return errors.New("invalid charset")
	}
	return nil
}
