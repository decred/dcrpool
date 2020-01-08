package pool

import (
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/mempool/v3"
	txrules "github.com/decred/dcrwallet/wallet/v3/txrules"
)

type PaymentMgrConfig struct {
	// DB represents the pool database.
	DB *bolt.DB
	// ActiveNet represents the network being mined on.
	ActiveNet *chaincfg.Params
	// PoolFee represents the fee charged to participating accounts of the pool.
	PoolFee float64
	// LastNPeriod represents the period to source shares from with the
	// PPLNS payment scheme.
	LastNPeriod uint32
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

// bigEndianBytesToNano returns nanosecond time from the provided
// big endian bytes.
func bigEndianBytesToNano(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

// loadLastPaymentPaidOn fetches the last payment paid on time from the db.
func (pm *PaymentMgr) loadLastPaymentPaidOn(tx *bolt.Tx) error {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
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
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}
	return pbkt.Put(lastPaymentCreatedOn,
		nanoToBigEndianBytes(int64(pm.lastPaymentCreatedOn)))
}

// loadLastPaymentCreaedOn fetches the last payment created on time from the db.
func (pm *PaymentMgr) loadLastPaymentCreatedOn(tx *bolt.Tx) error {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
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
	min := now.Add(-(time.Second * time.Duration(pm.cfg.LastNPeriod)))
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
func (pm *PaymentMgr) payPerShare(coinbase dcrutil.Amount, height uint32) error {
	now := time.Now()
	percentages, err := pm.PPSSharePercentages()
	if err != nil {
		return err
	}
	estMaturity := height + uint32(pm.cfg.ActiveNet.CoinbaseMaturity)
	payments, err := CalculatePayments(percentages, coinbase, pm.cfg.PoolFee, height, estMaturity)
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
		return pruneShares(tx, now.UnixNano())
	})
	return err
}

// payPerLastNShares generates a payment bundle comprised of payments to all
// participating accounts within the lastNPeriod of the pool.
func (pm *PaymentMgr) payPerLastNShares(coinbase dcrutil.Amount, height uint32) error {
	percentages, err := pm.PPLNSSharePercentages()
	if err != nil {
		return err
	}
	var estMaturity uint32
	coinbaseMaturity := pm.cfg.ActiveNet.CoinbaseMaturity
	if coinbaseMaturity == 0 {
		// Allow immediately mature payments for testing purposes.
		estMaturity = height
	}
	if coinbaseMaturity > 0 {
		estMaturity = height + uint32(coinbaseMaturity)
	}
	payments, err := CalculatePayments(percentages, coinbase, pm.cfg.PoolFee,
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
		minNano := time.Now().Add(-(time.Second * time.Duration(pm.cfg.LastNPeriod))).UnixNano()
		return pruneShares(tx, minNano)
	})
	return err
}

// generatePayments creates payments for participating accounts. This should
// only be called when a block is confirmed mined, in pool mining mode.
func (pm *PaymentMgr) generatePayments(height uint32, coinbase dcrutil.Amount) error {
	cfg := pm.cfg
	switch cfg.PaymentMethod {
	case PPS:
		return pm.payPerShare(coinbase, height)

	case PPLNS:
		return pm.payPerLastNShares(coinbase, height)

	default:
		return fmt.Errorf("unknown payment method provided %v", cfg.PaymentMethod)
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
	_, err := dcrutil.DecodeAddress(addr, pm.cfg.ActiveNet)
	if err != nil {
		return err
	}
	id, err := AccountID(addr)
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

// fetchEligiblePaymentBundles fetches payment bundles greater than the
// configured minimum payment.
func (pm *PaymentMgr) fetchEligiblePaymentBundles(height uint32) ([]*PaymentBundle, error) {
	maturePayments, err := fetchMaturePendingPayments(pm.cfg.DB, height)
	if err != nil {
		return nil, err
	}
	bundles := generatePaymentBundles(maturePayments)

	// Iterating the bundles backwards implicitly handles decrementing the
	// slice index when a bundle entry in the slice is removed.
	for idx := len(bundles) - 1; idx >= 0; idx-- {
		if bundles[idx].Total() < pm.cfg.MinPayment {
			// Remove payments below the minimum payment if they have not been
			// requested for by the user.
			if !pm.isPaymentRequested(bundles[idx].Account) {
				bundles = append(bundles[:idx], bundles[idx+1:]...)
				continue
			}
			if txrules.IsDustAmount(
				bundles[idx].Total(),
				25, // P2PKHScriptSize
				mempool.DefaultMinRelayTxFee) {
				bundles = append(bundles[:idx], bundles[idx+1:]...)
			}
		}
	}
	return bundles, nil
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
	return err
}
