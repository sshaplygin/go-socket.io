# Engine.IO v3 payload vectors

`payload.json` contains 13 binary/text vectors shared by the codec and polling
lifecycle tests. Byte fields use JSON base64 encoding. Cases include mixed
text/binary packets, base64, CJK, BMP and supplementary UTF-16 lengths.

The vector source and its oracle annotations are available with
`git show 67e1926:engineio/payload/data_test.go`. The annotations identify
`engine.io-parser` 2.2.1 as the reference for the mixed binary examples.
