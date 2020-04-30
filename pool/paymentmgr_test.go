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
	minPayment, err := dcrutil.NewAmount(2.0)
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}
	maxTxFeeReserve, err := dcrutil.NewAmount(0.1)
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}
	activeNet := chaincfg.SimNetParams()
	pCfg := &PaymentMgrConfig{
		DB:              db,
		ActiveNet:       activeNet,
		PoolFee:         0.1,
		LastNPeriod:     time.Second * 120,
		SoloPool:        false,
		PaymentMethod:   PPS,
		MinPayment:      minPayment,
		PoolFeeAddrs:    []dcrutil.Address{poolFeeAddrs},
		MaxTxFeeReserve: maxTxFeeReserve,
		PublishTransaction: func(map[dcrutil.Address]dcrutil.Amount, dcrutil.Amount) (string, error) {
			return "", nil
		},
	}
	mgr, err := NewPaymentMgr(pCfg)
	if err != nil {
		t.Fatalf("[NewPaymentMgr] unexpected error: %v", err)
	}

	// Test pruneShares.
	now := time.Now()
	minimumTime := now.Add(-(time.Second * 60)).UnixNano()
	maximumTime := now.UnixNano()
	aboveMaximumTime := now.Add(time.Second * 10).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)

	_, err = persistShare(db, xID, weight, minimumTime)
	if err != nil {
		t.Fatal(err)
	}

	_, err = persistShare(db, yID, weight, maximumTime)
	if err != nil {
		t.Fatal(err)
	}

	err = mgr.cfg.DB.Update(func(tx *bolt.Tx) error {
		return mgr.pruneShares(tx, maximumTime)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure share A got pruned.
	_, err = fetchShare(db, nanoToBigEndianBytes(minimumTime))
	if err == nil {
		t.Fatalf("expected value not found error")
	}

	err = mgr.cfg.DB.Update(func(tx *bolt.Tx) error {
		return mgr.pruneShares(tx, aboveMaximumTime)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure share B got pruned.
	_, err = fetchShare(db, nanoToBigEndianBytes(maximumTime))
	if err == nil {
		t.Fatalf("expected value not found error")
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
		err = mgr.loadLastPaymentPaidOn(tx)
		if err != nil {
			return fmt.Errorf("unable to load last payment paid on: %v", err)
		}
		err = mgr.loadTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("unable to load tx fee reserve: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	initialLastPaymentHeight := mgr.fetchLastPaymentHeight()
	initialLastPaymentPaidOn := mgr.fetchLastPaymentPaidOn()
	initialLastPaymentCreatedOn := mgr.fetchLastPaymentCreatedOn()
	initialTxFeeReserve := mgr.fetchTxFeeReserve()
	zeroAmount := dcrutil.Amount(0)
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
	if initialTxFeeReserve != zeroAmount {
		t.Fatalf("[fetchTxFeeReserve] expected last payment height of "+
			" %d, got %d", initialTxFeeReserve, zeroAmount)
	}

	lastPaymentHeight := uint32(1)
	mgr.setLastPaymentHeight(lastPaymentHeight)
	lastPaymentPaidOn := uint64(time.Now().UnixNano())
	mgr.setLastPaymentPaidOn(lastPaymentPaidOn)
	lastPaymentCreatedOn := uint64(time.Now().UnixNano())
	mgr.setLastPaymentCreatedOn(lastPaymentCreatedOn)
	feeReserve, err := dcrutil.NewAmount(0.02)
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}
	mgr.setTxFeeReserve(feeReserve)
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
		err = mgr.persistTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("[persistTxFeeReserve] unable to persist tx fee reserve: %v", err)
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
		err = mgr.loadTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("[loadTxFeeReserve] unable to load tx fee reserve: %v", err)
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
	txFeeReserve := mgr.fetchTxFeeReserve()
	if feeReserve != txFeeReserve {
		t.Fatalf("[fetchTxFeeReserve] expected tx fee reserve to be %d, got %d",
			feeReserve, txFeeReserve)
	}

	// Ensure the tx fee reserve can be replenished partially and fully.
	feeA, err := dcrutil.NewAmount(0.05)
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}
	updatedFeeA := mgr.replenishTxFeeReserve(feeA)
	if updatedFeeA != zeroAmount {
		t.Fatalf("[replenishTxFeeReserve] expected fees after replenishing "+
			"with feeA to be %d, got %d", zeroAmount, updatedFeeA)
	}
	updatedTFR := mgr.fetchTxFeeReserve()
	if updatedTFR != feeReserve+feeA {
		t.Fatalf("[replenishTxFeeReserve] expected updated tx fee reserve "+
			"after feeA replenish to be %d, got %d", feeReserve+feeA, updatedTFR)
	}

	txFeeReserve = mgr.fetchTxFeeReserve()
	feeB, err := dcrutil.NewAmount(2)
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}
	updatedFeeB := mgr.replenishTxFeeReserve(feeB)
	if updatedFeeB >= feeB {
		t.Fatalf("[replenishTxFeeReserve] expected fees after replenishing "+
			" with feeB to be %d, got %d", feeB-(maxTxFeeReserve-txFeeReserve), updatedFeeB)
	}
	updatedTFR = mgr.fetchTxFeeReserve()
	if updatedTFR != maxTxFeeReserve {
		t.Fatalf("[replenishTxFeeReserve] expected updated tx fee reserve "+
			"after feeB replenish to be %d, got %d", maxTxFeeReserve, updatedTFR)
	}

	// Reset backed up values to their defaults.
	mgr.setLastPaymentHeight(0)
	mgr.setLastPaymentPaidOn(0)
	mgr.setLastPaymentCreatedOn(0)
	mgr.setTxFeeReserve(zeroAmount)
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
		err = mgr.persistTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default tx fee reserve: %v", err)
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
	shareCount := 10
	coinbaseValue := 80
	height := uint32(20)
	paymentMaturity := height + uint32(activeNet.CoinbaseMaturity)

	// Create shares for account x and y.
	for i := 0; i < shareCount; i++ {
		_, err := persistShare(db, xID, weight, sixtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
		_, err = persistShare(db, yID, weight, thirtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	zeroSource := &PaymentSource{
		BlockHash: chainhash.Hash{0}.String(),
		Coinbase:  chainhash.Hash{0}.String(),
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
	pmts, err := fetchPendingPayments(db)
	if err != nil {
		t.Error(err)
	}
	expectedBundleCount := 3
	bundles := generatePaymentBundles(pmts)
	if len(bundles) != expectedBundleCount {
		t.Fatalf("[PPS] expected %v payment bundles, got %v.",
			expectedBundleCount, len(bundles))
	}

	var xb, yb, fb *PaymentBundle
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Account == xID {
			xb = bundles[idx]
		}
		if bundles[idx].Account == yID {
			yb = bundles[idx]
		}
		if bundles[idx].Account == poolFeesK {
			fb = bundles[idx]
		}
	}

	// Ensure the two account payment bundles have the same payments since
	// they have the same share weights.
	if xb.Total() != yb.Total() {
		t.Fatalf("[PPS] expected equal account amounts, %v != %v",
			xb.Total(), yb.Total())
	}

	// Ensure the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt := coinbase.MulF64(mgr.cfg.PoolFee)
	if fb.Total() != expectedFeeAmt {
		t.Fatalf("[PPS]expected %v fee payment amount, got %v",
			fb.Total(), expectedFeeAmt)
	}

	// Ensure the sum of all payment bundle amounts is equal to the initial
	// coinbase amount.
	sum := xb.Total() + yb.Total() + fb.Total()
	if sum != coinbase {
		t.Fatalf("[PPS] expected the sum of all payments to be %v, got %v", coinbase, sum)
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
	mgr.setTxFeeReserve(zeroAmount)
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
		err = mgr.persistTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default tx fee reserve: %v", err)
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
		_, err := persistShare(db, xID, weight, sixtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
		_, err = persistShare(db, yID, weight, thirtyBefore+int64(i))
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
	pmts, err = fetchPendingPayments(db)
	if err != nil {
		t.Fatalf("[PPLNS] fetchPendingPayments error: %v", err)
	}
	bundles = generatePaymentBundles(pmts)
	if len(bundles) != expectedBundleCount {
		t.Fatalf("[PPLNS] expected %v payment bundles, got %v.",
			expectedBundleCount, len(bundles))
	}

	xb = nil
	yb = nil
	fb = nil
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Account == xID {
			xb = bundles[idx]
		}
		if bundles[idx].Account == yID {
			yb = bundles[idx]
		}
		if bundles[idx].Account == poolFeesK {
			fb = bundles[idx]
		}
	}

	// Ensure the two account payment bundles have the same payments since
	// they have the same share weights.
	if xb.Total() != yb.Total() {
		t.Fatalf("[PPLNS] expected equal account amounts, %v != %v",
			xb.Total(), yb.Total())
	}

	// Ensure the fee payment is the exact fee percentage of the total amount.
	expectedFeeAmt = coinbase.MulF64(mgr.cfg.PoolFee)
	if fb.Total() != expectedFeeAmt {
		t.Fatalf("[PPLNS] expected %v fee payment amount, got %v",
			fb.Total(), expectedFeeAmt)
	}

	// Ensure the sum of all payment bundle amounts is equal to the initial
	// amount.
	sum = xb.Total() + yb.Total() + fb.Total()
	if sum != coinbase {
		t.Fatalf("[PPLNS] expected the sum of all payments to be %v, got %v", coinbase, sum)
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
	mgr.setTxFeeReserve(zeroAmount)
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
		err = mgr.persistTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default tx fee reserve: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure minimum processing payment amount is enforced.
	pCfg.PaymentMethod = PPS
	xShareCount := 10
	yShareCount := 5

	// Create shares for account x and Y.
	for i := 0; i < xShareCount; i++ {
		_, err := persistShare(db, xID, weight, sixtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < yShareCount; i++ {
		_, err := persistShare(db, yID, weight, thirtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Generate payments.
	err = mgr.generatePayments(height, zeroSource, coinbase)
	if err != nil {
		t.Fatalf("unable to generate payments: %v", err)
	}

	// Make minimum payment greater than account Y's payment.
	ratio := (float64(yShareCount) / float64(xShareCount+yShareCount))
	pCfg.MinPayment = coinbase.MulF64(ratio)
	if err != nil {
		t.Fatalf("[MulF64] unexpected error: %v", err)
	}
	bundles, err = mgr.fetchEligiblePaymentBundles(paymentMaturity)
	if err != nil {
		t.Fatalf("[fetchEligiblePaymentBundles] unexpected error: %v", err)
	}
	expectedBundleCount = 1
	if len(bundles) != expectedBundleCount {
		t.Fatalf("expected %v payment bundles, got %v", expectedBundleCount, len(bundles))
	}

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
	mgr.setTxFeeReserve(zeroAmount)
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
		err = mgr.persistTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default tx fee reserve: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure payment requests work as expected.
	for i := 0; i < xShareCount; i++ {
		// Create shares for account x and Y.
		_, err := persistShare(db, xID, weight, sixtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < yShareCount; i++ {
		_, err := persistShare(db, yID, weight, thirtyBefore+int64(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Generate readily available payments.
	err = mgr.generatePayments(height, zeroSource, coinbase)
	if err != nil {
		t.Fatalf("unable to generate payments: %v", err)
	}

	// Ensure minimum payment allowed is greater than account Y's payment.
	pCfg.MinPayment = coinbase.MulF64(ratio)
	if err != nil {
		t.Fatalf("[MulF64] unexpected error: %v", err)
	}

	// Create a payment request for account Y.
	err = mgr.addPaymentRequest(yAddr)
	if err != nil {
		t.Fatalf("[AddPaymentRequest] unexpected error: %v", err)
	}

	// Ensure the requested payment for account Y is returned as eligible.
	bundles, err = mgr.fetchEligiblePaymentBundles(paymentMaturity)
	if err != nil {
		t.Fatalf("[fetchEligiblePaymentBundles] unexpected error: %v", err)
	}
	expectedBundleCount = 2
	if len(bundles) != expectedBundleCount {
		t.Fatalf("expected %v payment bundles, got %v", expectedBundleCount, len(bundles))
	}

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
	mgr.setTxFeeReserve(zeroAmount)
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
		err = mgr.persistTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default tx fee reserve: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Ensure payment maturity works as expected.
	pCfg.MinPayment = minPayment
	for i := 0; i < shareCount; i++ {
		// Create readily available shares for account X.
		_, err = persistShare(db, xID, weight, thirtyBefore)
		if err != nil {
			t.Fatal(err)
		}
	}
	sixtyAfter := time.Now().Add((time.Second * 60)).UnixNano()
	for i := 0; i < shareCount; i++ {
		// Create future shares for account Y.
		_, err = persistShare(db, yID, weight, sixtyAfter)
		if err != nil {
			t.Fatal(err)
		}
	}

	err = mgr.generatePayments(height, zeroSource, coinbase)
	if err != nil {
		t.Fatalf("unable to generate payments: %v", err)
	}

	// Ensure only bundles for account x and fees were created.
	bundles, err = mgr.fetchEligiblePaymentBundles(paymentMaturity)
	if err != nil {
		t.Fatalf("[fetchEligiblePaymentBundles] unexpected error: %v", err)
	}
	expectedBundleCount = 2
	if len(bundles) != expectedBundleCount {
		t.Fatalf("expected %v payment bundles, got %v", expectedBundleCount, len(bundles))
	}

	xb = nil
	yb = nil
	fb = nil
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Account == xID {
			xb = bundles[idx]
		}
		if bundles[idx].Account == yID {
			yb = bundles[idx]
		}
		if bundles[idx].Account == poolFeesK {
			fb = bundles[idx]
		}
	}

	// Ensure there are no bundles for account Y.
	if yb != nil {
		t.Fatalf("expected no payment bundles for account Y")
	}

	// Ensure the bundle amounts are as expected.
	expectedXAmt := coinbase - coinbase.MulF64(mgr.cfg.PoolFee)
	if xb.Total() != expectedXAmt {
		t.Fatalf("expected account X's bundle to have %v, got %v",
			expectedXAmt, xb.Total())
	}

	expectedFeeAmt = coinbase.MulF64(mgr.cfg.PoolFee)
	if fb.Total() != expectedFeeAmt {
		t.Fatalf("expected pool fee bundle to have %v, got %v",
			expectedFeeAmt, fb.Total())
	}

	// Ensure dividend payments work as expected.
	lastPaymentHeight = mgr.fetchLastPaymentHeight()
	err = mgr.payDividends(paymentMaturity)
	if err != nil {
		t.Fatalf("[payDividends] unexpected error: %v", err)
	}

	// Ensure the last payment height changed because
	// dividends were paid.
	currentPaymentHeight := mgr.fetchLastPaymentHeight()
	if lastPaymentHeight == currentPaymentHeight {
		t.Fatal("expected an updated payment height")
	}

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
	mgr.setTxFeeReserve(zeroAmount)
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
		err = mgr.persistTxFeeReserve(tx)
		if err != nil {
			return fmt.Errorf("unable to persist default tx fee reserve: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
