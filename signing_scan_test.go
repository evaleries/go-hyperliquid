package hyperliquid

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The write-free scanner must agree with the copying converter on every
// payload shape: conversion happens iff a convertible str16 header exists.
func TestScanMsgpackValueAgreesWithConverter(t *testing.T) {
	corpus := [][]byte{
		// plain scalars
		{0x00}, {0x7f}, {0xe0}, {0xc0}, {0xc2}, {0xc3},
		// fixstr / fixarray / fixmap
		{0xa1, 'x'}, {0x90}, {0x80}, {0x91, 0x01}, {0x81, 0xa1, 'k', 0x05},
		// str8 already compact
		{0xd9, 0x03, 'a', 'b', 'c'},
		// str16 convertible (len < 256)
		append([]byte{0xda, 0x00, 0x64}, []byte(strings.Repeat("A", 100))...),
		// str16 not convertible (len >= 256)
		append([]byte{0xda, 0x01, 0x2c}, []byte(strings.Repeat("B", 300))...),
		// str32
		append([]byte{0xdb, 0x00, 0x00, 0x01, 0x2c}, []byte(strings.Repeat("C", 300))...),
		// nested: array -> map -> convertible str16
		append([]byte{0x91, 0x81, 0xa1, 'k', 0xda, 0x00, 0xc8}, []byte(strings.Repeat("B", 200))...),
		// uint64 containing 0xda as a data byte — must NOT be treated as str16
		{0xcf, 0x00, 0x00, 0x00, 0x54, 0x38, 0xda, 0x00, 0xa4},
		// bin8/bin16 with 0xda in payload
		{0xc4, 0x02, 0xda, 0x00},
		{0xc5, 0x00, 0x02, 0xda, 0xda},
		// floats and ints
		{0xca, 0x3f, 0x80, 0x00, 0x00},
		{0xcb, 0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2d, 0x18},
		{0xcc, 0xff}, {0xcd, 0xff, 0xff}, {0xd0, 0x80}, {0xd3},
		// fixext
		{0xd4, 0x01, 0x00},
		// ext8
		{0xc7, 0x02, 0x01, 0xda, 0x00},
		// map16 with convertible nested str16
		append([]byte{0xde, 0x00, 0x01, 0xa1, 'k', 0xda, 0x00, 0x20}, []byte(strings.Repeat("D", 32))...),
		// array16 empty
		{0xdc, 0x00, 0x00},
		// malformed: truncated str16 — scanner must defer to copying path
		{0xda, 0x00},
	}

	malformed := map[int]bool{24: true, 29: true} // truncated int64 / str16

	for i, data := range corpus {
		detected := msgpackNeedsStr16Conversion(data)
		if malformed[i] {
			assert.True(t, detected,
				"case %d (%x): malformed input must take the fail-safe copying path", i, data)
			continue
		}
		converted := convertStr16ToStr8(append([]byte(nil), data...))
		changed := !bytes.Equal(converted, data)
		assert.Equal(t, changed, detected,
			"case %d (%x): converter changed=%v scanner detected=%v", i, data, changed, detected)
	}
}
