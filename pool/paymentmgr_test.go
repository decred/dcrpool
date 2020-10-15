package pool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"decred.org/dcrwallet/rpc/walletrpc"
	txrules "decred.org/dcrwallet/wallet/txrules"
	"decred.org/dcrwallet/wallet/txsizes"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/grpc"
)

type txCreatorImpl struct {
	getBlock             func(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
	getTxOut             func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error)
	createRawTransaction func(ctx context.Context, inputs []chainjson.TransactionInput, amounts map[dcrutil.Address]dcrutil.Amount, lockTime *int64, expiry *int64) (*wire.MsgTx, error)
}

// GetBlock fetches the block associated with the provided block hash.
func (txC *txCreatorImpl) GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	return txC.getBlock(ctx, blockHash)
}

// GetTxOut fetches the output referenced by the provided txHash and index.
func (txC *txCreatorImpl) GetTxOut(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
	return txC.getTxOut(ctx, txHash, index, mempool)
}

// CreateRawTransaction generates a transaction from the provided inputs and payouts.
func (txC *txCreatorImpl) CreateRawTransaction(ctx context.Context, inputs []chainjson.TransactionInput,
	amounts map[dcrutil.Address]dcrutil.Amount, lockTime *int64, expiry *int64) (*wire.MsgTx, error) {
	return txC.createRawTransaction(ctx, inputs, amounts, lockTime, expiry)
}

type txBroadcasterImpl struct {
	signTransaction    func(ctx context.Context, req *walletrpc.SignTransactionRequest, options ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error)
	publishTransaction func(ctx context.Context, req *walletrpc.PublishTransactionRequest, options ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error)
}

// SignTransaction signs transaction inputs, unlocking them for use.
func (txB *txBroadcasterImpl) SignTransaction(ctx context.Context, req *walletrpc.SignTransactionRequest, options ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error) {
	return txB.signTransaction(ctx, req, options...)
}

// PublishTransaction broadcasts the transaction unto the network.
func (txB *txBroadcasterImpl) PublishTransaction(ctx context.Context, req *walletrpc.PublishTransactionRequest, options ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error) {
	return txB.publishTransaction(ctx, req, options...)
}

// fetchShare fetches the share referenced by the provided id.
func fetchShare(db *bolt.DB, id string) (*Share, error) {
	var share Share
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		v := bkt.Get([]byte(id))
		if v == nil {
			return fmt.Errorf("no share found for id %s", id)
		}
		err = json.Unmarshal(v, &share)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &share, err
}

func TestSharePercentages(t *testing.T) {
	mgr := PaymentMgr{}

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
			err:    poolError(ErrDivideByZero, "division by zero"),
		},
	}

	for name, test := range shareSet {
		actual, err := mgr.sharePercentages(test.input)
		if !errors.Is(err, test.err) {
			t.Fatalf("%s: error generated was %v, expected %v.",
				name, err, test.err)
		}

		for account, dividend := range test.output {
			if actual[account].Cmp(dividend) != 0 {
				t.Fatalf("%s: account %v dividend was %v, "+
					"expected %v.", name, account, actual[account], dividend)
			}
		}
	}
}

func testPaymentMgr(t *testing.T) {
	activeNet := chaincfg.SimNetParams()

	getBlockConfirmations := func(context.Context, *chainhash.Hash) (int64, error) {
		return -1, nil
	}

	fetchTxCreator := func() TxCreator {
		return nil
	}

	fetchTxBroadcaster := func() TxBroadcaster {
		return nil
	}

	pCfg := &PaymentMgrConfig{
		DB:                    db,
		ActiveNet:             activeNet,
		PoolFee:               0.1,
		LastNPeriod:           time.Second * 120,
		SoloPool:              false,
		PaymentMethod:         PPS,
		GetBlockConfirmations: getBlockConfirmations,
		FetchTxCreator:        fetchTxCreator,
		FetchTxBroadcaster:    fetchTxBroadcaster,
		PoolFeeAddrs:          []dcrutil.Address{poolFeeAddrs},
	}
	mgr, err := NewPaymentMgr(pCfg)
	if err != nil {
		t.Fatalf("[NewPaymentMgr] unexpected error: %v", err)
	}

	// Ensure Pay-Per-Share (PPS) works as expected.
	now := time.Now()
	sixtyBefore := now.Add(-(time.Second * 60)).UnixNano()
	thirtyBefore := now.Add(-(time.Second * 30)).UnixNano()
	shareCount := 10
	coinbaseValue := 80
	height := uint32(20)
	weight := new(big.Rat).SetFloat64(1.0)

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

	zeroHash := chainhash.Hash{0}
	zeroSource := &PaymentSource{
		BlockHash: zeroHash.String(),
		Coinbase:  zeroHash.String(),
	}

	// Ensure the last payment created on time was updated.
	previousPaymentCreatedOn, err := loadLastPaymentCreatedOn(db)
	if err != nil {
		t.Fatalf("[PPS] unable to get previous payment created-on: %v", err)
	}
	err = mgr.generatePayments(height, zeroSource, coinbase, now.UnixNano())
	if err != nil {
		t.Fatalf("[PPS] unable to generate payments: %v", err)
	}
	currentPaymentCreatedOn, err := loadLastPaymentCreatedOn(db)
	if err != nil {
		t.Fatalf("[PPS] unable to get current payment created-on: %v", err)
	}
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

	var xt, yt, ft dcrutil.Amount
	for _, pmt := range pmts {
		if pmt.Account == xID {
			xt += pmt.Amount
		}
		if pmt.Account == yID {
			yt += pmt.Amount
		}
		if pmt.Account == PoolFeesK {
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
	err = persistLastPaymentInfo(db, 0, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment info: %v", err)
	}
	err = persistLastPaymentCreatedOn(db, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment created on: %v", err)
	}

	// Ensure Pay-Per-Last-N-Shares (PPLNS) works as expected.
	now = time.Now()
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
	previousPaymentCreatedOn, err = loadLastPaymentCreatedOn(db)
	if err != nil {
		t.Fatalf("[PPLNS] unable to get previous payment created-on: %v", err)
	}
	err = mgr.generatePayments(height, zeroSource, coinbase, now.UnixNano())
	if err != nil {
		t.Fatalf("[PPLNS] unable to generate payments: %v", err)
	}
	currentPaymentCreatedOn, err = loadLastPaymentCreatedOn(db)
	if err != nil {
		t.Fatalf("[PPLNS] unable to get current payment created-on: %v", err)
	}
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
		t.Fatalf("[PPLNS] pendingPayments error: %v", err)
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
		if pmt.Account == PoolFeesK {
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
	err = persistLastPaymentInfo(db, 0, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment info: %v", err)
	}
	err = persistLastPaymentCreatedOn(db, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment created on: %v", err)
	}

	now = time.Now()
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

	err = mgr.generatePayments(height, zeroSource, coinbase, now.UnixNano())
	if err != nil {
		t.Fatalf("unable to generate payments: %v", err)
	}

	// Ensure payments for account x, y and fees were created.
	pmtSets, err := maturePendingPayments(db, paymentMaturity)
	if err != nil {
		t.Fatalf("[maturePendingPayments] unexpected error: %v", err)
	}

	if len(pmtSets) == 0 {
		t.Fatal("[maturePendingPayments] expected mature payments")
	}

	_, ok := pmtSets[zeroSource.BlockHash]
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
		if pmt.Account == PoolFeesK {
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
	err = persistLastPaymentInfo(db, 0, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment info: %v", err)
	}
	err = persistLastPaymentCreatedOn(db, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment created on: %v", err)
	}

	// pruneOrphanedPayments tests.
	var randBytes [chainhash.HashSize + 1]byte
	_, err = rand.Read(randBytes[:])
	if err != nil {
		t.Fatalf("unable to generate random bytes: %v", err)
	}

	estMaturity := uint32(26)
	randHash := chainhash.HashH(randBytes[:])
	randSource := &PaymentSource{
		BlockHash: randHash.String(),
		Coinbase:  randHash.String(),
	}
	amt, _ := dcrutil.NewAmount(5)
	mPmts := make(map[string][]*Payment)
	pmtA := NewPayment(xID, zeroSource, amt, height, estMaturity)
	mPmts[zeroSource.Coinbase] = []*Payment{pmtA}
	pmtB := NewPayment(yID, randSource, amt, height, estMaturity)
	mPmts[randSource.Coinbase] = []*Payment{pmtB}

	ctx, cancel := context.WithCancel(context.Background())

	// Ensure orphaned payments pruning returns an error if it cannot
	// confirm a block.
	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		return 0, fmt.Errorf("unable to confirm block")
	}
	_, err = mgr.pruneOrphanedPayments(ctx, mPmts)
	if err == nil {
		cancel()
		t.Fatal("expected a block confirmation error")
	}

	// Create an invalid block hash / payment set entry.
	invalidBlockHash := "0123456789012345678901234567890123456789" +
		"0123456789012345678912345"
	mPmts[invalidBlockHash] = []*Payment{pmtB}

	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		if bh.String() != zeroSource.BlockHash {
			return -1, nil
		}
		return 16, nil
	}

	// Ensure orphaned payments pruning returns an errors if it encounters
	// an invalid block hash as a key.
	_, err = mgr.pruneOrphanedPayments(ctx, mPmts)
	if !errors.Is(err, ErrCreateHash) {
		cancel()
		t.Fatalf("expected a hash error, got %v", err)
	}

	// remove the invalid block hash key pair.
	delete(mPmts, invalidBlockHash)

	// Ensure orphaned payments pruning accurately prunes payments
	// sourcing from orphaned blocks.
	pmtSet, err := mgr.pruneOrphanedPayments(ctx, mPmts)
	if err != nil {
		cancel()
		t.Fatalf("unexpected pruneOrphanPayments error: %v", err)
	}
	if len(pmtSet) != 1 {
		cancel()
		t.Fatalf("expected a single valid mature payment after "+
			"pruning, got %v", len(pmtSet))
	}

	// applyTxFee tests.
	outV, _ := dcrutil.NewAmount(100)
	in := chainjson.TransactionInput{
		Amount: float64(outV),
		Txid:   chainhash.Hash{1}.String(),
		Vout:   2,
		Tree:   wire.TxTreeRegular,
	}

	poolFeeValue := amt.MulF64(0.1)
	xValue := amt.MulF64(0.6)
	yValue := amt.MulF64(0.3)

	feeAddr := poolFeeAddrs.String()
	out := make(map[string]dcrutil.Amount)
	out[xAddr] = xValue
	out[yAddr] = yValue
	out[feeAddr] = poolFeeValue

	_, txFee, err := mgr.applyTxFees([]chainjson.TransactionInput{in},
		out, outV, poolFeeAddrs)
	if err != nil {
		t.Fatalf("unexpected applyTxFees error: %v", err)
	}

	// Ensure the pool fee payment was exempted from tx fee deductions.
	if out[feeAddr] != poolFeeValue {
		t.Fatalf("expected pool fee payment to be %v, got %v",
			poolFeeValue, out[feeAddr])
	}

	// Ensure the difference between initial account payments and updated
	// account payments plus the transaction fee is not more than the
	// maximum rounding difference.
	initialAccountPayments := xValue + yValue
	updatedAccountPaymentsPlusTxFee := out[xAddr] + out[yAddr] + txFee
	if initialAccountPayments-updatedAccountPaymentsPlusTxFee <= maxRoundingDiff {
		t.Fatalf("initial account payment total %v to be equal to updated "+
			"values plus the transaction fee %v", initialAccountPayments,
			updatedAccountPaymentsPlusTxFee)
	}

	// Ensure providing no tx inputs triggers an error.
	_, _, err = mgr.applyTxFees([]chainjson.TransactionInput{},
		out, outV, poolFeeAddrs)
	if !errors.Is(err, ErrTxIn) {
		t.Fatalf("expected a tx input error, got %v", err)
	}

	// Ensure providing no tx outputs triggers an error.
	_, _, err = mgr.applyTxFees([]chainjson.TransactionInput{in},
		make(map[string]dcrutil.Amount), outV, poolFeeAddrs)
	if !errors.Is(err, ErrTxOut) {
		t.Fatalf("expected a tx output error, got %v", err)
	}

	// confirmCoinbases tests.
	txHashes := make(map[string]*chainhash.Hash)
	hashA := chainhash.Hash{'a'}
	txHashes[hashA.String()] = &hashA
	hashB := chainhash.Hash{'b'}
	txHashes[hashB.String()] = &hashB
	hashC := chainhash.Hash{'c'}
	txHashes[hashC.String()] = &hashC
	spendableHeight := uint32(10)

	mgr.cfg.GetTxConfNotifications = func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
		return nil, fmt.Errorf("unable to fetch tx conf notification source")
	}

	// Ensure confirming coinbases returns an error if transaction
	// confirmation notifications cannot be fetched.
	err = mgr.confirmCoinbases(ctx, txHashes, spendableHeight)
	if err == nil {
		cancel()
		t.Fatalf("expected tx conf notification source error")
	}

	mgr.cfg.GetTxConfNotifications = func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
		return func() (*walletrpc.ConfirmationNotificationsResponse, error) {
			return &walletrpc.ConfirmationNotificationsResponse{}, nil
		}, nil
	}

	go func() {
		time.Sleep(time.Microsecond * 200)
		cancel()
	}()

	// Ensure confirming coinbases returns an error if the provided context
	// is cancelled.
	err = mgr.confirmCoinbases(ctx, txHashes, spendableHeight)
	if !errors.Is(err, ErrContextCancelled) {
		t.Fatalf("expected a context cancellation error")
	}

	// The context here needs to be recreated after the previous test.
	ctx, cancel = context.WithCancel(context.Background())
	mgr.cfg.GetTxConfNotifications = func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
		return func() (*walletrpc.ConfirmationNotificationsResponse, error) {
			return nil, fmt.Errorf("unable to confirm transactions")
		}, nil
	}

	// Ensure confirming coinbases returns an error if notification source
	// cannot confirm transactions.
	err = mgr.confirmCoinbases(ctx, txHashes, spendableHeight)
	if !errors.Is(err, ErrTxConf) {
		cancel()
		t.Fatalf("expected tx confirmation error, got %v", err)
	}

	txConfs := make([]*walletrpc.ConfirmationNotificationsResponse_TransactionConfirmations, 0)
	confA := walletrpc.ConfirmationNotificationsResponse_TransactionConfirmations{
		TxHash:        hashA[:],
		Confirmations: 50,
		BlockHash:     []byte(zeroSource.BlockHash),
		BlockHeight:   60,
	}
	txConfs = append(txConfs, &confA)
	confB := walletrpc.ConfirmationNotificationsResponse_TransactionConfirmations{
		TxHash:        hashB[:],
		Confirmations: 50,
		BlockHash:     []byte(zeroSource.BlockHash),
		BlockHeight:   60,
	}
	txConfs = append(txConfs, &confB)
	confC := walletrpc.ConfirmationNotificationsResponse_TransactionConfirmations{
		TxHash:        hashC[:],
		Confirmations: 50,
		BlockHash:     []byte(zeroSource.BlockHash),
		BlockHeight:   60,
	}
	txConfs = append(txConfs, &confC)

	mgr.cfg.GetTxConfNotifications = func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
		return func() (*walletrpc.ConfirmationNotificationsResponse, error) {
			return &walletrpc.ConfirmationNotificationsResponse{
				Confirmations: txConfs,
			}, nil
		}, nil
	}

	// Ensure confirming coinbases returns without error if all expected
	// tx confirmations are returned.
	err = mgr.confirmCoinbases(ctx, txHashes, spendableHeight)
	if err != nil {
		cancel()
		t.Fatalf("expected no tx confirmation errors, got %v", err)
	}

	// generatePayoutTxDetails tests.
	amt, _ = dcrutil.NewAmount(5)
	mPmts = make(map[string][]*Payment)
	pmtA = NewPayment(xID, zeroSource, amt, height, estMaturity)
	mPmts[zeroSource.Coinbase] = []*Payment{pmtA}
	pmtB = NewPayment(yID, randSource, amt, height, estMaturity)
	mPmts[randSource.Coinbase] = []*Payment{pmtB}
	treasuryActive := true

	// Ensure generating payout tx details returns an error if fetching txOut
	// information fails.
	txC := &txCreatorImpl{
		getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
			return nil, fmt.Errorf("unable to fetch txOut")
		},
	}
	_, _, _, _, err = mgr.generatePayoutTxDetails(ctx, txC, poolFeeAddrs,
		mPmts, treasuryActive)
	if !errors.Is(err, ErrTxOut) {
		cancel()
		t.Fatalf("expected a fetch txOut error, got %v", err)
	}

	// Ensure generating payout tx details returns an error if the returned
	// output is not spendable.
	txC = &txCreatorImpl{
		getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
			return &chainjson.GetTxOutResult{
				BestBlock:     chainhash.Hash{0}.String(),
				Confirmations: 0,
				Value:         5,
				Coinbase:      true,
			}, nil
		},
	}

	_, _, _, _, err = mgr.generatePayoutTxDetails(ctx, txC, poolFeeAddrs,
		mPmts, treasuryActive)
	if !errors.Is(err, ErrCoinbase) {
		cancel()
		t.Fatalf("expected a spendable error")
	}

	// Ensure generating payout tx details returns an error if an account
	// referenced by a payment cannot be found.
	unknownID := "abcd"
	unknownIDCoinbase := chainhash.Hash{'u'}
	pmtD := NewPayment(unknownID, randSource, amt, height, estMaturity)
	mPmts[unknownIDCoinbase.String()] = []*Payment{pmtD}
	txC = &txCreatorImpl{
		getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
			return &chainjson.GetTxOutResult{
				BestBlock:     chainhash.Hash{0}.String(),
				Confirmations: 50,
				Value:         5,
				Coinbase:      true,
			}, nil
		},
	}

	_, _, _, _, err = mgr.generatePayoutTxDetails(ctx, txC, poolFeeAddrs,
		mPmts, treasuryActive)
	if !errors.Is(err, ErrValueNotFound) {
		cancel()
		t.Fatalf("expected an account not found error")
	}

	// Ensure generating payout tx details returns an error if the
	// total input value is less than the total output value.
	delete(mPmts, unknownIDCoinbase.String())
	txC = &txCreatorImpl{
		getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
			return &chainjson.GetTxOutResult{
				BestBlock:     chainhash.Hash{0}.String(),
				Confirmations: 50,
				Value:         1,
				Coinbase:      true,
			}, nil
		},
	}

	_, _, _, _, err = mgr.generatePayoutTxDetails(ctx, txC, poolFeeAddrs,
		mPmts, treasuryActive)
	if !errors.Is(err, ErrCreateTx) {
		cancel()
		t.Fatalf("expected an input output mismatch error")
	}

	// Ensure generating payout tx details returns an error if the outputs of
	// the transaction do not exhaust all remaining input value after rounding
	// errors.
	txC = &txCreatorImpl{
		getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
			return &chainjson.GetTxOutResult{
				BestBlock:     chainhash.Hash{0}.String(),
				Confirmations: 50,
				Value:         100,
				Coinbase:      true,
			}, nil
		},
	}

	_, _, _, _, err = mgr.generatePayoutTxDetails(ctx, txC, poolFeeAddrs,
		mPmts, treasuryActive)
	if !errors.Is(err, ErrCreateTx) {
		cancel()
		t.Fatalf("expected an unclaimed input value error, got %v", err)
	}

	// Ensure generating payout tx details does not error with valid parameters.
	txC = &txCreatorImpl{
		getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
			return &chainjson.GetTxOutResult{
				BestBlock:     chainhash.Hash{0}.String(),
				Confirmations: 50,
				Value:         5,
				Coinbase:      true,
			}, nil
		},
	}

	inputs, inputTxHashes, outputs, _, err := mgr.generatePayoutTxDetails(ctx,
		txC, poolFeeAddrs,
		mPmts, treasuryActive)
	if err != nil {
		cancel()
		t.Fatalf("unexpected payout tx details error, got %v", err)
	}

	expectedTxHashes := 2
	if len(inputTxHashes) != expectedTxHashes {
		cancel()
		t.Fatalf("expected %d input tx hashes, got %d",
			expectedTxHashes, len(inputTxHashes))
	}

	expectedInputs := 2
	if len(inputs) != expectedInputs {
		cancel()
		t.Fatalf("expected %d inputs, got %d", expectedInputs, len(inputs))
	}

	for _, hash := range inputTxHashes {
		txHash := hash.String()
		var match bool
		for _, in := range inputs {
			if in.Txid == txHash {
				match = true
			}
		}
		if !match {
			cancel()
			t.Fatalf("no input found for tx hash: %s", txHash)
		}
	}

	expectedOutputs := 2
	if len(outputs) != expectedOutputs {
		cancel()
		t.Fatalf("expected %d inputs, got %d", expectedOutputs, len(outputs))
	}

	for addr := range outputs {
		var match bool
		if addr == feeAddr || addr == xAddr || addr == yAddr {
			match = true
		}
		if !match {
			cancel()
			t.Fatalf("no payment found for output destination: %s", addr)
		}
	}

	// payDividends tests.
	height = uint32(10)
	estMaturity = uint32(26)
	amt, _ = dcrutil.NewAmount(5)
	_, err = persistPayment(db, xID, zeroSource, amt, height, estMaturity)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_, err = persistPayment(db, yID, randSource, amt, height, estMaturity)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	// Ensure dividend payments returns no error if there are no mature
	// payments to work with.
	err = mgr.payDividends(ctx, estMaturity-1, treasuryActive)
	if err != nil {
		cancel()
		t.Fatal("expected no error since there are no mature payments")
	}

	// Ensure dividend payment returns an error if the tx creator cannot be
	// fetched.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return nil
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if !errors.Is(err, ErrDisconnected) {
		cancel()
		t.Fatalf("expected a nil tx creator error, got %v", err)
	}

	// Ensure dividend payment returns an error if pruning orphaned payments
	// fails.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return &txCreatorImpl{}
	}
	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		return -1, fmt.Errorf("unable to confirm blocks")
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if err == nil {
		cancel()
		t.Fatalf("expected a prune orphan payments error")
	}

	// Ensure dividend payment returns an error if generating payout tx details
	// fails.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return &txCreatorImpl{
			getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
				return nil, fmt.Errorf("unable to fetch txOut")
			},
		}
	}
	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		return 16, nil
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if !errors.Is(err, ErrTxOut) {
		cancel()
		t.Fatalf("expected a generate payout tx details error, got %v", err)
	}

	// Ensure dividend payment returns an error if applying tx fees fails.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return &txCreatorImpl{}
	}
	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		return -1, nil
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if !errors.Is(err, ErrTxIn) {
		cancel()
		t.Fatalf("expected an apply tx fee error, got %v", err)
	}

	// Ensure dividend payment returns an error if confirming a coinbase fails.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return &txCreatorImpl{
			getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
				return &chainjson.GetTxOutResult{
					BestBlock:     chainhash.Hash{0}.String(),
					Confirmations: int64(estMaturity) + 1,
					Value:         5,
					Coinbase:      true,
				}, nil
			},
		}
	}
	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		return int64(estMaturity) + 1, nil
	}
	mgr.cfg.GetTxConfNotifications = func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
		return nil, fmt.Errorf("unable to fetch tx conf notification source")
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if err == nil {
		cancel()
		t.Fatalf("expected a coinbase confirmation error, got %v", err)
	}

	// Ensure dividend payment returns an error if the payout transaction cannot
	// be created.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return &txCreatorImpl{
			getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
				return &chainjson.GetTxOutResult{
					BestBlock:     chainhash.Hash{0}.String(),
					Confirmations: int64(estMaturity) + 1,
					Value:         5,
					Coinbase:      true,
				}, nil
			},
			createRawTransaction: func(ctx context.Context, inputs []chainjson.TransactionInput, amounts map[dcrutil.Address]dcrutil.Amount, lockTime *int64, expiry *int64) (*wire.MsgTx, error) {
				return nil, fmt.Errorf("unable to create raw transactions")
			},
		}
	}
	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		return int64(estMaturity) + 1, nil
	}

	txConfs = make([]*walletrpc.ConfirmationNotificationsResponse_TransactionConfirmations, 0)
	confA = walletrpc.ConfirmationNotificationsResponse_TransactionConfirmations{
		TxHash:        zeroHash[:],
		Confirmations: 50,
		BlockHash:     []byte(zeroSource.BlockHash),
		BlockHeight:   60,
	}
	txConfs = append(txConfs, &confA)
	confB = walletrpc.ConfirmationNotificationsResponse_TransactionConfirmations{
		TxHash:        randHash[:],
		Confirmations: 50,
		BlockHash:     []byte(zeroSource.BlockHash),
		BlockHeight:   60,
	}
	txConfs = append(txConfs, &confB)

	mgr.cfg.CoinbaseConfTimeout = time.Millisecond * 500
	mgr.cfg.GetTxConfNotifications = func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
		return func() (*walletrpc.ConfirmationNotificationsResponse, error) {
			return &walletrpc.ConfirmationNotificationsResponse{
				Confirmations: txConfs,
			}, nil
		}, nil
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if err == nil {
		cancel()
		t.Fatal("expected a create transaction error")
	}

	// Ensure dividend payment returns an error if the tx broadcaster cannot be
	// fetched.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return &txCreatorImpl{
			getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
				return &chainjson.GetTxOutResult{
					BestBlock:     chainhash.Hash{0}.String(),
					Confirmations: int64(estMaturity) + 1,
					Value:         5,
					Coinbase:      true,
				}, nil
			},
			createRawTransaction: func(ctx context.Context, inputs []chainjson.TransactionInput, amounts map[dcrutil.Address]dcrutil.Amount, lockTime *int64, expiry *int64) (*wire.MsgTx, error) {
				return &wire.MsgTx{}, nil
			},
		}
	}
	mgr.cfg.GetBlockConfirmations = func(ctx context.Context, bh *chainhash.Hash) (int64, error) {
		return int64(estMaturity) + 1, nil
	}
	mgr.cfg.FetchTxBroadcaster = func() TxBroadcaster {
		return nil
	}
	mgr.cfg.WalletPass = "123"
	mgr.cfg.GetTxConfNotifications = func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
		return func() (*walletrpc.ConfirmationNotificationsResponse, error) {
			return &walletrpc.ConfirmationNotificationsResponse{
				Confirmations: txConfs,
			}, nil
		}, nil
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if !errors.Is(err, ErrDisconnected) {
		cancel()
		t.Fatalf("expected a fetch tx broadcaster error, got %v", err)
	}

	// Ensure dividend payment returns an error if the payout transaction cannot
	// be signed.
	mgr.cfg.FetchTxCreator = func() TxCreator {
		return &txCreatorImpl{
			getTxOut: func(ctx context.Context, txHash *chainhash.Hash, index uint32, mempool bool) (*chainjson.GetTxOutResult, error) {
				return &chainjson.GetTxOutResult{
					BestBlock:     chainhash.Hash{0}.String(),
					Confirmations: int64(estMaturity) + 1,
					Value:         5,
					Coinbase:      true,
				}, nil
			},
			createRawTransaction: func(ctx context.Context, inputs []chainjson.TransactionInput, amounts map[dcrutil.Address]dcrutil.Amount, lockTime *int64, expiry *int64) (*wire.MsgTx, error) {
				return &wire.MsgTx{}, nil
			},
		}
	}
	mgr.cfg.FetchTxBroadcaster = func() TxBroadcaster {
		return &txBroadcasterImpl{
			signTransaction: func(ctx context.Context, req *walletrpc.SignTransactionRequest, options ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error) {
				return nil, fmt.Errorf("unable to sign transaction")
			},
		}
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if !errors.Is(err, ErrSignTx) {
		cancel()
		t.Fatalf("expected a signing error, got %v", err)
	}

	// Ensure dividend payment returns an error if the payout transaction
	// cannot be published.
	txBytes := []byte("01000000018e17619f0d627c2769ee3f957582691aea59c2" +
		"e79cc45b8ba1f08485dd88d75c0300000001ffffffff017a64e43703000000" +
		"00001976a914978fa305bd66f63f0de847338bb56ff65fa8e27288ac000000" +
		"000000000001f46ce43703000000846c0700030000006b483045022100d668" +
		"5812801db991b72e80863eba7058466dfebb4aba0af75ab47bade177325102" +
		"205f466fc47435c1a177482e527ff0e76f3c2c613940b358e57f0f0d78d5f2" +
		"ffcb012102d040a4c34ae65a2b87ea8e9df7413e6504e5f27c6bde019a78ee" +
		"96145b27c517")
	mgr.cfg.FetchTxBroadcaster = func() TxBroadcaster {
		return &txBroadcasterImpl{
			signTransaction: func(ctx context.Context, req *walletrpc.SignTransactionRequest, options ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error) {
				return &walletrpc.SignTransactionResponse{
					Transaction: txBytes,
				}, nil
			},
			publishTransaction: func(ctx context.Context, req *walletrpc.PublishTransactionRequest, options ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error) {
				return nil, fmt.Errorf("unable to publish transaction")
			},
		}
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if !errors.Is(err, ErrPublishTx) {
		cancel()
		t.Fatalf("expected a publish error, got %v", err)
	}

	// Ensure paying dividend payment succeeds with valid inputs.
	txHash, _ := hex.DecodeString("013264da8cc53f70022dc2b5654ebefc9ecfed24ea18dfcfc9adca5642d4fe66")
	mgr.cfg.FetchTxBroadcaster = func() TxBroadcaster {
		return &txBroadcasterImpl{
			signTransaction: func(ctx context.Context, req *walletrpc.SignTransactionRequest, options ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error) {
				return &walletrpc.SignTransactionResponse{
					Transaction: txBytes,
				}, nil
			},
			publishTransaction: func(ctx context.Context, req *walletrpc.PublishTransactionRequest, options ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error) {
				return &walletrpc.PublishTransactionResponse{
					TransactionHash: txHash,
				}, nil
			},
		}
	}

	err = mgr.payDividends(ctx, estMaturity+1, treasuryActive)
	if err != nil {
		cancel()
		t.Fatalf("unexpected dividend payment error, got %v", err)
	}

	cancel()

	// Reset backed up values to their defaults.
	err = persistLastPaymentInfo(db, 0, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment info: %v", err)
	}
	err = persistLastPaymentCreatedOn(db, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment created on: %v", err)
	}

	// Ensure dust payments are forfeited by their originating accounts and
	// added to the pool fee payout.
	now = time.Now()
	pCfg.PaymentMethod = PPLNS
	coinbaseValue = 1
	mul := 100000
	yWeight := new(big.Rat).Mul(weight, new(big.Rat).SetInt64(int64(mul)))

	// Create shares for account x and y.
	err = persistShare(db, xID, weight, now.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	err = persistShare(db, yID, yWeight, now.UnixNano())
	if err != nil {
		t.Fatal(err)
	}

	coinbase, err = dcrutil.NewAmount(float64(coinbaseValue))
	if err != nil {
		t.Fatalf("[NewAmount] unexpected error: %v", err)
	}

	// Ensure the expected payout amount for account x is dust.
	expectedDustAmt := coinbase.MulF64(1 / float64(mul))
	if !txrules.IsDustAmount(expectedDustAmt, txsizes.P2PKHOutputSize,
		txrules.DefaultRelayFeePerKb) {
		t.Fatal("expected dust amount for account x")
	}

	err = mgr.generatePayments(height, zeroSource, coinbase, now.UnixNano())
	if err != nil {
		t.Fatalf("unable to generate payments: %v", err)
	}

	// Ensure the payments created are for account y and a fee
	// payment entry.
	pmts, err = fetchPendingPayments(db)
	if err != nil {
		t.Fatalf("pendingPayments error: %v", err)
	}

	// Ensure only two pending payments were generated.
	if len(pmts) != 2 {
		t.Fatalf("expected 2 pending payments, got %d", len(pmts))
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
		if pmt.Account == PoolFeesK {
			ft += pmt.Amount
		}
	}

	// Ensure account x has no payments for it since it generated dust.
	if xt != dcrutil.Amount(0) {
		t.Fatalf("expected no payment amounts for account x, got %v", xt)
	}

	// Ensure the updated pool fee includes the dust amount from account x.
	expectedFeeAmt = coinbase.MulF64(mgr.cfg.PoolFee)
	if ft-maxRoundingDiff < expectedFeeAmt {
		t.Fatalf("expected the updated pool fee (%v) to be greater "+
			"than the initial (%v)", ft, expectedFeeAmt)
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
	err = persistLastPaymentInfo(db, 0, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment info: %v", err)
	}
	err = persistLastPaymentCreatedOn(db, 0)
	if err != nil {
		t.Fatalf("unable to persist default last payment created on: %v", err)
	}
}
