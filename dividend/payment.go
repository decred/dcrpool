package dividend

import (
	"encoding/json"
	"fmt"
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
	Amount            dcrutil.Amount `json:"amount"`
	CreatedOn         int64          `json:"createdon"`
	PaidOn            int64          `json:"paidon"`
}

// NewPayment creates a payment instance.
func NewPayment(account string, amount dcrutil.Amount, estMaturity int64) *Payment {
	return &Payment{
		Account:           account,
		EstimatedMaturity: estMaturity,
		Amount:            amount,
		CreatedOn:         time.Now().UnixNano(),
	}
}

// GeneratePaymentID prefixes the provided account with the provided
// created on nano time to generate a unique id for the payment.
func GeneratePaymentID(account string, createdOnNano int64) []byte {
	id := fmt.Sprintf("%v%v", NanoToBigEndianBytes(createdOnNano), account)
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

		id := GeneratePaymentID(payment.Account, payment.CreatedOn)
		err = bkt.Put(id, paymentBytes)
		return err
	})
	return err
}

// Update persists the updated payment to the database.
func (payment *Payment) Update(db *bolt.DB) error {
	return payment.Create(db)
}

// Delete is not supported for payments.
func (payment *Payment) Delete(db *bolt.DB, state bool) error {
	return ErrNotSupported("payment", "delete")
}

// FetchMaturePendingPayments fetches all payments past their estimated
// maturities which have not been paid yet.
func FetchMaturePendingPayments(db *bolt.DB) ([]*Payment, error) {
	payments := make([]*Payment, 0)
	now := time.Now().UnixNano()
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
		var payment Payment
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			if payment.EstimatedMaturity < now && payment.PaidOn == 0 {
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
func FetchEligiblePayments(db *bolt.DB, minPayment dcrutil.Amount) ([]*Payment, error) {
	maturePayments, err := FetchMaturePendingPayments(db)
	if err != nil {
		return nil, err
	}

	eligiblePmts := make([]*Payment, 0)
	for _, payment := range maturePayments {
		if payment.Amount > minPayment {
			eligiblePmts = append(eligiblePmts, payment)
		}
	}

	return eligiblePmts, nil
}

// UpdatePayments updates payment records with the provided payments, a new
// payment recored is created if there is no existing payment record to
// update.
func UpdatePayments(db *bolt.DB, newPayments []*Payment) error {
	payments, err := FetchMaturePendingPayments(db)
	if err != nil {
		return err
	}

	for _, newPayment := range newPayments {
		updated := false
		for _, payment := range payments {
			if newPayment.Account == payment.Account {
				payment.Amount += newPayment.Amount
				payment.EstimatedMaturity = newPayment.EstimatedMaturity
				err := payment.Update(db)
				if err != nil {
					return err
				}

				// Update the last payment created time.
				err = db.Update(func(tx *bolt.Tx) error {
					pbkt := tx.Bucket(database.PoolBkt)
					if pbkt == nil {
						return database.ErrBucketNotFound(database.PoolBkt)
					}

					return pbkt.Put(LastPaymentCreatedOn,
						NanoToBigEndianBytes(newPayment.CreatedOn))
				})

				if err != nil {
					return err
				}

				updated = true
				break
			}
		}

		// Create a payment record for the account if one does not exist.
		if !updated {
			err := newPayment.Create(db)
			if err != nil {
				return err
			}

			// Update the last payment created time.
			err = db.Update(func(tx *bolt.Tx) error {
				pbkt := tx.Bucket(database.PoolBkt)
				if pbkt == nil {
					return database.ErrBucketNotFound(database.PoolBkt)
				}

				return pbkt.Put(LastPaymentCreatedOn,
					NanoToBigEndianBytes(newPayment.CreatedOn))
			})
		}
	}

	return nil
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
func PayPerShare(db *bolt.DB, amount dcrutil.Amount, poolFee float64, coinbaseMaturity uint16) error {
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

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := CalculateSharePercentages(shares)
	if err != nil {
		return err
	}

	// 30 minutes is added to the the estimated maturity as contingency for
	// network delays.
	estMaturityTime := (coinbaseMaturity * 5) + 30
	estMaturityNano := futureTime(&now, 0, 0, time.Duration(estMaturityTime),
		0).UnixNano()

	payments, err := CalculatePayments(db, percentages, amount, poolFee,
		estMaturityNano)

	// Update or create unpaid payment records for the participating accounts.
	err = UpdatePayments(db, payments)
	return err
}

// PayPerLastNShares generates a payment bundle comprised of payments to all
// participating accounts within the last n time period provided.
func PayPerLastNShares(db *bolt.DB, amount dcrutil.Amount, poolFee float64, coinbaseMaturity uint16, periodSecs uint32) error {
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

	// 30 minutes is added to the the estimated maturity as contingency for
	// network delays.
	estMaturityTime := (coinbaseMaturity * 5) + 30
	estMaturityNano := futureTime(&now, 0, 0, time.Duration(estMaturityTime),
		0).UnixNano()

	payments, err := CalculatePayments(db, percentages, amount, poolFee,
		estMaturityNano)

	// Update or create unpaid payment records for the participating accounts.
	err = UpdatePayments(db, payments)
	return err
}
