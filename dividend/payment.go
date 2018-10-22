package dividend

import (
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/dcrutil"

	"dnldd/dcrpool/database"
)

var (
	// LastBatchOn is the key of the last time a payment batch was
	// persisted.
	LastBatchOn = []byte("lastbatchon")
)

// Payment represents an outstanding payment for a pool account.
type Payment struct {
	Account string         `json:"account"`
	Amount  dcrutil.Amount `json:"amount"`
}

// NewPayment creates a payment instance.
func NewPayment(account string, amount dcrutil.Amount) *Payment {
	return &Payment{
		Account: account,
		Amount:  amount,
	}
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

	// Fetch the last bundle time.
	var minTime []byte
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		v := pbkt.Get(LastBatchOn)
		minTime = v

		// If the last bundle time is not set the applicable share range should
		// be inclusive of all shares.
		if minTime == nil {
			minTime = NanoToBigEndianBytes(new(time.Time).UnixNano())
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Fetch all eligible shares for payment calculations.
	shares, err := FetchEligibleShares(db, minTime, nowNano)
	if err != nil {
		return err
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := CalculateSharePercentages(shares)
	if err != nil {
		return err
	}

	payments, err := CalculatePayments(db, percentages, amount, poolFee)

	// 30 minutes is added to the the estimated maturity as contingency for
	// network delays.
	estMaturity := (coinbaseMaturity * 5) + 30
	estMaturityNano := futureTime(&now, 0, 0, time.Duration(estMaturity),
		0).UnixNano()

	// Persist the payment batch.
	bundle := NewPaymentBatch(payments, estMaturityNano)
	bundle.Create(db)

	// Update the last batch time.
	err = db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		return pbkt.Put(LastBatchOn, nowNano)
	})

	if err != nil {
		log.Error(err)
	}

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

	payments, err := CalculatePayments(db, percentages, amount, poolFee)

	// 30 minutes is added to the the estimated maturity as contingency for
	// network delays.
	estMaturity := (coinbaseMaturity * 5) + 30
	estMaturityNano := futureTime(&now, 0, 0, time.Duration(estMaturity),
		0).UnixNano()

	// Persist the payment batch.
	bundle := NewPaymentBatch(payments, estMaturityNano)
	bundle.Create(db)

	return err
}
