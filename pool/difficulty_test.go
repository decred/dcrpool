package pool

import (
	"math/big"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
)

func testDifficulty(t *testing.T) {
	// Test calculate pool target.
	poolTgts := []struct {
		hashRate   *big.Int
		targetTime *big.Int
		expected   string
	}{
		{
			new(big.Int).SetInt64(1.2e12),
			new(big.Int).SetInt64(15),
			"942318434548471642444425333729556541774658078333663444331523307356028928/146484375",
		},
		{
			new(big.Int).SetInt64(1.2e12),
			new(big.Int).SetInt64(10),
			"471159217274235821222212666864778270887329039166831722165761653678014464/48828125",
		},
	}

	for _, test := range poolTgts {
		target, _, err := calculatePoolTarget(chaincfg.MainNetParams(),
			test.hashRate, test.targetTime)
		if err != nil {
			t.Fatal(err)
		}

		expected, success := new(big.Rat).SetString(test.expected)
		if !success {
			t.Fatalf("failed to parse %v as a big.Int", test.expected)
		}

		if target.Cmp(expected) != 0 {
			t.Fatalf("for a hashrate of %v and a target time of %v the "+
				"expected target is %v, got %v", test.hashRate,
				test.targetTime, expected, target)
		}
	}

	// Test fetchMinerDifficulty.
	diffSet := []struct {
		miner   string
		wantErr bool
	}{
		{
			miner:   CPU,
			wantErr: false,
		},
		{
			miner:   "",
			wantErr: true,
		},
		{
			miner:   "antminerdr7",
			wantErr: true,
		},
	}

	for idx, tc := range diffSet {
		net := chaincfg.SimNetParams()
		powLimit := new(big.Rat).SetInt(net.PowLimit)
		set, err := NewDifficultySet(net, powLimit, soloMaxGenTime)
		if err != nil {
			t.Fatalf("[NewDifficultySet] #%d, unexpected error %v", idx+1, err)
		}

		diffInfo, err := set.fetchMinerDifficulty(tc.miner)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[fetchMinerDifficulty] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if !tc.wantErr {
			if diffInfo == nil {
				t.Fatalf("[fetchMinerDifficulty] #%d: expected valid "+
					"difficulty info for %s", idx+1, tc.miner)
			}
		}
	}
}
