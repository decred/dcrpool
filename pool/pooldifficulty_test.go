package pool

import (
	"math/big"
	"testing"

	"github.com/decred/dcrd/chaincfg/v2"
)

func testPoolDifficulty(t *testing.T) {
	set := []struct {
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

	for idx, tc := range set {
		net := chaincfg.SimNetParams()
		powLimit := new(big.Rat).SetInt(net.PowLimit)
		pd, err := NewPoolDifficulty(net, powLimit, soloMaxGenTime)
		if err != nil {
			t.Fatalf("[NewPoolDiffiiculty] #%d, unexpected error %v", idx+1, err)
		}

		diffInfo, err := pd.fetchMinerDifficulty(tc.miner)
		if (err != nil) != tc.wantErr {
			t.Fatalf("[FetchMinerDifficulty] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}

		if !tc.wantErr {
			if diffInfo == nil {
				t.Fatalf("[FetchMinerDifficulty] #%d: expected valid "+
					"difficulty info for %s", idx+1, tc.miner)
			}
		}
	}
}
