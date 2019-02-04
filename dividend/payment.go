package dividend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/davecgh/go-spew/spew"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/dcrutil"

	"github.com/dnldd/dcrpool/database"
)

var (
	// LastPaymentCreatedOn is the key of the last time a payment was
	// persisted.
	LastPaymentCreatedOn = []byte("lastpaymentcreatedon")

	// LastPaymentPaidOn is the key of the last time a payment was
	// paid.
	LastPaymentPaidOn = []byte("lastpaymentpaidon")

	// LastPaymentHeight is the key of the last payment height.
	LastPaymentHeight = []byte("lastpaymentheight")

	// TxFeeReserve is the key of the tx fee reserve.
	TxFeeReserve = []byte("txfeereserve")
)

// Payment represents an outstanding payment for a pool account.
type Payment struct {
	Account           string         `json:"account"`
	EstimatedMaturity uint32         `json:"estimatedmaturity"`
	Height            uint32         `json:"height"`
	Amount            dcrutil.Amount `json:"amount"`
	CreatedOn         int64          `json:"createdon"`
	PaidOnHeight      uint32         `json:"paidonheight"`
}

// NewPayment creates a payment instance.
func NewPayment(account string, amount dcrutil.Amount, height uint32, estMaturity uint32) *Payment {
	return &Payment{
		Account:           account,
		Amount:            amount,
		Height:            height,
		EstimatedMaturity: estMaturity,
		CreatedOn:         time.Now().UnixNano(),
	}
}

// GeneratePaymentID generates a unique id using the provided account, created
// on nano time and the block height.
func GeneratePaymentID(createdOnNano int64, height uint32, account string) []byte {
	id := fmt.Sprintf("%v%v%v", NanoToBigEndianBytes(createdOnNano),
		height, account)
	return []byte(id)
}

// GetPayment fetches the payment referenced by the provided id.
func GetPayment(db *bolt.DB, id []byte) (*Payment, error) {
	var payment Payment
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBkt)
		}
		v := bkt.Get(id)
		if v == nil {
			return database.ErrValueNotFound(id)
		}
		err := json.Unmarshal(v, &payment)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &payment, err
}

// Create persists a payment to the database.
func (payment *Payment) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBkt)
		}
		paymentBytes, err := json.Marshal(payment)
		if err != nil {
			return err
		}

		id := GeneratePaymentID(payment.CreatedOn, payment.Height,
			payment.Account)
		err = bkt.Put(id, paymentBytes)
		return err
	})
	return err
}

// Update persists the updated payment to the database.
func (payment *Payment) Update(db *bolt.DB) error {
	return payment.Create(db)
}

// Delete purges the referenced pending payment from the database.
func (payment *Payment) Delete(db *bolt.DB) error {
	id := GeneratePaymentID(payment.CreatedOn, payment.Height, payment.Account)
	return database.Delete(db, database.PaymentBkt, id)
}

// PaymentBundle is a convenience type for grouping payments for an account.
type PaymentBundle struct {
	Account  string
	Payments []*Payment
}

// NewPaymentBundle initializes a payment bundle instance.
func NewPaymentBundle(account string) *PaymentBundle {
	return &PaymentBundle{
		Account:  account,
		Payments: make([]*Payment, 0),
	}
}

// Total returns the sum of all payments amount for the account.
func (bundle *PaymentBundle) Total() dcrutil.Amount {
	total := dcrutil.Amount(0)
	for _, payment := range bundle.Payments {
		total += payment.Amount
	}

	return total
}

// UpdateAsPaid updates all associated payments referenced by a payment bundle
// as paid.
func (bundle *PaymentBundle) UpdateAsPaid(db *bolt.DB, height uint32) {
	for idx := 0; idx < len(bundle.Payments); idx++ {
		bundle.Payments[idx].PaidOnHeight = height
	}
}

// ArchivePayments removes all payments included in the payment bundle from the
// payment bucket and archives them.
func (bundle *PaymentBundle) ArchivePayments(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		pmtbkt := pbkt.Bucket(database.PaymentBkt)
		if pmtbkt == nil {
			return database.ErrBucketNotFound(database.PaymentBkt)
		}

		abkt := pbkt.Bucket(database.PaymentArchiveBkt)
		if abkt == nil {
			return database.ErrBucketNotFound(database.PaymentArchiveBkt)
		}

		for _, pmt := range bundle.Payments {
			id := GeneratePaymentID(pmt.CreatedOn, pmt.Height, pmt.Account)
			err := pmtbkt.Delete(id)
			if err != nil {
				return err
			}

			pmt.CreatedOn = time.Now().UnixNano()
			pmtBytes, err := json.Marshal(pmt)
			if err != nil {
				return err
			}

			id = GeneratePaymentID(pmt.CreatedOn, pmt.Height, pmt.Account)
			err = abkt.Put(id, pmtBytes)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// GeneratePaymentBundles creates account payment bundles from the provided
// set of payments.
func GeneratePaymentBundles(payments []*Payment) []*PaymentBundle {
	bundles := make([]*PaymentBundle, 0)
	for _, payment := range payments {
		match := false
		for _, bdl := range bundles {
			if payment.Account == bdl.Account {
				bdl.Payments = append(bdl.Payments, payment)
				match = true
				break
			}
		}

		if !match {
			bdl := NewPaymentBundle(payment.Account)
			bdl.Payments = append(bdl.Payments, payment)
			bundles = append(bundles, bdl)
		}
	}

	return bundles
}

// FetchPendingPayments fetches all unpaid payments.
func FetchPendingPayments(db *bolt.DB) ([]*Payment, error) {
	payments := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBkt)
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

// FetchMaturePendingPayments fetches all payments past their estimated
// maturities which have not been paid yet.
func FetchMaturePendingPayments(db *bolt.DB, height uint32) ([]*Payment, error) {
	payments := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBkt)
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			if payment.PaidOnHeight == 0 &&
				payment.EstimatedMaturity <= height {
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

// FetchPendingPaymentsAtHeight fetches all pending payments at the provided
// height.
func FetchPendingPaymentsAtHeight(db *bolt.DB, height uint32) ([]*Payment, error) {
	payments := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBkt)
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			if payment.PaidOnHeight == 0 && payment.Height == height {
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

// FetchEligiblePaymentBundles fetches payment bundles greater than the
// configured minimum payment.
func FetchEligiblePaymentBundles(db *bolt.DB, height uint32, minPayment dcrutil.Amount) ([]*PaymentBundle, error) {
	maturePayments, err := FetchMaturePendingPayments(db, height)
	if err != nil {
		return nil, err
	}

	bundles := GeneratePaymentBundles(maturePayments)
	for idx := 0; idx < len(bundles); idx++ {
		if bundles[idx].Total() < minPayment {
			bundles = append(bundles[:idx], bundles[idx+1:]...)
		}
	}

	return bundles, nil
}

// replenishTxFeeReserve adjusts the pool fee amount supplied to leave the specified
// max tx fee reserve.
func replenishTxFeeReserve(maxTxFeeReserve dcrutil.Amount, txFeeReserve *dcrutil.Amount, poolFee dcrutil.Amount) dcrutil.Amount {
	if *txFeeReserve < maxTxFeeReserve {
		diff := maxTxFeeReserve - *txFeeReserve
		*txFeeReserve += maxTxFeeReserve
		return poolFee - diff
	}

	return poolFee
}

// PayPerShare generates a payment bundle comprised of payments to all
// participating accounts. Payments are calculated based on work contributed
// to the pool since the last payment batch.
func PayPerShare(db *bolt.DB, total dcrutil.Amount, poolFee float64, height uint32, coinbaseMaturity uint16) error {
	now := time.Now()
	nowNano := NanoToBigEndianBytes(now.UnixNano())

	// Fetch the last payment created time.
	var lastPaymentTimeNano []byte
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		v := pbkt.Get(LastPaymentCreatedOn)
		lastPaymentTimeNano = v

		return nil
	})

	if err != nil {
		return err
	}

	// Fetch all eligible shares for payment calculations.
	shares, err := PPSEligibleShares(db, lastPaymentTimeNano, nowNano)
	if err != nil {
		return err
	}

	if len(shares) == 0 {
		return fmt.Errorf("no eligible shares found (PPS)")
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := CalculateSharePercentages(shares)
	if err != nil {
		return err
	}

	log.Tracef("Share percentages are: %v", spew.Sdump(percentages))

	var estMaturity uint32

	// Allow immediately mature payments for testing purposes.
	if coinbaseMaturity == 0 {
		estMaturity = height
	}

	if coinbaseMaturity > 0 {
		estMaturity = height + uint32(coinbaseMaturity)
	}

	payments, err := CalculatePayments(percentages, total, poolFee, height,
		estMaturity)
	if err != nil {
		return err
	}

	// Persist all payments.
	for _, payment := range payments {
		err := payment.Create(db)
		if err != nil {
			return err
		}
	}

	log.Tracef("new payouts (PPS) at height (%v), matures at height (%v).",
		height, height+uint32(coinbaseMaturity))

	// Update the last payment created time.
	err = db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		return pbkt.Put(LastPaymentCreatedOn,
			NanoToBigEndianBytes(payments[len(payments)-1].CreatedOn))
	})

	return err
}

// PayPerLastNShares generates a payment bundle comprised of payments to all
// participating accounts within the last n time period provided.
func PayPerLastNShares(db *bolt.DB, amount dcrutil.Amount, poolFee float64, height uint32, coinbaseMaturity uint16, periodSecs uint32) error {
	now := time.Now()
	min := now.Add(-(time.Second * time.Duration(periodSecs)))
	minNano := NanoToBigEndianBytes(min.UnixNano())

	// Fetch all eligible shares within the specified period.
	shares, err := PPLNSEligibleShares(db, minNano)
	if err != nil {
		return err
	}

	if len(shares) == 0 {
		return fmt.Errorf("no eligible shares found (PPLNS)")
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := CalculateSharePercentages(shares)
	if err != nil {
		return err
	}

	log.Tracef("Share percentages are: %v", spew.Sdump(percentages))

	var estMaturity uint32

	// Allow immediately mature payments for testing purposes.
	if coinbaseMaturity == 0 {
		estMaturity = height
	}

	if coinbaseMaturity > 0 {
		estMaturity = height + uint32(coinbaseMaturity)
	}

	payments, err := CalculatePayments(percentages, amount, poolFee, height,
		estMaturity)
	if err != nil {
		return err
	}

	// Persist all payments.
	for _, payment := range payments {
		err := payment.Create(db)
		if err != nil {
			return err
		}
	}

	log.Tracef("new payouts (PPLNS) at height (%v), matures at height (%v)",
		height, height+uint32(coinbaseMaturity))

	// Update the last payment created time.
	err = db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		return pbkt.Put(LastPaymentCreatedOn,
			NanoToBigEndianBytes(payments[len(payments)-1].CreatedOn))
	})

	return err
}

// GeneratePaymentDetails generates kv pair of addresses and payment amounts
// from the provided eligible payments.
func GeneratePaymentDetails(db *bolt.DB, poolFeeAddrs []dcrutil.Address, eligiblePmts []*PaymentBundle, maxTxFeeReserve dcrutil.Amount, txFeeReserve *dcrutil.Amount) (map[string]dcrutil.Amount, *dcrutil.Amount, error) {
	// Generate the address and payment amount kv pairs.
	var targetAmt dcrutil.Amount
	pmts := make(map[string]dcrutil.Amount)

	// Fetch a pool fee address at random.
	rand.Seed(time.Now().UnixNano())
	addr := poolFeeAddrs[rand.Intn(len(poolFeeAddrs))]

	for _, p := range eligiblePmts {
		// For pool fee payments, use the fetched address.
		if p.Account == PoolFeesK {
			bundleAmt := p.Total()
			pmts[addr.String()] = bundleAmt
			targetAmt += bundleAmt
			continue
		}

		// For a dividend payment, fetch the corresponding account address.
		acc, err := FetchAccount(db, []byte(p.Account))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to fetch account: %v", err)
		}

		bundleAmt := p.Total()
		pmts[acc.Address] = bundleAmt
		targetAmt += bundleAmt
	}

	// replenish the tx fee reserve if a pool fee bundle entry exists.
	poolFee, ok := pmts[addr.String()]
	if ok {
		updatedPoolFee := replenishTxFeeReserve(maxTxFeeReserve, txFeeReserve,
			poolFee)
		pmts[addr.String()] = updatedPoolFee
	}

	return pmts, &targetAmt, nil
}

// FetchArchivedPaymentsForAccount fetches archived payments for the provided
// account that were created before the provided timestamp.
func FetchArchivedPaymentsForAccount(db *bolt.DB, account []byte, minNano []byte) ([]*Payment, error) {
	pmts := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		abkt := pbkt.Bucket(database.PaymentArchiveBkt)
		if abkt == nil {
			return database.ErrBucketNotFound(database.PaymentArchiveBkt)
		}

		c := abkt.Cursor()
		for k, v := c.Next(); k != nil && (bytes.Equal(k[12:], account) &&
			bytes.Compare(k[:8], minNano) > 0); k, v = c.Next() {
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
