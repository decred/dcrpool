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
func paymentID(height uint32, createdOnNano int64, account string) string {
	var buf bytes.Buffer
	_, _ = buf.WriteString(hex.EncodeToString(heightToBigEndianBytes(height)))
	_, _ = buf.WriteString(hex.EncodeToString(nanoToBigEndianBytes(createdOnNano)))
	_, _ = buf.WriteString(account)
	return buf.String()
}

// FetchPayment fetches the payment referenced by the provided id.
func FetchPayment(db *bolt.DB, id string) (*Payment, error) {
	const funcName = "FetchPayment"
	var payment Payment
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no payment found for id %s", funcName, id)
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &payment, err
}

// Persist saves a payment to the database.
func (pmt *Payment) Persist(db *bolt.DB) error {
	const funcName = "Payment.Persist"
	return db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		b, err := json.Marshal(pmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		id := paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
		err = bkt.Put([]byte(id), b)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment bytes: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// Update persists the updated payment to the database.
func (pmt *Payment) Update(db *bolt.DB) error {
	return pmt.Persist(db)
}

// Delete purges the referenced payment from the database. Note that
// archived payments cannot be deleted.
func (pmt *Payment) Delete(db *bolt.DB) error {
	id := paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
	return deleteEntry(db, paymentBkt, id)
}

// Archive removes the associated payment from active payments and archives it.
func (pmt *Payment) Archive(db *bolt.DB) error {
	const funcName = "Payment.Archive"
	return db.Update(func(tx *bolt.Tx) error {
		pbkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		abkt, err := fetchBucket(tx, paymentArchiveBkt)
		if err != nil {
			return err
		}

		// Remove the active payment record.
		id := paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
		err = pbkt.Delete([]byte(id))
		if err != nil {
			return err
		}

		// Archive the payment.
		pmt.CreatedOn = time.Now().UnixNano()
		pmtB, err := json.Marshal(pmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		id = paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
		err = abkt.Put([]byte(id), pmtB)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to archive payment entry: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// fetchPaymentsAtHeight returns all payments sourcing from orphaned blocks at
// the provided height.
func fetchPaymentsAtHeight(db *bolt.DB, height uint32) ([]*Payment, error) {
	toReturn := make([]*Payment, 0)
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
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
				// If a payment is spendable but does not get processed it
				// becomes eligible for pruning.
				spendableHeight := payment.EstimatedMaturity + 1
				if height > spendableHeight {
					toReturn = append(toReturn, &payment)
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return toReturn, nil
}

// fetchPendingPayments fetches all unpaid payments.
func fetchPendingPayments(db *bolt.DB) ([]*Payment, error) {
	funcName := "fetchPendingPayments"
	payments := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal "+
					"payment: %v", funcName, err)
				return dbError(ErrParse, desc)
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

// pendingPaymentsForBlockHash returns the number of pending payments with the
// provided block hash as their source.
func pendingPaymentsForBlockHash(db *bolt.DB, blockHash string) (uint32, error) {
	funcName := "pendingPaymentsForBlockHash"
	var count uint32
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
					funcName, err)
				return dbError(ErrParse, desc)
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
func archivedPayments(db *bolt.DB) ([]*Payment, error) {
	funcName := "archivedPayments"
	pmts := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
		abkt, err := fetchBucket(tx, paymentArchiveBkt)
		if err != nil {
			return err
		}

		c := abkt.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
					funcName, err)
				return dbError(ErrParse, desc)
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
func maturePendingPayments(db *bolt.DB, height uint32) (map[string][]*Payment, error) {
	funcName := "maturePendingPayments"
	payments := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
					funcName, err)
				return dbError(ErrParse, desc)
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
