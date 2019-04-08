// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package util

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"time"
)

const (
	// uint256Size is the number of bytes needed to represent an unsigned
	// 256-bit integer.
	uint256Size = 32
)

// BigToLEUint256 returns the passed big integer as an unsigned 256-bit integer
// encoded as little-endian bytes.  Numbers which are larger than the max
// unsigned 256-bit integer are truncated.
func BigToLEUint256(n *big.Int) [uint256Size]byte {
	// Pad or truncate the big-endian big int to correct number of bytes.
	nBytes := n.Bytes()
	nlen := len(nBytes)
	pad := 0
	start := 0
	if nlen <= uint256Size {
		pad = uint256Size - nlen
	} else {
		start = nlen - uint256Size
	}
	var buf [uint256Size]byte
	copy(buf[pad:], nBytes[start:])

	// Reverse the bytes to little endian and return them.
	for i := 0; i < uint256Size/2; i++ {
		buf[i], buf[uint256Size-1-i] = buf[uint256Size-1-i], buf[i]
	}
	return buf
}

// LEUint256ToBig returns the passed unsigned 256-bit integer
// encoded as little-endian as a big integer.
func LEUint256ToBig(n [uint256Size]byte) *big.Int {
	var buf [uint256Size]byte
	copy(buf[:], n[:])

	// Reverse the bytes to big endian and create a big.Int.
	for i := 0; i < uint256Size/2; i++ {
		buf[i], buf[uint256Size-1-i] = buf[uint256Size-1-i], buf[i]
	}

	v := new(big.Int).SetBytes(buf[:])

	return v
}

// HeightToBigEndianBytes returns an 4-byte big endian representation of
// the provided block height.
func HeightToBigEndianBytes(height uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, height)
	return b
}

// BigEndianBytesToHeight returns the block height of the provided 4-byte big
// endian representation.
func BigEndianBytesToHeight(b []byte) uint32 {
	return binary.BigEndian.Uint32(b[0:4])
}

// ReversePrevBlockWords reverses each 4-byte word in the provided hex encoded
// previous block hash.
func ReversePrevBlockWords(hashE string) string {
	buf := bytes.NewBufferString("")
	for i := 0; i < len(hashE); i += 8 {
		buf.WriteString(hashE[i+6 : i+8])
		buf.WriteString(hashE[i+4 : i+6])
		buf.WriteString(hashE[i+2 : i+4])
		buf.WriteString(hashE[i : i+2])
	}
	return buf.String()
}

// HexReversed reverses a hex string.
func HexReversed(in string) (string, error) {
	if len(in)%2 != 0 {
		return "", fmt.Errorf("incorrect hex input length")
	}

	buf := bytes.NewBufferString("")
	for i := len(in) - 1; i > -1; i -= 2 {
		buf.WriteByte(in[i-1])
		buf.WriteByte(in[i])
	}
	return buf.String(), nil
}

// NanoToBigEndianBytes returns an 8-byte big endian representation of
// the provided nanosecond time.
func NanoToBigEndianBytes(nano int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(nano))
	return b
}

// BigEndianBytesToNano returns nanosecond time of the provided 8-byte big
// endian representation.
func BigEndianBytesToNano(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(b[0:8]))
}

// BigEndianBytesToTime returns a time instance of the provided 8-byte big
// endian representation.
func BigEndianBytesToTime(b []byte) *time.Time {
	t := time.Unix(0, BigEndianBytesToNano(b))
	return &t
}
