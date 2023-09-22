// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"time"

	"github.com/decred/dcrd/dcrutil/v4"
)

// PaymentSource represents the payment's source of funds.
type PaymentSource struct {
	BlockHash string `json:"blockhash"`
	Coinbase  string `json:"coinbase"`
}

// Payment represents value paid to a pool account or collected fees.
type Payment struct {
	UUID              string         `json:"uuid"`
	Account           string         `json:"account"`
	EstimatedMaturity uint32         `json:"estimatedmaturity"`
	Height            uint32         `json:"height"`
	Amount            dcrutil.Amount `json:"amount"`
	CreatedOn         int64          `json:"createdon"`
	PaidOnHeight      uint32         `json:"paidonheight"`
	TransactionID     string         `json:"transactionid"`

	Source *PaymentSource `json:"source"`
}

// paymentID generates a unique id using the provided payment details and a
// random uint64.
func paymentID(height uint32, createdOnNano int64, account string, randVal uint64) string {
	var randValEncoded [8]byte
	binary.BigEndian.PutUint64(randValEncoded[:], randVal)
	var buf bytes.Buffer
	_, _ = buf.WriteString(hex.EncodeToString(heightToBigEndianBytes(height)))
	_, _ = buf.WriteString(hex.EncodeToString(nanoToBigEndianBytes(createdOnNano)))
	_, _ = buf.WriteString(account)
	_, _ = buf.WriteString(hex.EncodeToString(randValEncoded[:]))
	return buf.String()
}

// NewPayment creates a payment instance.
func NewPayment(account string, source *PaymentSource, amount dcrutil.Amount,
	height uint32, estMaturity uint32) *Payment {
	now := time.Now().UnixNano()
	return &Payment{
		UUID:              paymentID(height, now, account, uuidPRNG.Uint64()),
		Account:           account,
		Amount:            amount,
		Height:            height,
		Source:            source,
		EstimatedMaturity: estMaturity,
		CreatedOn:         now,
	}
}
