package pool

import (
	"math/big"
	"testing"

	"github.com/decred/dcrd/chaincfg/v3"
)

func TestDifficulty(t *testing.T) {
	// Test calculate pool target.
	poolTgts := []struct {
		hashRate       *big.Int
		targetTime     *big.Int
		expectedTarget string
		expectedDiff   string
	}{
		{
			new(big.Int).SetInt64(1.2e12),
			new(big.Int).SetInt64(15),
			"942318434548471642444425333729556541774658078333663444331523307356028928/146484375",
			"2197265625/524288",
		},
		{
			new(big.Int).SetInt64(1.2e12),
			new(big.Int).SetInt64(10),
			"471159217274235821222212666864778270887329039166831722165761653678014464/48828125",
			"732421875/262144",
		},
		{
			// Difficulty clamped to 1.
			new(big.Int).SetInt64(5),
			new(big.Int).SetInt64(20),
			"26959946667150639794667015087019630673637144422540572481103610249215/1",
			"1/1",
		},
	}

	for _, test := range poolTgts {
		target, diff := calculatePoolTarget(chaincfg.MainNetParams(),
			test.hashRate, test.targetTime)
		expectedTarget, success := new(big.Rat).SetString(test.expectedTarget)
		if !success {
			t.Fatalf("failed to parse %v as a big.Int", test.expectedTarget)
		}

		if target.Cmp(expectedTarget) != 0 {
			t.Fatalf("for a hashrate of %v and a target time of %v seconds "+
				"the expected target is %v, got %v", test.hashRate,
				test.targetTime, expectedTarget, target)
		}

		expectedDiff, success := new(big.Rat).SetString(test.expectedDiff)
		if !success {
			t.Fatalf("failed to parse %v as a big.Int", test.expectedDiff)
		}

		if diff.Cmp(expectedDiff) != 0 {
			t.Fatalf("for a hashrate of %v and a target time of %v the "+
				"expected difficulty is %v, got %v", test.hashRate,
				test.targetTime, expectedDiff, diff)
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
		set := NewDifficultySet(net, powLimit, soloMaxGenTime)
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
