package pool

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	bolt "go.etcd.io/bbolt"
)

// fetchShare fetches the share referenced by the provided id.
func fetchShare(db *bolt.DB, id []byte) (*Share, error) {
	var share Share
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}
		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("no share found for id %s", string(id))
			return MakeError(ErrValueNotFound, desc, nil)
		}
		err = json.Unmarshal(v, &share)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &share, err
}

func testPaymentMgr(t *testing.T, db *bolt.DB) {
	activeNet := chaincfg.SimNetParams()
	pCfg := &PaymentMgrConfig{
		DB:            db,
		ActiveNet:     activeNet,
		PoolFee:       0.1,
		LastNPeriod:   time.Second * 120,
		SoloPool:      false,
		PaymentMethod: PPS,
		PoolFeeAddrs:  []dcrutil.Address{poolFeeAddrs},
	}
	mgr, err := NewPaymentMgr(pCfg)
	if err != nil {
		t.Fatalf("[NewPaymentMgr] unexpected error: %v", err)
	}

	// Test sharePercentages.
	shareSet := map[string]struct {
		input  []*Share
		output map[string]*big.Rat
		err    error
	}{
		"equal shares": {
			input: []*Share{
				NewShare("a", new(big.Rat).SetInt64(5)),
				NewShare("b", new(big.Rat).SetInt64(5)),
				NewShare("c", new(big.Rat).SetInt64(5)),
				NewShare("d", new(big.Rat).SetInt64(5)),
				NewShare("e", new(big.Rat).SetInt64(5)),
			},
			output: map[string]*big.Rat{
				"a": new(big.Rat).SetFrac64(5, 25),
				"b": new(big.Rat).SetFrac64(5, 25),
				"c": new(big.Rat).SetFrac64(5, 25),
				"d": new(big.Rat).SetFrac64(5, 25),
				"e": new(big.Rat).SetFrac64(5, 25),
			},
			err: nil,
		},
		"inequal shares": {
			input: []*Share{
				NewShare("a", new(big.Rat).SetInt64(5)),
				NewShare("b", new(big.Rat).SetInt64(10)),
				NewShare("c", new(big.Rat).SetInt64(15)),
				NewShare("d", new(big.Rat).SetInt64(20.0)),
				NewShare("e", new(big.Rat).SetInt64(25.0)),
			},
			output: map[string]*big.Rat{
				"a": new(big.Rat).SetFrac64(5, 75),
				"b": new(big.Rat).SetFrac64(10, 75),
				"c": new(big.Rat).SetFrac64(15, 75),
				"d": new(big.Rat).SetFrac64(20, 75),
				"e": new(big.Rat).SetFrac64(25, 75),
			},
			err: nil,
		},
		"zero shares": {
			input: []*Share{
				NewShare("a", new(big.Rat)),
				NewShare("b", new(big.Rat)),
				NewShare("c", new(big.Rat)),
				NewShare("d", new(big.Rat)),
				NewShare("e", new(big.Rat)),
			},
			output: nil,
			err:    MakeError(ErrDivideByZero, "division by zero", nil),
		},
	}

	for name, test := range shareSet {
		actual, err := mgr.sharePercentages(test.input)
		if err != test.err {
			var errCode ErrorCode
			var expectedCode ErrorCode

			if err != nil {
				e, ok := err.(Error)
				if ok {
					errCode = e.ErrorCode
				}
			}

			if test.err != nil {
				e, ok := test.err.(Error)
				if ok {
					expectedCode = e.ErrorCode
				}
			}

			if errCode.String() != expectedCode.String() {
				t.Fatalf("%s: error generated was %v, expected %v.",
					name, errCode.String(), expectedCode.String())
			}
		}

		for account, dividend := range test.output {
			if actual[account].Cmp(dividend) != 0 {
				t.Fatalf("%s: account %v dividend was %v, "+
					"expected %v.", name, account, actual[account], dividend)
			}
		}
	}

	// Test pruneShares.
	now := time.Now()
	zeroSource := &PaymentSource{
		BlockHash: chainhash.Hash{0}.String(),
		Coinbase:  chainhash.Hash{0}.String(),
	}
	minimumTime := now.Add(-(time.Second * 60)).UnixNano()
	maximumTime := now.UnixNano()
	aboveMaximumTime := now.Add(time.Second * 10).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)

	err = persistShare(db, xID, weight, minimumTime) // Share A
	if err != nil {
		t.Fatal(err)
	}

	err = persistShare(db, yID, weight, maximumTime) // Share B
	if err != nil {
		t.Fatal(err)
	}

	err = mgr.cfg.DB.Update(func(tx *bolt.Tx) error {
		return mgr.pruneShares(tx, maximumTime)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure share A got pruned with share B remaining.
	shareAID := shareID(xID, minimumTime)
	_, err = fetchShare(db, shareAID)
	if err == nil {
		t.Fatal("expected value not found error")
	}

	shareBID := shareID(yID, maximumTime)
	_, err = fetchShare(db, shareBID)
	if err != nil {
		t.Fatalf("unexpected error fetching share B: %v", err)
	}

	err = mgr.cfg.DB.Update(func(tx *bolt.Tx) error {
		return mgr.pruneShares(tx, aboveMaximumTime)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure share B got pruned.
	_, err = fetchShare(db, shareBID)
	if err == nil {
		t.Fatalf("expected value not found error")
	}

	// Test PPSEligibleShares and PPLNSEligibleShares.
	now = time.Now()
	minimumTime = now.Add(-(time.Second * 60)).UnixNano()
	maximumTime = now.UnixNano()
	belowMinimumTime := now.Add(-(time.Second * 80)).UnixNano()
	aboveMaximumTime = now.Add(time.Second * 10).UnixNano()
	weight = new(big.Rat).SetFloat64(1.0)

	shareCount := 1
	expectedShareCount := 2

	// Create a share below the PPS time range for account x.
	err = persistShare(db, xID, weight, belowMinimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum of the PPS time range for account x.
	err = persistShare(db, xID, weight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at maximum of the PPS time range for account y.
	err = persistShare(db, yID, weight, maximumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above maximum of the PPS time range for account y.
	err = persistShare(db, yID, weight, aboveMaximumTime)
	if err != nil {
		t.Fatal(err)
	}

	minimumTimeBytes := nanoToBigEndianBytes(minimumTime)
	maximumTimeBytes := nanoToBigEndianBytes(maximumTime)

	// Fetch eligible shares using the minimum and maximum time range.
	shares, err := mgr.PPSEligibleShares(minimumTimeBytes, maximumTimeBytes)
	if err != nil {
		t.Fatalf("PPSEligibleShares: unexpected error: %v", err)
	}

	// Ensure the returned share count is as expected.
	if len(shares) != expectedShareCount {
		t.Fatalf("PPS error: expected %v eligible PPS shares, got %v",
			expectedShareCount, len(shares))
	}

	forAccX := 0
	forAccY := 0
	for _, share := range shares {
		if share.Account == xID {
			forAccX++
		}

		if share.Account == yID {
			forAccY++
		}
	}

	// Ensure account x and account y both have shares returned.
	if forAccX == 0 || forAccY == 0 {
		t.Fatalf("PPS error: expected shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have equal number of shares.
	if forAccX != forAccY {
		t.Fatalf("PPS error: expected equal shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have shares equal to the share count.
	if forAccX != shareCount || forAccY != shareCount {
		t.Fatalf("PPS error: expected share counts of %v for account X and Y, "+
			"got %v (for x), %v (for y).", shareCount, forAccX, forAccY)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Create a share below the minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, belowMinimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share below the minimum exxcuive PPLNS time for account y.
	err = persistShare(db, yID, weight, belowMinimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum exclusive PPLNS time for account y.
	err = persistShare(db, yID, weight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, maximumTime)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above minimum exclusive PPLNS time for account y.
	err = persistShare(db, yID, weight, aboveMaximumTime)
	if err != nil {
		t.Fatal(err)
	}

	shares, err = mgr.PPLNSEligibleShares(minimumTimeBytes)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the returned number of shates is as expected.
	if len(shares) != expectedShareCount {
		t.Fatalf("PPLNS error: expected %v eligible PPLNS shares, got %v",
			expectedShareCount, len(shares))
	}

	// Ensure account x and account y both have shares returned.
	if forAccX == 0 || forAccY == 0 {
		t.Fatalf("PPLNS error: expected shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have equal number of shares.
	if forAccX != forAccY {
		t.Fatalf("PPLNS error: expected equal shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have shares equal to the share count.
	if forAccX != shareCount || forAccY != shareCount {
		t.Fatalf("PPLNS error: expected share counts of %v for account X and Y, "+
			"got %v (for x), %v (for y).", shareCount, forAccX, forAccY)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Test pendingPayments, pendingPaymentsAtHeight,
	// maturePendingPayments and archivedPayments.
	height := uint32(10)
	estMaturity := uint32(26)
	amt, _ := dcrutil.NewAmount(5)
	_, err = persistPayment(db, xID, zeroSource, amt, height+1, estMaturity+1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = persistPayment(db, xID, zeroSource, amt, height+1, estMaturity+1)
	if err != nil {
		t.Fatal(err)
	}

	pmtC, err := persistPayment(db, yID, zeroSource, amt, height, estMaturity)
	if err != nil {
		t.Fatal(err)
	}
	pmtC.PaidOnHeight = estMaturity + 1
	pmtC.TransactionID = chainhash.Hash{0}.String()
	err = pmtC.Update(db)
	if err != nil {
		t.Fatal(err)
	}
	err = pmtC.Archive(db)
	if err != nil {
		t.Fatal(err)
	}

	pmtD, err := persistPayment(db, yID, zeroSource, amt, height, estMaturity)
	if err != nil {
		t.Fatal(err)
	}
	pmtD.PaidOnHeight = estMaturity + 1
	pmtD.TransactionID = chainhash.Hash{0}.String()
	err = pmtD.Update(db)
	if err != nil {
		t.Fatal(err)
	}
	err = pmtD.Archive(db)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure there are two pending payments.
	pmts, err := mgr.pendingPayments()
	if err != nil {
		t.Fatalf("pendingPayments error: %v", err)
	}

	if len(pmts) != 2 {
		t.Fatalf("expected 2 pending payments, got %d", len(pmts))
	}

	// Ensure there are two pending payments at height 15.
	pmts, err = mgr.pendingPaymentsAtHeight(15)
	if err != nil {
		t.Fatalf("pendingPaymentsAtHeight error: %v", err)
	}

	if len(pmts) != 2 {
		t.Fatalf("expected 2 pending payments at height 15, got %d", len(pmts))
	}

	// Ensure there are no pending payments at height 8.
	pmts, err = mgr.pendingPaymentsAtHeight(8)
	if err != nil {
		t.Fatalf("pendingPaymentsAtHeight error: %v", err)
	}

	if len(pmts) != 0 {
		t.Fatalf("expected no pending payments at height 8, got %d", len(pmts))
	}

	// Ensure there are two archived payments (payment C and D).
	pmts, err = mgr.archivedPayments()
	if err != nil {
		t.Fatalf("archivedPayments error: %v", err)
	}

	if len(pmts) != 2 {
		t.Fatalf("expected 2 archived payments, got %d", len(pmts))
	}

	// Ensure there are two mature payments at height 28 (payment A and B).
	pmtSet, err := mgr.maturePendingPayments(28)
	if err != nil {
		t.Fatalf("maturePendingPayments error: %v", err)
	}

	if len(pmtSet) != 1 {
		t.Fatalf("expected 1 payment set, got %d", len(pmtSet))
	}

	set, ok := pmtSet[height+1]
	if !ok {
		t.Fatalf("expected pending payments at height %d to be "+
			"mature at height %d", height+1, 28)
	}

	if len(set) != 2 {
		t.Fatalf("expected 2 mature pending payments from "+
			"height %d, got %d", height, len(set))
	}

	// Ensure there are no mature payments at height 27 (payment A and B).
	pmtSet, err = mgr.maturePendingPayments(27)
	if err != nil {
		t.Fatalf("maturePendingPayments error: %v", err)
	}

	if len(pmtSet) != 0 {
		t.Fatalf("expected no payment sets, got %d", len(pmtSet))
	}

	// Empty the paymnts and archived payment buckets.
	err = emptyBucket(db, paymentArchiveBkt)
	if err != nil {
		t.Fatal(err)
	}
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure backed up values to the database persist and load as expected.
	err = db.Update(func(tx *bolt.Tx) error {
		err = mgr.loadLastPaymentHeight(tx)
		if err != nil {
			return fmt.Errorf("unable to load last payment height: %v", err)
		}
		err = mgr.loadLastPaymentCreatedOn(tx)
		if err != nil {
			return fmt.Errorf("unable to load last payment created on: %v", err)
		}
		return mgr.loadLastPaymentPaidOn(tx)
	})
	if err != nil {
		t.Fatal(err)
	}
	initialLastPaymentHeight := mgr.fetchLastPaymentHeight()
	initialLastPaymentPaidOn := mgr.fetchLastPaymentPaidOn()
	initialLastPaymentCreatedOn := mgr.fetchLastPaymentCreatedOn()
	if initialLastPaymentHeight != 0 {
		t.Fatalf("[fetchLastPaymentHeight] expected last payment height of "+
			" %d, got %d", 0, initialLastPaymentHeight)
	}
	if initialLastPaymentPaidOn != 0 {
		t.Fatalf("[fetchLastPaymentPaidOn] expected last payment paid on of "+
			" %d, got %d", 0, initialLastPaymentPaidOn)
	}
	if initialLastPaymentCreatedOn != 0 {
		t.Fatalf("[fetchLastPaymentCreatedOn] expected last payment created "+
			"on of %d, got %d", 0, initialLastPaymentCreatedOn)
	}

	lastPaymentHeight := uint32(1)
	mgr.setLastPaymentHeight(lastPaymentHeight)
	lastPaymentPaidOn := uint64(time.Now().UnixNano())
	mgr.setLastPaymentPaidOn(lastPaymentPaidOn)
	lastPaymentCreatedOn := uint64(time.Now().UnixNano())
	mgr.setLastPaymentCreatedOn(lastPaymentCreatedOn)
	err = db.Update(func(tx *bolt.Tx) error {
		err := mgr.persistLastPaymentHeight(tx)
		if err != nil {
			return fmt.Errorf("[persistLastPaymentHeight] unable to persist last payment height: %v", err)
		}
		err = mgr.persistLastPaymentPaidOn(tx)
		if err != nil {
			return fmt.Errorf("[persistLastPaymentPaidOn] unable to persist last payment paid on: %v", err)
		}
		err = mgr.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return fmt.Errorf("[persistLastPaymentCreatedOn] unable to persist last payment created on: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	err = db.View(func(tx *bolt.Tx) error {
		err := mgr.loadLastPaymentHeight(tx)
		if err != nil {
			return fmt.Errorf("[loadLastPaymentHeight] unable to load last payment height: %v", err)
		}
		err = mgr.loadLastPaymentPaidOn(tx)
		if err != nil {
			return fmt.Errorf("[loadLastPaymentPaidOn] unable to load last payment paid on: %v", err)
		}
		err = mgr.loadLastPaymentCreatedOn(tx)
		if err != nil {
			return fmt.Errorf("[loadLastPaymentCreatedOn] unable to load last payment created on: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	paymentHeight := mgr.fetchLastPaymentHeight()
	if lastPaymentHeight != paymentHeight {
		t.Fatalf("[fetchLastPaymentHeight] expected last payment height to be %d, got %d",
			paymentHeight, paymentHeight)
	}
	paymentPaidOn := mgr.fetchLastPaymentPaidOn()
	if lastPaymentPaidOn != paymentPaidOn {
		t.Fatalf("[fetchLastPaymentPaidOn] expected last payment paid on to be %d, got %d",
			lastPaymentPaidOn, paymentPaidOn)
	}
	paymentCreatedOn := mgr.fetchLastPaymentCreatedOn()
	if lastPaymentCreatedOn != paymentCreatedOn {
		t.Fatalf("[fetchLastPaymentCreatedOn] expected last payment created on to be %d, got %d",
			lastPaymentCreatedOn, paymentCreatedOn)
	}

	// Reset backed up values to their defaults.
	mgr.setLastPaymentHeight(0)
	mgr.setLastPaymentPaidOn(0)
	mgr.setLastPaymentCreatedOn(0)
	err = db.Update(func(tx *bolt.Tx) error {
		err := mgr.persistLastPaymentHeight(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment height: %v", err)
		}
		err = mgr.persistLastPaymentPaidOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment paid on: %v", err)
		}
		err = mgr.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment created on: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure Pay-Per-Share (PPS) works as expected.
	now = time.Now()
	sixtyBefore := now.Add(-(time.Second * 60)).UnixNano()
	thirtyBefore := now.Add(-(time.Second * 30)).UnixNano()
	shareCount = 10
	coinbaseValue := 80
	height = uint32(20)

	// Create shares for account x and y.
	for i := 0; i < shareCount; i++ {
		err := persistShare(db, xID, weight, sixtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
		err = persistShare(db, yID, weight, thirtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	coinbase, err := dcrutil.NewAmount(float64(coinbaseValue))
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the last payment created on time was updated.
	previousPaymentCreatedOn := int64(mgr.fetchLastPaymentCreatedOn())
	err = mgr.generatePayments(height, zeroSource, coinbase)
	if err != nil {
		t.Fatalf("[PPS] unable to generate payments: %v", err)
	}
	currentPaymentCreatedOn := int64(mgr.fetchLastPaymentCreatedOn())
	if currentPaymentCreatedOn < now.UnixNano() {
		t.Fatalf("[PPS] expected last payment created on time to "+
			"be greater than %v,got %v", now, currentPaymentCreatedOn)
	}
	if currentPaymentCreatedOn < previousPaymentCreatedOn {
		t.Fatalf("[PPS] expected last payment created on time to "+
			"be greater than %v,got %v", previousPaymentCreatedOn,
			currentPaymentCreatedOn)
	}

	// Ensure the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err = mgr.pendingPayments()
	if err != nil {
		t.Error(err)
	}

	var xt, yt, ft dcrutil.Amount
	for _, pmt := range pmts {
		if pmt.Account == xID {
			xt += pmt.Amount
		}
		if pmt.Account == yID {
			yt += pmt.Amount
		}
		if pmt.Account == poolFeesK {
			ft += pmt.Amount
		}
	}

	// Ensure the two account payments have the same payments since
	// they have the same share weights.
	if xt != yt {
		t.Fatalf("[PPS] expected equal account amounts, %v != %v", xt, yt)
	}

	// Ensure the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt := coinbase.MulF64(mgr.cfg.PoolFee)
	if ft != expectedFeeAmt {
		t.Fatalf("[PPS] expected %v fee payment amount, got %v",
			ft, expectedFeeAmt)
	}

	// Ensure the sum of all payment amounts is equal to the initial
	// coinbase amount.
	sum := xt + yt + ft
	if sum != coinbase {
		t.Fatalf("[PPS] expected the sum of all payments to be %v, got %v",
			coinbase, sum)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("[PPS] emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("[PPS] emptyBucket error: %v", err)
	}

	// Reset backed up values to their defaults.
	mgr.setLastPaymentHeight(0)
	mgr.setLastPaymentPaidOn(0)
	mgr.setLastPaymentCreatedOn(0)
	err = db.Update(func(tx *bolt.Tx) error {
		err := mgr.persistLastPaymentHeight(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment height: %v", err)
		}
		err = mgr.persistLastPaymentPaidOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment paid on: %v", err)
		}
		err = mgr.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment created on: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure Pay-Per-Last-N-Shares (PPLNS) works as expected.
	pCfg.PaymentMethod = PPLNS
	shareCount = 5
	coinbaseValue = 60

	// Create shares for account x and y.
	for i := 0; i < shareCount; i++ {
		err := persistShare(db, xID, weight, sixtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
		err = persistShare(db, yID, weight, thirtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	coinbase, err = dcrutil.NewAmount(float64(coinbaseValue))
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}

	// Ensure the last payment created on time was updated.
	previousPaymentCreatedOn = int64(mgr.fetchLastPaymentCreatedOn())
	err = mgr.generatePayments(height, zeroSource, coinbase)
	if err != nil {
		t.Fatalf("[PPLNS] unable to generate payments: %v", err)
	}
	currentPaymentCreatedOn = int64(mgr.fetchLastPaymentCreatedOn())
	if currentPaymentCreatedOn < now.UnixNano() {
		t.Fatalf("[PPLNS] expected last payment created on time "+
			"to be greater than %v,got %v", now, currentPaymentCreatedOn)
	}
	if currentPaymentCreatedOn < previousPaymentCreatedOn {
		t.Fatalf("[PPLNS] expected last payment created on time "+
			"to be greater than %v,got %v", previousPaymentCreatedOn,
			currentPaymentCreatedOn)
	}

	// Ensure the payments created are for accounts x, y and a fee
	// payment entry.
	pmts, err = mgr.pendingPayments()
	if err != nil {
		t.Fatalf("[PPLNS] fetchPendingPayments error: %v", err)
	}

	xt = dcrutil.Amount(0)
	yt = dcrutil.Amount(0)
	ft = dcrutil.Amount(0)
	for _, pmt := range pmts {
		if pmt.Account == xID {
			xt += pmt.Amount
		}
		if pmt.Account == yID {
			yt += pmt.Amount
		}
		if pmt.Account == poolFeesK {
			ft += pmt.Amount
		}
	}

	// Ensure the two account payments have the same payments since
	// they have the same share weights.
	if xt != yt {
		t.Fatalf("[PPLNS] expected equal account amounts, %v != %v", xt, yt)
	}

	// Ensure the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt = coinbase.MulF64(mgr.cfg.PoolFee)
	if ft != expectedFeeAmt {
		t.Fatalf("[PPLNS] expected %v fee payment amount, got %v",
			ft, expectedFeeAmt)
	}

	// Ensure the sum of all payment amounts is equal to the initial
	// amount.
	sum = xt + yt + ft
	if sum != coinbase {
		t.Fatalf("[PPLNS] expected the sum of all payments to be %v, got %v",
			coinbase, sum)
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("[PPLNS] emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("[PPLNS] emptyBucket error: %v", err)
	}

	// Reset backed up values to their defaults.
	mgr.setLastPaymentHeight(0)
	mgr.setLastPaymentPaidOn(0)
	mgr.setLastPaymentCreatedOn(0)
	err = db.Update(func(tx *bolt.Tx) error {
		err := mgr.persistLastPaymentHeight(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment height: %v", err)
		}
		err = mgr.persistLastPaymentPaidOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment paid on: %v", err)
		}
		err = mgr.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment created on: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	paymentMaturity := height + uint32(activeNet.CoinbaseMaturity+1)

	// Ensure payment maturity works as expected.
	for i := 0; i < shareCount; i++ {
		// Create readily available shares for account X.
		err = persistShare(db, xID, weight, thirtyBefore)
		if err != nil {
			t.Fatal(err)
		}
	}
	sixtyAfter := time.Now().Add((time.Second * 60)).UnixNano()
	for i := 0; i < shareCount; i++ {
		// Create future shares for account Y.
		err = persistShare(db, yID, weight, sixtyAfter)
		if err != nil {
			t.Fatal(err)
		}
	}

	err = mgr.generatePayments(height, zeroSource, coinbase)
	if err != nil {
		t.Fatalf("unable to generate payments: %v", err)
	}

	// Ensure payments for  for account x, y and fees were created.
	pmtSets, err := mgr.maturePendingPayments(paymentMaturity)
	if err != nil {
		t.Fatalf("[maturePendingPayments] unexpected error: %v", err)
	}

	if len(pmtSets) == 0 {
		t.Fatal("[maturePendingPayments] expected mature payments")
	}

	_, ok = pmtSets[height]
	if !ok {
		t.Fatalf("[maturePendingPayments] expected mature payments "+
			"at height %d", height)
	}

	xt = dcrutil.Amount(0)
	yt = dcrutil.Amount(0)
	ft = dcrutil.Amount(0)
	for _, pmt := range pmts {
		if pmt.Account == xID {
			xt += pmt.Amount
		}
		if pmt.Account == yID {
			yt += pmt.Amount
		}
		if pmt.Account == poolFeesK {
			ft += pmt.Amount
		}
	}

	// Ensure the two account payments have the same payments since
	// they have the same share weights.
	if xt != yt {
		t.Fatalf("[PPLNS] expected equal account amounts, %v != %v", xt, yt)
	}

	expectedFeeAmt = coinbase.MulF64(mgr.cfg.PoolFee)
	if ft != expectedFeeAmt {
		t.Fatalf("expected pool fee payment total to have %v, got %v",
			expectedFeeAmt, ft)
	}

	// TODO: Add tests for payDividend.

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Empty the payment bucket.
	err = emptyBucket(db, paymentBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Reset backed up values to their defaults.
	mgr.setLastPaymentHeight(0)
	mgr.setLastPaymentPaidOn(0)
	mgr.setLastPaymentCreatedOn(0)
	err = db.Update(func(tx *bolt.Tx) error {
		err := mgr.persistLastPaymentHeight(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment height: %v", err)
		}
		err = mgr.persistLastPaymentPaidOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment paid on: %v", err)
		}
		err = mgr.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default last payment created on: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
