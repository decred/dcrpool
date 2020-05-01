// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrpool/pool/errors"
	bolt "go.etcd.io/bbolt"
)

// PaymentSource represents the payment's source of funds.
type PaymentSource struct {
	BlockHash string `json:"blockhash"`
	Coinbase  string `json:"coinbase"`
}

// Payment represents value paid to a pool account or collected fees.
type Payment struct {
	Account           string         `json:"account"`
	EstimatedMaturity uint32         `json:"estimatedmaturity"`
	Height            uint32         `json:"height"`
	Amount            dcrutil.Amount `json:"amount"`
	CreatedOn         int64          `json:"createdon"`
	PaidOnHeight      uint32         `json:"paidonheight"`
	TransactionID     string         `json:"transactionid"`

	// The source could be empty if the payment was
	// created before the version 3 db upgrade.
	Source *PaymentSource `json:"source"`
}

// NewPayment creates a payment instance.
func NewPayment(account string, source *PaymentSource, amount dcrutil.Amount,
	height uint32, estMaturity uint32) *Payment {
	return &Payment{
		Account:           account,
		Amount:            amount,
		Height:            height,
		Source:            source,
		EstimatedMaturity: estMaturity,
		CreatedOn:         time.Now().UnixNano(),
	}
}

// paymentID generates a unique id using the provided payment details.
func paymentID(height uint32, createdOnNano int64, account string) []byte {
	buf := bytes.Buffer{}
	buf.WriteString(hex.EncodeToString(heightToBigEndianBytes(height)))
	buf.WriteString(hex.EncodeToString(nanoToBigEndianBytes(createdOnNano)))
	buf.WriteString(account)
	return buf.Bytes()
}

// fetchPaymentBucket is a helper function for getting the payment bucket.
func fetchPaymentBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, errors.MakeError(errors.ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(paymentBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentBkt))
		return nil, errors.MakeError(errors.ErrBucketNotFound, desc, nil)
	}
	return bkt, nil
}

// fetchPaymentArchiveBucket is a helper function for getting the
// payment archive bucket.
func fetchPaymentArchiveBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, errors.MakeError(errors.ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(paymentArchiveBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentArchiveBkt))
		return nil, errors.MakeError(errors.ErrBucketNotFound, desc, nil)
	}
	return bkt, nil
}

// FetchPayment fetches the payment referenced by the provided id.
func FetchPayment(db *bolt.DB, id []byte) (*Payment, error) {
	var payment Payment
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}
		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("no payment found for id %s", string(id))
			return errors.MakeError(errors.ErrValueNotFound, desc, nil)
		}
		err = json.Unmarshal(v, &payment)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &payment, err
}

// Create persists a payment to the database.
func (pmt *Payment) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}
		b, err := json.Marshal(pmt)
		if err != nil {
			return err
		}
		id := paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
		return bkt.Put(id, b)
	})
	return err
}

// Update persists the updated payment to the database.
func (pmt *Payment) Update(db *bolt.DB) error {
	return pmt.Create(db)
}

// Delete purges the referenced payment from the database. Note that
// archived payments cannot be deleted.
func (pmt *Payment) Delete(db *bolt.DB) error {
	id := paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
	return deleteEntry(db, paymentBkt, id)
}

// Archive removes the associated payment from active payments and archives it.
func (pmt *Payment) Archive(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		pbkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}
		abkt, err := fetchPaymentArchiveBucket(tx)
		if err != nil {
			return err
		}

		// Remove the active payment record.
		id := paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
		err = pbkt.Delete(id)
		if err != nil {
			return err
		}

		// Archive the payment.
		pmt.CreatedOn = time.Now().UnixNano()
		pmtB, err := json.Marshal(pmt)
		if err != nil {
			return err
		}

		id = paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
		return abkt.Put(id, pmtB)
	})
}
