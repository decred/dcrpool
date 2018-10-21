package dividend

import (
	"encoding/json"
	"time"

	"github.com/coreos/bbolt"

	"dnldd/dcrpool/database"
)

// PaymentBatch represents payments to participating accounts for work done. This
// also includes pool fee payments.
type PaymentBatch struct {
	UUID              string     `json:"uuid"`
	Payments          []*Payment `json:"payments"`
	EstimatedMaturity int64      `json:"estimatedmaturity"`
	Paid              bool       `json:"paid"`
}

// NewPaymentBatch creates a payment batch instance.
func NewPaymentBatch(payments []*Payment, estimatedMaturity int64) *PaymentBatch {
	return &PaymentBatch{
		Payments:          payments,
		EstimatedMaturity: estimatedMaturity,
		Paid:              false,
	}
}

// GetPaymentBatch fetches the payment batch referenced by the provided id.
func GetPaymentBatch(db *bolt.DB, id []byte) (*PaymentBatch, error) {
	var batch PaymentBatch
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBatchBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBatchBkt)
		}
		v := bkt.Get(id)
		if v == nil {
			return database.ErrValueNotFound(id)
		}
		err := json.Unmarshal(v, &batch)
		return err
	})
	return &batch, err
}

// Create persists a payment batch to the database.
func (bundle *PaymentBatch) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBatchBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBatchBkt)
		}
		bundleBytes, err := json.Marshal(bundle)
		if err != nil {
			return err
		}

		err = bkt.Put([]byte(bundle.UUID), bundleBytes)
		return err
	})
	return err
}

// Update persists the updated payment batch to the database.
func (bundle *PaymentBatch) Update(db *bolt.DB) error {
	return bundle.Create(db)
}

// Delete is not supported for payment batches.
func (bundle *PaymentBatch) Delete(db *bolt.DB, state bool) error {
	return ErrNotSupported("payment batch", "delete")
}

// FetchMatureBatches fetches all batches past their estimated maturities.
func (bundle *PaymentBatch) FetchMatureBatches(db *bolt.DB) ([]*PaymentBatch, error) {
	batches := make([]*PaymentBatch, 0)
	now := time.Now().UnixNano()
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.PaymentBatchBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.PaymentBatchBkt)
		}

		cursor := bkt.Cursor()
		var batch PaymentBatch
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			err := json.Unmarshal(v, &batch)
			if err != nil {
				return err
			}

			if batch.EstimatedMaturity < now {
				batches = append(batches, &batch)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return batches, nil
}
