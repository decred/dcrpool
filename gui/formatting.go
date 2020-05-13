package gui

import (
	"fmt"
	"math/big"
	"time"

	"github.com/decred/dcrd/dcrutil/v3"
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

func truncateAccountID(accountID string) string {
	return fmt.Sprintf("%.12s...", accountID)
}

// HashString formats the provided hashrate per the best-fit unit.
func hashString(hash *big.Rat) string {
	if hash.Cmp(ZeroRat) == 0 {
		return "0 H/s"
	}

	if hash.Cmp(PetaHash) > 0 {
		ph := new(big.Rat).Quo(hash, PetaHash)
		return fmt.Sprintf("%v PH/s", ph.FloatString(2))
	}

	if hash.Cmp(TeraHash) > 0 {
		th := new(big.Rat).Quo(hash, TeraHash)
		return fmt.Sprintf("%v TH/s", th.FloatString(2))
	}

	if hash.Cmp(GigaHash) > 0 {
		gh := new(big.Rat).Quo(hash, GigaHash)
		return fmt.Sprintf("%v GH/s", gh.FloatString(2))
	}

	if hash.Cmp(MegaHash) > 0 {
		mh := new(big.Rat).Quo(hash, MegaHash)
		return fmt.Sprintf("%v MH/s", mh.FloatString(2))
	}

	if hash.Cmp(KiloHash) > 0 {
		kh := new(big.Rat).Quo(hash, KiloHash)
		return fmt.Sprintf("%v KH/s", kh.FloatString(2))
	}

	return "< 1KH/s"
}

func blockURL(blockExplorerURL string, blockHeight uint32) string {
	return blockExplorerURL + "/block/" + fmt.Sprint(blockHeight)
}

func txURL(blockExplorerURL string, txID string) string {
	return blockExplorerURL + "/tx/" + txID
}

func amount(amt dcrutil.Amount) string {
	return fmt.Sprintf("%.3f DCR", amt.ToCoin())
}

// formatUnixTime formats the provided integer as a UTC time string,
func formatUnixTime(unix int64) string {
	return time.Unix(0, unix).Format("2-Jan-2006 15:04:05 MST")
}

// floatToPercent formats the provided float64 as a percentage,
// rounded to the nearest decimal place. eg. "10.5%"
func floatToPercent(rat float64) string {
	rat *= 100
	str := fmt.Sprintf("%.1f%%", rat)
	return str
}

// ratToPercent formats the provided big.Rat as a percentage,
// rounded to the nearest decimal place. eg. "10.5%"
func ratToPercent(rat *big.Rat) string {
	real, _ := rat.Float64()
	return floatToPercent(real)
}
