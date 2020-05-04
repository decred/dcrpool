package pool

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/mempool/v3"
	txrules "github.com/decred/dcrwallet/wallet/v3/txrules"
	bolt "go.etcd.io/bbolt"
)

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
	// MinPayment represents the minimum payment eligible for processing by the
	// pool.
	MinPayment dcrutil.Amount
	// PoolFeeAddrs represents the pool fee addresses of the pool.
	PoolFeeAddrs []dcrutil.Address
	// MaxTxFeeReserve represents the maximum value the tx free reserve can be.
	MaxTxFeeReserve dcrutil.Amount
	// PublishTransaction generates a transaction from the provided payouts
	// and publishes it.
	PublishTransaction func(map[dcrutil.Address]dcrutil.Amount, dcrutil.Amount) (string, error)
	// SignalCache sends the provided cache update event to the gui cache.
	SignalCache func(event CacheUpdateEvent)
}

// PaymentMgr handles generating shares and paying out dividends to
// participating accounts.
type PaymentMgr struct {
	lastPaymentHeight    uint32 // update atomically.
	lastPaymentPaidOn    uint64 // update atomically.
	lastPaymentCreatedOn uint64 // update atomically.

	cfg             *PaymentMgrConfig
	txFeeReserve    dcrutil.Amount
	txFeeReserveMtx sync.RWMutex
	paymentReqs     map[string]struct{}
	paymentReqsMtx  sync.RWMutex
}

// NewPaymentMgr creates a new payment manager.
func NewPaymentMgr(pCfg *PaymentMgrConfig) (*PaymentMgr, error) {
	pm := &PaymentMgr{
		cfg:          pCfg,
		txFeeReserve: dcrutil.Amount(0),
		paymentReqs:  make(map[string]struct{}),
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
		err = pm.loadLastPaymentCreatedOn(tx)
		if err != nil {
			return err
		}
		return pm.loadTxFeeReserve(tx)
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
	minBytes := nanoToBigEndianBytes(minNano)
	bkt, err := fetchShareBucket(tx)
	if err != nil {
		return err
	}
	toDelete := [][]byte{}
	cursor := bkt.Cursor()
	for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
		if bytes.Compare(minBytes, k) > 0 {
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

// setTxFeeReserve updates the tx fee reserve.
func (pm *PaymentMgr) setTxFeeReserve(amt dcrutil.Amount) {
	pm.txFeeReserveMtx.Lock()
	pm.txFeeReserve = amt
	pm.txFeeReserveMtx.Unlock()
}

// fetchTxFeeReserve fetches the tx fee reserves.
func (pm *PaymentMgr) fetchTxFeeReserve() dcrutil.Amount {
	pm.txFeeReserveMtx.RLock()
	defer pm.txFeeReserveMtx.RUnlock()
	return pm.txFeeReserve
}

// persistTxFeeReserve saves the tx fee reserve to the db.
func (pm *PaymentMgr) persistTxFeeReserve(tx *bolt.Tx) error {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(pm.txFeeReserve))
	return pbkt.Put(txFeeReserve, b)
}

// loadTxFeeReserve fetches the tx fee reserve from the db.
func (pm *PaymentMgr) loadTxFeeReserve(tx *bolt.Tx) error {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}
	txFeeReserveB := pbkt.Get(txFeeReserve)
	if txFeeReserveB == nil {
		pm.setTxFeeReserve(dcrutil.Amount(0))
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, 0)
		return pbkt.Put(txFeeReserve, b)
	}
	amt := dcrutil.Amount(binary.LittleEndian.Uint32(txFeeReserveB))
	pm.setTxFeeReserve(amt)
	return nil
}

// replenishTxFeeReserve uses collected pool fees to replenish the
// pool's tx fee reserve. The remaining pool fee amount after replenishing
// the fee reserve is returned.
func (pm *PaymentMgr) replenishTxFeeReserve(poolFee dcrutil.Amount) dcrutil.Amount {
	txFeeReserve := pm.fetchTxFeeReserve()
	if txFeeReserve < pm.cfg.MaxTxFeeReserve {
		diff := pm.cfg.MaxTxFeeReserve - txFeeReserve
		if poolFee > diff {
			pm.setTxFeeReserve(txFeeReserve + diff)
			return poolFee - diff
		}
		pm.setTxFeeReserve(txFeeReserve + poolFee)
		return dcrutil.Amount(0)
	}
	return poolFee
}

// PPLNSSharePercentages calculates the current mining reward percentages
// due participating pool accounts based on work performed measured by
// the PPS payment scheme.
func (pm *PaymentMgr) PPSSharePercentages() (map[string]*big.Rat, error) {
	now := nanoToBigEndianBytes(time.Now().UnixNano())
	lastPaymentCreatedOn := pm.fetchLastPaymentCreatedOn()
	shares, err := PPSEligibleShares(pm.cfg.DB, nanoToBigEndianBytes(int64(lastPaymentCreatedOn)), now)
	if err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}
	percentages, err := sharePercentages(shares)
	if err != nil {
		return nil, err
	}
	return percentages, nil
}

// PPLNSSharePercentages calculates the current mining reward percentages due pool
// accounts based on work performed measured by the PPLNS payment scheme.
func (pm *PaymentMgr) PPLNSSharePercentages() (map[string]*big.Rat, error) {
	now := time.Now()
	min := now.Add(-pm.cfg.LastNPeriod)
	minNano := nanoToBigEndianBytes(min.UnixNano())
	shares, err := PPLNSEligibleShares(pm.cfg.DB, minNano)
	if err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := sharePercentages(shares)
	if err != nil {
		return nil, err
	}
	return percentages, nil
}

// PayPerShare generates a payment bundle comprised of payments to all
// participating accounts. Payments are calculated based on work contributed
// to the pool since the last payment batch.
func (pm *PaymentMgr) payPerShare(source *PaymentSource, amt dcrutil.Amount, height uint32) error {
	now := time.Now()
	percentages, err := pm.PPSSharePercentages()
	if err != nil {
		return err
	}
	estMaturity := height + uint32(pm.cfg.ActiveNet.CoinbaseMaturity)
	payments, err := CalculatePayments(percentages, source, amt, pm.cfg.PoolFee,
		height, estMaturity)
	if err != nil {
		return err
	}
	for _, payment := range payments {
		err := payment.Create(pm.cfg.DB)
		if err != nil {
			return err
		}
	}
	lastPaymentCreatedOn := uint64(payments[len(payments)-1].CreatedOn)
	pm.setLastPaymentCreatedOn(lastPaymentCreatedOn)
	err = pm.cfg.DB.Update(func(tx *bolt.Tx) error {
		// Update the last payment created on time and prune invalidated shares.
		err := pm.persistLastPaymentCreatedOn(tx)
		if err != nil {
			return err
		}
		return pm.pruneShares(tx, now.UnixNano())
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
	payments, err := CalculatePayments(percentages, source, amt, pm.cfg.PoolFee,
		height, estMaturity)
	if err != nil {
		return err
	}
	for _, payment := range payments {
		err := payment.Create(pm.cfg.DB)
		if err != nil {
			return err
		}
	}
	lastPaymentCreatedOn := uint64(payments[len(payments)-1].CreatedOn)
	pm.setLastPaymentCreatedOn(lastPaymentCreatedOn)
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
func (pm *PaymentMgr) generatePayments(height uint32, source *PaymentSource, amt dcrutil.Amount) error {
	switch pm.cfg.PaymentMethod {
	case PPS:
		return pm.payPerShare(source, amt, height)

	case PPLNS:
		return pm.payPerLastNShares(source, amt, height)

	default:
		return fmt.Errorf("unknown payment method provided %v", pm.cfg.PaymentMethod)
	}
}

// isPaymentRequested checks if a payment request exists for the
// provided account.
func (pm *PaymentMgr) isPaymentRequested(accountID string) bool {
	pm.paymentReqsMtx.RLock()
	defer pm.paymentReqsMtx.RUnlock()
	_, ok := pm.paymentReqs[accountID]
	return ok
}

// addPaymentRequest creates a payment request from the provided account
// if not already requested.
func (pm *PaymentMgr) addPaymentRequest(addr string) error {
	id, err := AccountID(addr, pm.cfg.ActiveNet)
	if err != nil {
		return err
	}
	if pm.isPaymentRequested(id) {
		return fmt.Errorf("payment already requested for account"+
			" with id %s", id)
	}
	pm.paymentReqsMtx.Lock()
	pm.paymentReqs[id] = struct{}{}
	pm.paymentReqsMtx.Unlock()
	return nil
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
func (pm *PaymentMgr) maturePendingPayments(height uint32) (map[uint32][]*Payment, error) {
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

	pmts := make(map[uint32][]*Payment)
	for _, pmt := range payments {
		set, ok := pmts[pmt.Height]
		if !ok {
			set = make([]*Payment, 0)
		}

		set = append(set, pmt)
		pmts[pmt.Height] = set
	}

	return pmts, nil
}

// PayDividends pays mature mining rewards to participating accounts.
func (pm *PaymentMgr) payDividends(height uint32) error {
	// Waiting two blocks after a successful payment before proceeding with
	// the next one because the reserved amount for transaction fees becomes
	// change after a successful transaction. Change matures after the next
	// block is processed. The second block is as a result of trying to
	// maximize the transaction fee usage by processing mature payments
	// after the transaction fees reserve has matured and ready for another
	// transaction.
	lastPaymentHeight := pm.fetchLastPaymentHeight()
	if lastPaymentHeight != 0 && (height-lastPaymentHeight) < 3 {
		return nil
	}
	eligiblePmts, err := pm.fetchEligiblePaymentBundles(height)
	if err != nil {
		return err
	}
	pm.paymentReqsMtx.Lock()
	for accountID := range pm.paymentReqs {
		delete(pm.paymentReqs, accountID)
	}
	pm.paymentReqsMtx.Unlock()

	if len(eligiblePmts) == 0 {
		return nil
	}

	addr := pm.cfg.PoolFeeAddrs[rand.Intn(len(pm.cfg.PoolFeeAddrs))]
	pmtDetails, targetAmt, err := generatePaymentDetails(pm.cfg.DB, addr, eligiblePmts)
	if err != nil {
		return err
	}
	poolFee, ok := pmtDetails[addr.String()]
	if ok {
		// Replenish the tx fee reserve if a pool fee bundle entry exists.
		updatedFee := pm.replenishTxFeeReserve(poolFee)
		if updatedFee == 0 {
			delete(pmtDetails, addr.String())
		} else {
			pmtDetails[addr.String()] = updatedFee
		}
	}
	pmts := make(map[dcrutil.Address]dcrutil.Amount, len(pmtDetails))
	for dest, amt := range pmtDetails {
		addr, err := dcrutil.DecodeAddress(dest, pm.cfg.ActiveNet)
		if err != nil {
			return err
		}
		pmts[addr] = amt
	}

	txid, err := pm.cfg.PublishTransaction(pmts, *targetAmt)
	if err != nil {
		return err
	}
	for _, bundle := range eligiblePmts {
		bundle.UpdateAsPaid(pm.cfg.DB, height, txid)
		err = bundle.ArchivePayments(pm.cfg.DB)
		if err != nil {
			return err
		}
	}
	err = pm.cfg.DB.Update(func(tx *bolt.Tx) error {
		err = pm.persistTxFeeReserve(tx)
		if err != nil {
			return err
		}
		pm.setLastPaymentHeight(height)
		err = pm.persistLastPaymentHeight(tx)
		if err != nil {
			return err
		}
		return pm.persistLastPaymentPaidOn(tx)
	})
	if err != nil {
		return err
	}

	// Signal the gui cache of the paid dividends.
	if pm.cfg.SignalCache != nil {
		pm.cfg.SignalCache(DividendsPaid)
	}

	return nil
}
