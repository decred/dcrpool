package dividend

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/dcrutil"

	"dnldd/dcrpool/database"
)

var (
	// LastPaymentCreatedOn is the key of the last time a payment was
	// persisted.
	LastPaymentCreatedOn = []byte("lastpaymentcreatedon")

	// LastPaymentPaidOn is the key of the last time a payment was
	// paid.
	LastPaymentPaidOn = []byte("lastpaymentpaidon")
)

// Payment represents an outstanding payment for a pool account.
type Payment struct {
	Account           string         `json:"account"`
	EstimatedMaturity int64          `json:"estimatedmaturity"`
	Height            uint32         `json:"height"`
	Amount            dcrutil.Amount `json:"amount"`
	CreatedOn         int64          `json:"createdon"`
	PaidOn            int64          `json:"paidon"`
}

// NewPayment creates a payment instance.
func NewPayment(account string, amount dcrutil.Amount, height uint32, estMaturity int64) *Payment {
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
	id := fmt.Sprintf("%v%v%v", NanoToBigEndianBytes(createdOnNano), height, account)
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
func (payment *Payment) Delete(db *bolt.DB, state bool) error {
	if payment.PaidOn != 0 {
		return fmt.Errorf("Cannot delete a paid payment record:"+
			" (%v at height %v for account %v)", payment.Amount,
			payment.Height, payment.Account)
	}

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

// UpdateAsPaid updates all associated payments to a payment bundle as paid.
func (bundle *PaymentBundle) UpdateAsPaid(db *bolt.DB, nowNano int64) error {
	for _, payment := range bundle.Payments {
		payment.PaidOn = nowNano
		err := payment.Update(db)
		if err != nil {
			return err
		}
	}

	return nil
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

			if payment.PaidOn == 0 {
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
func FetchMaturePendingPayments(db *bolt.DB) ([]*Payment, error) {
	payments := make([]*Payment, 0)
	nowNano := time.Now().UnixNano()
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

			if payment.PaidOn == 0 && payment.EstimatedMaturity < nowNano {
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

			if payment.PaidOn == 0 && payment.Height == height {
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

// FetchEligiblePayments fetches all payments over the configured minimum
// payment for the pool.
func FetchEligiblePayments(db *bolt.DB, minPayment dcrutil.Amount) ([]*PaymentBundle, error) {
	maturePayments, err := FetchMaturePendingPayments(db)
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

// futureTime extends a base time to a time in the future.
func futureTime(date *time.Time, days time.Duration, hours time.Duration,
	minutes time.Duration, seconds time.Duration) *time.Time {
	duration := ((time.Hour * 24) * days) + (time.Hour * hours) +
		(time.Minute * minutes) + (time.Second * seconds)
	futureTime := date.Add(duration)
	return &futureTime
}

// pastTime regresses a base time to a time in the past.
func pastTime(date *time.Time, days time.Duration, hours time.Duration,
	minutes time.Duration, seconds time.Duration) time.Time {
	duration := ((time.Hour * 24) * days) + (time.Hour * hours) +
		(time.Minute * minutes) + (time.Second * seconds)
	pastTime := date.Add(-duration)
	return pastTime
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
		lastPaymentTimeNano := v

		// If the last payment created time is not set the applicable share
		// range should be inclusive of all shares.
		if lastPaymentTimeNano == nil {
			lastPaymentTimeNano = NanoToBigEndianBytes(new(time.Time).UnixNano())
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Fetch all eligible shares for payment calculations.
	shares, err := FetchEligibleShares(db, lastPaymentTimeNano, nowNano)
	if err != nil {
		return err
	}

	if len(shares) == 0 {
		return fmt.Errorf("no eligible shares found")
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := CalculateSharePercentages(shares)
	if err != nil {
		return err
	}

	var estMaturityNano int64
	// Allow immediately mature payments for testing purposes.
	if coinbaseMaturity == 0 {
		estMaturityNano = now.UnixNano()
	}

	if coinbaseMaturity > 0 {
		// the estimated maturity is extended by 30 minutes as contingency for
		// network delays.
		estMaturityTime := (coinbaseMaturity * 5) + 30
		estMaturityNano = futureTime(&now, 0, 0, time.Duration(estMaturityTime),
			0).UnixNano()
	}

	payments, err := CalculatePayments(percentages, total, poolFee, height,
		estMaturityNano)

	// Persist all payments.
	for _, payment := range payments {
		err := payment.Create(db)
		if err != nil {
			return err
		}
	}

	// Update the last payment created time.
	err = db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		return pbkt.Put(LastPaymentCreatedOn,
			NanoToBigEndianBytes(payments[len(payments)-1].CreatedOn))
	})

	if err != nil {
		return err
	}

	return err
}

// PayPerLastNShares generates a payment bundle comprised of payments to all
// participating accounts within the last n time period provided.
func PayPerLastNShares(db *bolt.DB, amount dcrutil.Amount, poolFee float64, height uint32, coinbaseMaturity uint16, periodSecs uint32) error {
	now := time.Now()
	startTime := pastTime(&now, 0, 0, 0, time.Duration(periodSecs))
	nowNano := NanoToBigEndianBytes(now.UnixNano())
	startNano := NanoToBigEndianBytes(startTime.UnixNano())

	// Fetch all eligible shares within the specified period.
	shares, err := FetchEligibleShares(db, startNano, nowNano)
	if err != nil {
		return err
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := CalculateSharePercentages(shares)
	if err != nil {
		return err
	}

	var estMaturityNano int64
	// Allow immediately mature payments for testing purposes.
	if coinbaseMaturity == 0 {
		estMaturityNano = now.UnixNano()
	}

	if coinbaseMaturity > 0 {
		// the estimated maturity is extended by 30 minutes as contingency for
		// network delays.
		estMaturityTime := (coinbaseMaturity * 5) + 30
		estMaturityNano = futureTime(&now, 0, 0, time.Duration(estMaturityTime),
			0).UnixNano()
	}

	payments, err := CalculatePayments(percentages, amount, poolFee, height,
		estMaturityNano)

	// Persist all payments.
	for _, payment := range payments {
		err := payment.Create(db)
		if err != nil {
			return err
		}
	}

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
func GeneratePaymentDetails(db *bolt.DB, poolFeeAddrs []dcrutil.Address, eligiblePmts []*PaymentBundle) (map[string]dcrutil.Amount, *dcrutil.Amount, error) {
	// Generate the address and payment amount kv pairs.
	var targetAmt dcrutil.Amount
	pmts := make(map[string]dcrutil.Amount, 0)
	for _, p := range eligiblePmts {
		var addr dcrutil.Address

		// For pool fee payments, fetch a pool fee address at random.
		if p.Account == PoolFeesK {
			rand.Seed(time.Now().UnixNano())
			addr = poolFeeAddrs[rand.Intn(len(poolFeeAddrs))]

			bundleAmt := p.Total()
			pmts[addr.String()] = bundleAmt
			targetAmt += bundleAmt
			continue
		}

		// For a dividend payment, fetch the corresponding account address.
		acc, err := GetAccount(db, GenerateAccountID(p.Account))
		if err != nil {
			return nil, nil, err
		}

		bundleAmt := p.Total()
		pmts[acc.Address] = bundleAmt
		targetAmt += bundleAmt
	}

	return pmts, &targetAmt, nil
}
