# Engine.IO v3 payload vectors

`payload.json` preserves the 13 binary/text vectors formerly declared in
`engineio/payload/data_test.go` at `67e1926`. Byte fields use JSON base64 encoding.
The codec and polling lifecycle tests read the same corpus. Cases include mixed
text/binary packets, base64, CJK, BMP and supplementary UTF-16 lengths; source comments
identified engine.io-parser 2.2.1 as the oracle for the mixed binary examples.
