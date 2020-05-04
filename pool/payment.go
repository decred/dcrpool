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

	"github.com/decred/dcrd/dcrutil/v2"
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
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(paymentBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	return bkt, nil
}

// fetchPaymentArchiveBucket is a helper function for getting the
// payment archive bucket.
func fetchPaymentArchiveBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(paymentArchiveBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentArchiveBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	return bkt, nil
}

// GetPayment fetches the payment referenced by the provided id.
func GetPayment(db *bolt.DB, id []byte) (*Payment, error) {
	var payment Payment
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}
		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("no payment found for id %s", string(id))
			return MakeError(ErrValueNotFound, desc, nil)
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

// PaymentBundle is a convenience type for grouping payments for an account.
type PaymentBundle struct {
	Account  string
	Payments []*Payment
}

// NewPaymentBundle initializes a payment bundle instance.
func newPaymentBundle(account string) *PaymentBundle {
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
func (bundle *PaymentBundle) UpdateAsPaid(db *bolt.DB, height uint32, txid string) {
	for idx := 0; idx < len(bundle.Payments); idx++ {
		bundle.Payments[idx].TransactionID = txid
		bundle.Payments[idx].PaidOnHeight = height
	}
}

// ArchivePayments removes all payments included in the payment bundle from the
// payment bucket and archives them.
func (bundle *PaymentBundle) ArchivePayments(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt, err := fetchPaymentBucket(tx)
		if err != nil {
			return err
		}
		abkt, err := fetchPaymentArchiveBucket(tx)
		if err != nil {
			return err
		}
		for _, pmt := range bundle.Payments {
			id := paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
			err := pbkt.Delete(id)
			if err != nil {
				return err
			}
			pmt.CreatedOn = time.Now().UnixNano()
			pmtBytes, err := json.Marshal(pmt)
			if err != nil {
				return err
			}
			id = paymentID(pmt.Height, pmt.CreatedOn, pmt.Account)
			err = abkt.Put(id, pmtBytes)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// generatePaymentBundles creates batched payments from the provided set of
// payments. Multiple payments for the same account will be bundled together.
func generatePaymentBundles(payments []*Payment) []*PaymentBundle {
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
			bdl := newPaymentBundle(payment.Account)
			bdl.Payments = append(bdl.Payments, payment)
			bundles = append(bundles, bdl)
		}
	}
	return bundles
}

// filterPayments iterates the payments bucket, the result set is generated
// based on the provided filter.
func filterPayments(db *bolt.DB, filter func(payment *Payment) bool) ([]*Payment, error) {
	payments := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
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
			if filter(&payment) {
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

// fetchPendingPayments fetches all unpaid payments.
func fetchPendingPayments(db *bolt.DB) ([]*Payment, error) {
	filter := func(payment *Payment) bool {
		return payment.PaidOnHeight == 0
	}
	payments, err := filterPayments(db, filter)
	if err != nil {
		return nil, err
	}
	return payments, nil
}

// fetchMaturePendingPayments fetches all payments past their estimated
// maturities which have not been paid yet.
func fetchMaturePendingPayments(db *bolt.DB, height uint32) ([]*Payment, error) {
	filter := func(payment *Payment) bool {
		return payment.PaidOnHeight == 0 &&
			payment.EstimatedMaturity <= height
	}
	payments, err := filterPayments(db, filter)
	if err != nil {
		return nil, err
	}
	return payments, nil
}

// fetchPendingPaymentsAtHeight fetches all pending payments at the provided
// height.
func fetchPendingPaymentsAtHeight(db *bolt.DB, height uint32) ([]*Payment, error) {
	filter := func(payment *Payment) bool {
		return payment.PaidOnHeight == 0 && payment.Height == height
	}
	payments, err := filterPayments(db, filter)
	if err != nil {
		return nil, err
	}
	return payments, nil
}

// generatePaymentDetails generates kv pair of addresses and payment amounts
// from the provided eligible payments.
func generatePaymentDetails(db *bolt.DB, poolFeeAddr dcrutil.Address,
	eligiblePmts []*PaymentBundle) (map[string]dcrutil.Amount, *dcrutil.Amount, error) {
	var targetAmt dcrutil.Amount
	pmts := make(map[string]dcrutil.Amount)
	for _, p := range eligiblePmts {
		if p.Account == poolFeesK {
			bundleAmt := p.Total()
			pmts[poolFeeAddr.String()] = bundleAmt
			targetAmt += bundleAmt
			continue
		}
		acc, err := FetchAccount(db, []byte(p.Account))
		if err != nil {
			return nil, nil, err
		}
		bundleAmt := p.Total()
		pmts[acc.Address] = bundleAmt
		targetAmt += bundleAmt
	}
	return pmts, &targetAmt, nil
}

// fetchArchivedPayments fetches all archived payments. List is ordered, most
// recent comes first.
func fetchArchivedPayments(db *bolt.DB) ([]*Payment, error) {
	pmts := make([]*Payment, 0)
	err := db.View(func(tx *bolt.Tx) error {
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

// FetchPendingPayments fetches all unpaid payments.
func (h *Hub) FetchPendingPayments() ([]*Payment, error) {
	return fetchPendingPayments(h.db)
}

// FetchArchivedPayments fetches all paid payments.
func (h *Hub) FetchArchivedPayments() ([]*Payment, error) {
	return fetchArchivedPayments(h.db)
}
