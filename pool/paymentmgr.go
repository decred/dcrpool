package pool

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"sync/atomic"
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

const (
	// PPS represents the pay per share payment method.
	PPS = "pps"

	// PPLNS represents the pay per last n shares payment method.
	PPLNS = "pplns"

	// maxRoundingDiff is the maximum amount of atoms the total
	// output value of a transaction is allowed to be short of the
	// provided input due to rounding errors.
	maxRoundingDiff = dcrutil.Amount(500)
)

// TxCreator defines the functionality needed by a transaction creator for the
// pool.
type TxCreator interface {
	// GetTxOut fetches the output referenced by the provided txHash and index.
	GetTxOut(context.Context, *chainhash.Hash, uint32, bool) (*chainjson.GetTxOutResult, error)
	// CreateRawTransaction generates a transaction from the provided
	// inputs and payouts.
	CreateRawTransaction(context.Context, []chainjson.TransactionInput, map[dcrutil.Address]dcrutil.Amount, *int64, *int64) (*wire.MsgTx, error)
	// GetBlock fetches the block associated with the provided block hash.
	GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
}

// TxBroadcaster defines the functionality needed by a transaction broadcaster
// for the pool.
type TxBroadcaster interface {
	// SignTransaction signs transaction inputs, unlocking them for use.
	SignTransaction(context.Context, *walletrpc.SignTransactionRequest, ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error)
	// PublishTransaction broadcasts the transaction unto the network.
	PublishTransaction(context.Context, *walletrpc.PublishTransactionRequest, ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error)
}

// PaymentMgrConfig contains all of the configuration values which should be
// provided when creating a new instance of PaymentMgr.
type PaymentMgrConfig struct {
	// DB represents the pool database.
	DB *bolt.DB
	// ActiveNet represents the network being mined on.
	ActiveNet *chaincfg.Params
	// PoolFee represents the fee charged to participating accounts of the pool.
	PoolFee float64
	// LastNPeriod represents the period to source shares from when using the
	// PPLNS payment scheme.
	LastNPeriod time.Duration
	// SoloPool represents the solo pool mining mode.
	SoloPool bool
	// PaymentMethod represents the payment scheme of the pool.
	PaymentMethod string
	// PoolFeeAddrs represents the pool fee addresses of the pool.
	PoolFeeAddrs []dcrutil.Address
	// WalletAccount represents the wallet account to process payments from.
	WalletAccount uint32
	// WalletPass represents the passphrase to unlock the wallet with.
	WalletPass string
	// GetBlockConfirmations returns the number of block confirmations for the
	// provided block hash.
	GetBlockConfirmations func(context.Context, *chainhash.Hash) (int64, error)
	// GetTxConfNotifications streams transaction confirmation notifications on
	// the provided hashes.
	GetTxConfNotifications func([]*chainhash.Hash, int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error)
	// FetchTxCreator returns a transaction creator that allows coinbase lookups
	// and payment transaction creation.
	FetchTxCreator func() TxCreator
	// FetchTxBroadcaster returns a transaction broadcaster that allows signing
	// and publishing of transactions.
	FetchTxBroadcaster func() TxBroadcaster
}

// PaymentMgr handles generating shares and paying out dividends to
// participating accounts.
type PaymentMgr struct {
	lastPaymentHeight    uint32 // update atomically.
	lastPaymentPaidOn    uint64 // update atomically.
	lastPaymentCreatedOn uint64 // update atomically.

	cfg *PaymentMgrConfig
}

// NewPaymentMgr creates a new payment manager.
func NewPaymentMgr(pCfg *PaymentMgrConfig) (*PaymentMgr, error) {
	pm := &PaymentMgr{
		cfg: pCfg,
	}
	rand.Seed(time.Now().UnixNano())
	err := pm.cfg.DB.Update(func(tx *bolt.Tx) error {
		err := pm.loadLastPaymentHeight(tx)
		if err != nil {
			return err
		}
		err = pm.loadLastPaymentPaidOn(tx)
		if err != nil {
			return err
		}
		return pm.loadLastPaymentCreatedOn(tx)
	})
	if err != nil {
		return nil, err
	}
	return pm, nil
}

// setLastPaymentHeight updates the last payment height.
func (pm *PaymentMgr) setLastPaymentHeight(height uint32) {
	atomic.StoreUint32(&pm.lastPaymentHeight, height)
}

// fetchLastPaymentHeight fetches the last payment height.
func (pm *PaymentMgr) fetchLastPaymentHeight() uint32 {
	return atomic.LoadUint32(&pm.lastPaymentHeight)
}

// persistLastPaymentHeight saves the last payment height to the db.
func (pm *PaymentMgr) persistLastPaymentHeight(tx *bolt.Tx) error {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}
	height := atomic.LoadUint32(&pm.lastPaymentHeight)
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, height)
	err := pbkt.Put(lastPaymentHeight, b)
	return err
}

// loadLastPaymentHeight fetches the last payment height from the db.
func (pm *PaymentMgr) loadLastPaymentHeight(tx *bolt.Tx) error {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}
	lastPaymentHeightB := pbkt.Get(lastPaymentHeight)
	if lastPaymentHeightB == nil {
		pm.setLastPaymentHeight(0)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, 0)
		return pbkt.Put(lastPaymentHeight, b)
	}
	pm.setLastPaymentHeight(binary.LittleEndian.Uint32(lastPaymentHeightB))
	return nil
}

// setLastPaymentPaidOn updates the last payment paid on time.
func (pm *PaymentMgr) setLastPaymentPaidOn(time uint64) {
	atomic.StoreUint64(&pm.lastPaymentPaidOn, time)
}

// fetchLastPaymentPaidOn fetches the last payment paid on time.
func (pm *PaymentMgr) fetchLastPaymentPaidOn() uint64 {
	return atomic.LoadUint64(&pm.lastPaymentPaidOn)
}

// persistLastPaymentPaidOn saves the last payment paid on time to the db.
func (pm *PaymentMgr) persistLastPaymentPaidOn(tx *bolt.Tx) error {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}
	return pbkt.Put(lastPaymentPaidOn,
		nanoToBigEndianBytes(int64(pm.lastPaymentPaidOn)))
}

// pruneShares removes invalidated shares from the db.
func (pm *PaymentMgr) pruneShares(tx *bolt.Tx, minNano int64) error {
	minB := nanoToBigEndianBytes(minNano)
	bkt, err := fetchShareBucket(tx)
	if err != nil {
		return err
	}
	toDelete := [][]byte{}
	cursor := bkt.Cursor()
	createdOnB := make([]byte, 8)
	for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
		_, err := hex.Decode(createdOnB, k[:16])
		if err != nil {
			return err
		}

		if bytes.Compare(minB, createdOnB) > 0 {
			toDelete = append(toDelete, k)
		}
	}
	for _, entry := range toDelete {
		err := bkt.Delete(entry)
		if err != nil {
			return err
		}
	}
	return nil
}

// fetchPoolBucket is a helper function for getting the pool bucket.
func fetchPoolBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	return pbkt, nil
}

// bigEndianBytesToNano returns nanosecond time from the provided
// big endian bytes.
func bigEndianBytesToNano(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

// loadLastPaymentPaidOn fetches the last payment paid on time from the db.
func (pm *PaymentMgr) loadLastPaymentPaidOn(tx *bolt.Tx) error {
	pbkt, err := fetchPoolBucket(tx)
	if err != nil {
		return err
	}
	lastPaymentPaidOnB := pbkt.Get(lastPaymentPaidOn)
	if lastPaymentPaidOnB == nil {
		pm.setLastPaymentPaidOn(0)
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, 0)
		return pbkt.Put(lastPaymentPaidOn, b)
	}
	pm.setLastPaymentPaidOn(bigEndianBytesToNano(lastPaymentPaidOnB))
	return nil
}

// setLastPaymentCreatedOn updates the last payment created on time.
func (pm *PaymentMgr) setLastPaymentCreatedOn(time uint64) {
	atomic.StoreUint64(&pm.lastPaymentCreatedOn, time)
}

// fetchLastPaymentCreatedOn fetches the last payment created on time.
func (pm *PaymentMgr) fetchLastPaymentCreatedOn() uint64 {
	return atomic.LoadUint64(&pm.lastPaymentCreatedOn)
}

// persistLastPaymentCreatedOn saves the last payment created on time to the db.
func (pm *PaymentMgr) persistLastPaymentCreatedOn(tx *bolt.Tx) error {
	pbkt, err := fetchPoolBucket(tx)
	if err != nil {
		return err
	}
	return pbkt.Put(lastPaymentCreatedOn,
		nanoToBigEndianBytes(int64(pm.lastPaymentCreatedOn)))
}

// loadLastPaymentCreaedOn fetches the last payment created on time from the db.
func (pm *PaymentMgr) loadLastPaymentCreatedOn(tx *bolt.Tx) error {
	pbkt, err := fetchPoolBucket(tx)
	if err != nil {
		return err
	}
	lastPaymentCreatedOnB := pbkt.Get(lastPaymentCreatedOn)
	if lastPaymentCreatedOnB == nil {
		pm.setLastPaymentCreatedOn(0)
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, 0)
		return pbkt.Put(lastPaymentCreatedOn, b)
	}
	pm.setLastPaymentCreatedOn(bigEndianBytesToNano(lastPaymentCreatedOnB))
	return nil
}

// sharePercentages calculates the percentages due each participating account
// according to their weighted shares.
func (pm *PaymentMgr) sharePercentages(shares []*Share) (map[string]*big.Rat, error) {
	totalShares := new(big.Rat)
	tally := make(map[string]*big.Rat)
	percentages := make(map[string]*big.Rat)

	// Tally all share weights for each participating account.
	for _, share := range shares {
		totalShares = totalShares.Add(totalShares, share.Weight)
		if _, ok := tally[share.Account]; ok {
			tally[share.Account] = tally[share.Account].
				Add(tally[share.Account], share.Weight)
			continue
		}
		tally[share.Account] = share.Weight
	}

	// Calculate each participating account percentage to be claimed.
	for account, shareCount := range tally {
		if tally[account].Cmp(ZeroRat) == 0 {
			return nil, MakeError(ErrDivideByZero, "division by zero", nil)
		}
		accPercent := new(big.Rat).Quo(shareCount, totalShares)
		percentages[account] = accPercent
	}
	return percentages, nil
}

// PPSEligibleShares fetches all shares created before or at the provided
// time.
func (pm *PaymentMgr) PPSEligibleShares(max []byte) ([]*Share, error) {
	eligibleShares := make([]*Share, 0)
	err := pm.cfg.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}
		c := bkt.Cursor()
		createdOnB := make([]byte, 8)
		for k, v := c.First(); k != nil; k, v = c.Next() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				return err
			}

			if bytes.Compare(createdOnB, max) <= 0 {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					return err
				}
				eligibleShares = append(eligibleShares, &share)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return eligibleShares, err
}

// PPSSharePercentages calculates the current mining reward percentages
// due participating pool accounts based on work performed measured by
// the PPS payment scheme.
func (pm *PaymentMgr) PPSSharePercentages(workCreatedOn int64) (map[string]*big.Rat, error) {
	max := nanoToBigEndianBytes(workCreatedOn)
	shares, err := pm.PPSEligibleShares(max)
	if err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}
	percentages, err := pm.sharePercentages(shares)
	if err != nil {
		return nil, err
	}
	return percentages, nil
}

// PPLNSEligibleShares fetches all shares keyed greater than the provided
// minimum.
func (pm *PaymentMgr) PPLNSEligibleShares(min []byte) ([]*Share, error) {
	eligibleShares := make([]*Share, 0)
	err := pm.cfg.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchShareBucket(tx)
		if err != nil {
			return err
		}
		c := bkt.Cursor()
		createdOnB := make([]byte, 8)
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				return err
			}

			if bytes.Compare(createdOnB, min) > 0 {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					return err
				}
				eligibleShares = append(eligibleShares, &share)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return eligibleShares, err
}

// PPLNSSharePercentages calculates the current mining reward percentages due pool
// accounts based on work performed measured by the PPLNS payment scheme.
func (pm *PaymentMgr) PPLNSSharePercentages() (map[string]*big.Rat, error) {
	now := time.Now()
	min := now.Add(-pm.cfg.LastNPeriod)
	shares, err := pm.PPLNSEligibleShares(nanoToBigEndianBytes(min.UnixNano()))
	if err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := pm.sharePercentages(shares)
	if err != nil {
		return nil, err
	}
	return percentages, nil
}

// calculatePayments creates the payments due participating accounts.
func (pm *PaymentMgr) calculatePayments(ratios map[string]*big.Rat, source *PaymentSource,
	total dcrutil.Amount, poolFee float64, height uint32, estMaturity uint32) ([]*Payment, int64, error) {
	if len(ratios) == 0 {
		return nil, 0, fmt.Errorf("valid share ratios required to" +
			" generate payments")
	}

	// Deduct pool fee from the amount to be shared.
	fee := total.MulF64(poolFee)
	amtSansFees := total - fee
	sansFees := new(big.Rat).SetInt64(int64(amtSansFees))
	paymentTotal := dcrutil.Amount(0)

	// Calculate each participating account's portion of the amount after fees.
	payments := make([]*Payment, 0)
	for account, ratio := range ratios {
		amtRat := new(big.Rat).Mul(sansFees, ratio)
		amtI, accuracy := new(big.Float).SetRat(amtRat).Int64()
		amt := dcrutil.Amount(amtI)

		// Reduce the amount by an atom if float conversion accuracy was
		// above the actual value.
		if accuracy > 0 {
			amt -= dcrutil.Amount(1)
		}

		paymentTotal += amt
		payments = append(payments, NewPayment(account, source, amt, height,
			estMaturity))
	}

	if amtSansFees < paymentTotal {
		diff := paymentTotal - amtSansFees
		return nil, 0, fmt.Errorf("total payments (%s) is greater than "+
			"the remaining coinbase amount after fees (%s). Difference is %s",
			paymentTotal, amtSansFees, diff)
	}

	// Add a payout entry for pool fees.
	feePayment := NewPayment(poolFeesK, source, fee, height, estMaturity)
	payments = append(payments, feePayment)

	return payments, feePayment.CreatedOn, nil
}

// PayPerShare generates a payment bundle comprised of payments to all
// participating accounts. Payments are calculated based on work contributed
// to the pool since the last payment batch.
func (pm *PaymentMgr) payPerShare(source *PaymentSource, amt dcrutil.Amount, height uint32, workCreatedOn int64) error {
	percentages, err := pm.PPSSharePercentages(workCreatedOn)
	if err != nil {
		return err
	}
	estMaturity := height + uint32(pm.cfg.ActiveNet.CoinbaseMaturity)
	payments, lastPmtCreatedOn, err := pm.calculatePayments(percentages,
		source, amt, pm.cfg.PoolFee, height, estMaturity)
	if err != nil {
		return err
	}
	for _, payment := range payments {
		err := payment.Create(pm.cfg.DB)
		if err != nil {
			return err
		}
	}
	pm.setLastPaymentCreatedOn(uint64(lastPmtCreatedOn))
	err = pm.cfg.DB.Update(func(tx *bolt.Tx) error {
		// Update the last payment created on time and prune invalidated shares.
		err := pm.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return err
		}
		return pm.pruneShares(tx, workCreatedOn)
	})
	return err
}

// payPerLastNShares generates a payment bundle comprised of payments to all
// participating accounts within the lastNPeriod of the pool.
func (pm *PaymentMgr) payPerLastNShares(source *PaymentSource, amt dcrutil.Amount, height uint32) error {
	percentages, err := pm.PPLNSSharePercentages()
	if err != nil {
		return err
	}
	estMaturity := height + uint32(pm.cfg.ActiveNet.CoinbaseMaturity)
	payments, lastPmtCreatedOn, err := pm.calculatePayments(percentages,
		source, amt, pm.cfg.PoolFee, height, estMaturity)
	if err != nil {
		return err
	}
	for _, payment := range payments {
		err := payment.Create(pm.cfg.DB)
		if err != nil {
			return err
		}
	}
	pm.setLastPaymentCreatedOn(uint64(lastPmtCreatedOn))
	err = pm.cfg.DB.Update(func(tx *bolt.Tx) error {
		// Update the last payment created on time and prune invalidated shares.
		err := pm.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return err
		}
		minNano := time.Now().Add(-pm.cfg.LastNPeriod).UnixNano()
		return pm.pruneShares(tx, minNano)
	})
	return err
}

// generatePayments creates payments for participating accounts. This should
// only be called when a block is confirmed mined, in pool mining mode.
func (pm *PaymentMgr) generatePayments(height uint32, source *PaymentSource, amt dcrutil.Amount, workCreatedOn int64) error {
	switch pm.cfg.PaymentMethod {
	case PPS:
		return pm.payPerShare(source, amt, height, workCreatedOn)

	case PPLNS:
		return pm.payPerLastNShares(source, amt, height)

	default:
		return fmt.Errorf("unknown payment method provided %v", pm.cfg.PaymentMethod)
	}
}

// pendingPayments fetches all unpaid payments.
func (pm *PaymentMgr) pendingPayments() ([]*Payment, error) {
	payments := make([]*Payment, 0)
	err := pm.cfg.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			if payment.PaidOnHeight == 0 {
				payments = append(payments, &payment)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return payments, nil
}

// pendingPaymentsAtHeight fetches all pending payments at the provided height.
func (pm *PaymentMgr) pendingPaymentsAtHeight(height uint32) ([]*Payment, error) {
	payments := make([]*Payment, 0)
	err := pm.cfg.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}

		heightBE := heightToBigEndianBytes(height)
		paymentHeightB := make([]byte, 8)
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			_, err := hex.Decode(paymentHeightB, k[:8])
			if err != nil {
				return err
			}

			if bytes.Compare(heightBE, paymentHeightB) > 0 {
				var payment Payment
				err := json.Unmarshal(v, &payment)
				if err != nil {
					return err
				}

				if payment.PaidOnHeight == 0 {
					payments = append(payments, &payment)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return payments, nil
}

// pendingPaymentsForBlockHash returns the number of  pending payments with
// the provided block hash as their source.
func (pm *PaymentMgr) pendingPaymentsForBlockHash(blockHash string) (uint32, error) {
	var count uint32
	err := pm.cfg.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			if payment.PaidOnHeight == 0 {
				if payment.Source.BlockHash == blockHash {
					count++
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// archivedPayments fetches all archived payments. List is ordered, most
// recent comes first.
func (pm *PaymentMgr) archivedPayments() ([]*Payment, error) {
	pmts := make([]*Payment, 0)
	err := pm.cfg.DB.View(func(tx *bolt.Tx) error {
		abkt, err := fetchPaymentArchiveBucket(tx)
		if err != nil {
			return err
		}

		c := abkt.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}
			pmts = append(pmts, &payment)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pmts, nil
}

// maturePendingPayments fetches all mature pending payments at the
// provided height.
func (pm *PaymentMgr) maturePendingPayments(height uint32) (map[string][]*Payment, error) {
	payments := make([]*Payment, 0)
	err := pm.cfg.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			spendableHeight := payment.EstimatedMaturity + 1
			if payment.PaidOnHeight == 0 && spendableHeight <= height {
				payments = append(payments, &payment)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	pmts := make(map[string][]*Payment)
	for _, pmt := range payments {
		set, ok := pmts[pmt.Source.BlockHash]
		if !ok {
			set = make([]*Payment, 0)
		}

		set = append(set, pmt)
		pmts[pmt.Source.BlockHash] = set
	}

	return pmts, nil
}

// PayDividends pays mature mining rewards to participating accounts.
func (pm *PaymentMgr) payDividends(ctx context.Context, height uint32) error {
	pmts, err := pm.maturePendingPayments(height)
	if err != nil {
		return err
	}

	if len(pmts) == 0 {
		return nil
	}

	// The fee address is being picked at random from the set of pool fee
	// addresses to make it difficult for third-parties wanting to track
	// pool fees collected by the pool and ultimately determine the
	// cumulative value accrued by pool operators.
	feeAddr := pm.cfg.PoolFeeAddrs[rand.Intn(len(pm.cfg.PoolFeeAddrs))]

	// Create the payout transaction.
	inputs := make([]chainjson.TransactionInput, 0)
	txHashes := make(map[string]*chainhash.Hash)
	outputs := make(map[string]dcrutil.Amount)
	var tIn dcrutil.Amount
	var tOut dcrutil.Amount
	var maxSpendableHeight uint32

	txCreator := pm.cfg.FetchTxCreator()
	if txCreator == nil {
		return fmt.Errorf("tx creator unset")
	}

	toDelete := make([]string, 0)
	for key := range pmts {
		blockHash, err := chainhash.NewHashFromStr(key)
		if err != nil {
			return err
		}

		confs, err := pm.cfg.GetBlockConfirmations(ctx, blockHash)
		if err != nil {
			return err
		}

		// If the block has no confirmations at the current height,
		// it is an orphan. Remove payments associated with it.
		if confs <= 0 {
			toDelete = append(toDelete, key)
		}
	}

	// Delete payments sourced from orphaned blocks.
	for _, k := range toDelete {
		delete(pmts, k)
	}

	for _, set := range pmts {
		index := uint32(2)
		txHash, err := chainhash.NewHashFromStr(set[0].Source.Coinbase)
		if err != nil {
			return err
		}

		txOutResult, err := txCreator.GetTxOut(ctx, txHash, index, false)
		if err != nil {
			return fmt.Errorf("unable to find tx output: %v", err)
		}

		// Ensure the referenced prevout to be spent is a coinbase and
		// spendable at the current height.
		if !txOutResult.Coinbase {
			return fmt.Errorf("expected the referenced output at index %d "+
				"for tx %v to be a coinbase", index, txHash.String())
		}

		if txOutResult.Confirmations < int64(pm.cfg.ActiveNet.CoinbaseMaturity+1) {
			return fmt.Errorf("expected the referenced coinbase at index %d "+
				"for tx %v to be spendable", index, txHash.String())
		}

		in := chainjson.TransactionInput{
			Amount: txOutResult.Value,
			Txid:   txHash.String(),
			Vout:   index,
			Tree:   wire.TxTreeRegular,
		}
		inputs = append(inputs, in)

		spendableHeight := set[0].EstimatedMaturity + 1
		if maxSpendableHeight < spendableHeight {
			maxSpendableHeight = spendableHeight
		}

		txHashes[txHash.String()] = txHash

		outV, err := dcrutil.NewAmount(txOutResult.Value)
		if err != nil {
			return err
		}
		tIn += outV

		// Generate the outputs paying dividends and fees.
		for _, pmt := range set {
			if pmt.Account == poolFeesK {
				_, ok := outputs[feeAddr.String()]
				if !ok {
					outputs[feeAddr.String()] = pmt.Amount
					tOut += pmt.Amount
					continue
				}
				outputs[feeAddr.String()] += pmt.Amount
				tOut += pmt.Amount
				continue
			}

			acc, err := FetchAccount(pm.cfg.DB, []byte(pmt.Account))
			if err != nil {
				return err
			}

			_, ok := outputs[acc.Address]
			if !ok {
				outputs[acc.Address] = pmt.Amount
				tOut += pmt.Amount
				continue
			}
			outputs[acc.Address] += pmt.Amount
			tOut += pmt.Amount
		}
	}

	if tOut > tIn {
		return fmt.Errorf("total output values for the transaction (%s) "+
			"is greater than the provided inputs %s", tIn, tOut)
	}

	diff := tIn - tOut
	if diff > maxRoundingDiff {
		return fmt.Errorf("difference between total output values and "+
			"the provided inputs (%s) exceeds the maximum allowed "+
			"for rounding errors (%s)", diff, maxRoundingDiff)
	}

	inSizes := make([]int, len(inputs))
	for range inputs {
		inSizes = append(inSizes, txsizes.RedeemP2PKHSigScriptSize)
	}

	outSizes := make([]int, len(outputs))
	for range outputs {
		outSizes = append(outSizes, txsizes.P2PKHOutputSize)
	}

	estSize := txsizes.EstimateSerializeSizeFromScriptSizes(inSizes, outSizes, 0)
	estFee := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, estSize)
	sansFees := tOut - estFee

	// Deduct the portion of transaction fees being paid for by
	// participating accounts from outputs being paid to them.
	//
	// It is  calculated as the percentage of fees based on the
	// ratio of the amount being paid to the total transaction
	// output minus pool fees.
	for addr, v := range outputs {
		if addr == feeAddr.String() {
			continue
		}

		ratio := float64(int64(sansFees)) / float64(int64(v))
		outFee := estFee.MulF64(ratio)
		outputs[addr] -= outFee
	}

	// Generate the output set with decoded addresses.
	outs := make(map[dcrutil.Address]dcrutil.Amount, len(outputs))
	for sAddr, amt := range outputs {
		addr, err := dcrutil.DecodeAddress(sAddr, pm.cfg.ActiveNet)
		if err != nil {
			return fmt.Errorf("unable to decode address: %v", err)
		}

		outs[addr] = amt
	}

	// Ensure the wallet is aware of all the outputs to be spent by the payout
	// transaction.
	if maxSpendableHeight < height {
		maxSpendableHeight = height
	}

	hashes := make([]*chainhash.Hash, 0, len(txHashes))
	for _, hash := range txHashes {
		hashes = append(hashes, hash)
	}

	notifSource, err := pm.cfg.GetTxConfNotifications(hashes,
		int32(maxSpendableHeight))
	if err != nil {
		return fmt.Errorf("unable to stream tx confirmations: %v", err)
	}

	// Wait for coinbase tx confirmations from the wallet.
	maxSpendableConfs := int32(pm.cfg.ActiveNet.CoinbaseMaturity) + 1
	for {
		resp, err := notifSource()
		if err != nil {
			return fmt.Errorf("tx confirmations notification error: %v", err)
		}

		// Ensure all coinbases being spent are spendable before proceeding
		// with creating and publishing the transaction.
		for _, coinbase := range resp.Confirmations {
			if coinbase.Confirmations >= maxSpendableConfs {
				cbHash, err := chainhash.NewHash(coinbase.TxHash)
				if err != nil {
					return fmt.Errorf("unable to create block hash: %v", err)
				}

				// Remove spendable coinbases from the tx hash set. All
				// coinbases are spendable when the tx hash set is empty.
				delete(txHashes, cbHash.String())
			}
		}

		if len(txHashes) == 0 {
			break
		}
	}

	tx, err := txCreator.CreateRawTransaction(ctx, inputs, outs, nil, nil)
	if err != nil {
		return fmt.Errorf("unable to create raw transaction: %v", err)
	}

	txB, err := tx.Bytes()
	if err != nil {
		return err
	}

	txBroadcaster := pm.cfg.FetchTxBroadcaster()
	if txBroadcaster == nil {
		return fmt.Errorf("tx broadcaster unset")
	}

	// Sign the transaction.
	signTxReq := &walletrpc.SignTransactionRequest{
		SerializedTransaction: txB,
		Passphrase:            []byte(pm.cfg.WalletPass),
	}
	signedTxResp, err := txBroadcaster.SignTransaction(context.TODO(),
		signTxReq)
	if err != nil {
		return fmt.Errorf("unable to sign transaction: %v", err)
	}

	// Publish the transaction.
	pubTxReq := &walletrpc.PublishTransactionRequest{
		SignedTransaction: signedTxResp.Transaction,
	}
	pubTxResp, err := txBroadcaster.PublishTransaction(context.TODO(),
		pubTxReq)
	if err != nil {
		return fmt.Errorf("unable to publish transaction: %v", err)
	}

	txid, err := chainhash.NewHash(pubTxResp.TransactionHash)
	if err != nil {
		return err
	}

	fees := outputs[feeAddr.String()]

	log.Infof("paid a total of %v in tx %s, including %v in pool fees.",
		tOut, txid.String(), fees)

	// Update all associated payments as paid and archive them.
	for _, set := range pmts {
		for _, pmt := range set {
			pmt.PaidOnHeight = height
			pmt.TransactionID = txid.String()
			err := pmt.Update(pm.cfg.DB)
			if err != nil {
				return fmt.Errorf("unable to update payment: %v", err)
			}

			err = pmt.Archive(pm.cfg.DB)
			if err != nil {
				return fmt.Errorf("unable to archive payment: %v", err)
			}
		}
	}

	// Update payments metadata.
	err = pm.cfg.DB.Update(func(tx *bolt.Tx) error {
		pm.setLastPaymentHeight(height)
		err = pm.persistLastPaymentHeight(tx)
		if err != nil {
			return err
		}

		pm.setLastPaymentPaidOn(uint64(time.Now().UnixNano()))
		return pm.persistLastPaymentPaidOn(tx)
	})
	if err != nil {
		return err
	}

	return nil
}
