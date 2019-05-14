// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package util

import (
	"fmt"
	"math/big"
)

var (
	// ZeroInt is the default value for a big.Int.
	ZeroInt = new(big.Int).SetInt64(0)

	// ZeroRat is the default value for a big.Rat.
	ZeroRat = new(big.Rat).SetInt64(0)

	// KiloHash is 1 KH represented as a big.Rat.
	KiloHash = new(big.Rat).SetInt64(1000)

	// MegaHash is 1MH represented as a big.Rat.
	MegaHash = new(big.Rat).SetInt64(1000000)

	// GigaHash is 1GH represented as a big.Rat.
	GigaHash = new(big.Rat).SetInt64(1000000000)

	// TeraHash is 1TH represented as a big.Rat.
	TeraHash = new(big.Rat).SetInt64(1000000000000)

	// PetaHash is 1PH represented as a big.Rat
	PetaHash = new(big.Rat).SetInt64(1000000000000000)
)

// HashString formats the provided hashrate per the best-fit unit.
func HashString(hash *big.Rat) string {
	if hash.Cmp(ZeroRat) == 0 {
		return "0 H/s"
	}

	if hash.Cmp(PetaHash) > 0 {
		ph := new(big.Rat).Quo(hash, PetaHash)
		return fmt.Sprintf("%v PH/s", ph.FloatString(4))
	}

	if hash.Cmp(TeraHash) > 0 {
		th := new(big.Rat).Quo(hash, TeraHash)
		return fmt.Sprintf("%v TH/s", th.FloatString(4))
	}

	if hash.Cmp(GigaHash) > 0 {
		gh := new(big.Rat).Quo(hash, GigaHash)
		return fmt.Sprintf("%v GH/s", gh.FloatString(4))
	}

	if hash.Cmp(MegaHash) > 0 {
		mh := new(big.Rat).Quo(hash, MegaHash)
		return fmt.Sprintf("%v MH/s", mh.FloatString(4))
	}

	if hash.Cmp(KiloHash) > 0 {
		kh := new(big.Rat).Quo(hash, KiloHash)
		return fmt.Sprintf("%v KH/s", kh.FloatString(4))
	}

	return "< 1KH/s"
}
