// Copyright (c) 2019-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package errors

import "errors"

// ErrorKind identifies a kind of error.  It has full support for errors.Is and
// errors.As, so the caller can directly check against an error kind when
// determining the reason for an error.
type ErrorKind string

// These constants are used to identify a specific error.
const (
	// ------------------------------------------
	// Errors related to database operations.
	// ------------------------------------------

	// ValueNotFound indicates no value found.
	ValueNotFound = ErrorKind("ErrValueNotFound")

	// BucketNotFound indicates no bucket found.
	BucketNotFound = ErrorKind("ErrBucketNotFound")

	// BucketCreate indicates bucket creation failed.
	BucketCreate = ErrorKind("ErrBucketCreate")

	// DBOpen indicates a database open error.
	DBOpen = ErrorKind("ErrDBOpen")

	// DBUpgrade indicates a database upgrade error.
	DBUpgrade = ErrorKind("ErrDBUpgrade")

	// PersistEntry indicates a database persistence error.
	PersistEntry = ErrorKind("ErrPersistEntry")

	// DeleteEntry indicates a database entry delete error.
	DeleteEntry = ErrorKind("ErrDeleteEntry")

	// FetchEntry indicates a database entry fetching error.
	FetchEntry = ErrorKind("ErrFetchEntry")

	// Backup indicates database backup error.
	Backup = ErrorKind("ErrBackup")

	// Parse indicates a parsing error.
	Parse = ErrorKind("ErrParse")

	// Decode indicates a decoding error.
	Decode = ErrorKind("ErrDecode")

	// ValueFound indicates a an unexpected value found.
	ValueFound = ErrorKind("ErrValueFound")

	// ------------------------------------------
	// Errors related to pool operations.
	// ------------------------------------------

	// GetWork indicates current work could not be fetched.
	GetWork = ErrorKind("ErrGetWork")

	// GetBlock indicates a block could not be fetched.
	GetBlock = ErrorKind("ErrGetBlock")

	// Disconnected indicates a disconnected resource.
	Disconnected = ErrorKind("ErrDisconnected")

	// Listener indicates a miner listener error.
	Listener = ErrorKind("ErrListener")

	// HeaderInvalid indicates header creation failed.
	HeaderInvalid = ErrorKind("ErrHeaderInvalid")

	// MinerUnknown indicates an unknown miner.
	MinerUnknown = ErrorKind("ErrMinerUnknown")

	// DivideByZero indicates a division by zero error.
	DivideByZero = ErrorKind("ErrDivideByZero")

	// HexLength indicates an invalid hex length.
	HexLength = ErrorKind("ErrHexLength")

	// TxConf indicates a transaction confirmation error.
	TxConf = ErrorKind("ErrTxConf")

	// BlockConf indicates a block confirmation error.
	BlockConf = ErrorKind("ErrBlockConf")

	// ClaimShare indicates a share claim error.
	ClaimShare = ErrorKind("ErrClaimShare")

	// LimitExceeded indicates a rate limit exhaustion error.
	LimitExceeded = ErrorKind("ErrLimitExceeded")

	// Difficulty indicates a difficulty related error.
	Difficulty = ErrorKind("ErrDifficulty")

	// WorkRejected indicates the rejected submitted work.
	WorkRejected = ErrorKind("ErrWorkRejected")

	// PaymentSource indicates a payment source error.
	PaymentSource = ErrorKind("ErrPaymentSource")

	// ShareRatio indicates a share ratio error.
	ShareRatio = ErrorKind("ErrShareRatio")

	// CreateHash indicates a hash creation error.
	CreateHash = ErrorKind("ErrCreateHash")

	// Coinbase indicates a coinbase related error.
	Coinbase = ErrorKind("ErrCoinbase")

	// CreateTx indicates a transaction creation error.
	CreateTx = ErrorKind("ErrCreateTx")

	// SignTx indicates a transaction signing error.
	SignTx = ErrorKind("ErrSignTx")

	// PublishTx indicates a transaction pubishing error.
	PublishTx = ErrorKind("ErrPublishTx")

	// TxOut indicates a transaction output related error.
	TxOut = ErrorKind("ErrTxOut")

	// TxIn indicates a transaction input related error.
	TxIn = ErrorKind("ErrTxIn")

	// ContextCancelled indicates a context cancellation related error.
	ContextCancelled = ErrorKind("ErrContextCancelled")

	// CreateAmount indicates an amount creation error.
	CreateAmount = ErrorKind("ErrCreateAmount")
)

// Error satisfies the error interface and prints human-readable errors.
func (e ErrorKind) Error() string {
	return string(e)
}

// Error identifies an error. It has full support for errors.Is and
// errors.As, so the caller can ascertain the specific reason for
// the error by checking the underlying error.
type Error struct {
	Description string
	Err         error
}

// New creates a simple error from a string.  New is identical to "errors".New
// from the standard library.
func New(text string) error {
	return errors.New(text)
}

// Error satisfies the error interface and prints human-readable errors.
func (e Error) Error() string {
	return e.Description
}

// Unwrap returns the underlying wrapped error.
func (e Error) Unwrap() error {
	return e.Err
}

// Is returns whether err equals or wraps target.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As attempts to assign the error pointed to by target with the first error in
// err's error chain with a compatible type.  Returns true if target is
// assigned.
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}

// PoolError creates an Error given a set of arguments. This should only be
// used when creating error related to the mining pool and its processes.
func PoolError(kind ErrorKind, desc string) Error {
	return Error{Err: kind, Description: desc}
}

// DBError creates an Error given a set of arguments. This should only be
// used when creating errors related to the database.
func DBError(kind ErrorKind, desc string) Error {
	return Error{Err: kind, Description: desc}
}

// MsgError creates an Error given a set of arguments. This should only be
// used when creating errors related to sending, receiving and processing
// messages.
func MsgError(kind ErrorKind, desc string) Error {
	return Error{Err: kind, Description: desc}
}
