// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package errors

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
	ValueNotFound = ErrorKind("ValueNotFound")

	// StorageNotFound indicates a missing storage error.
	StorageNotFound = ErrorKind("StorageNotFound")

	// CreateStorage indicates a storage creation error.
	CreateStorage = ErrorKind("CreateStorage")

	// DBOpen indicates a database open error.
	DBOpen = ErrorKind("DBOpen")

	// DBOpen indicates a database close error.
	DBClose = ErrorKind("DBClose")

	// DBUpgrade indicates a database upgrade error.
	DBUpgrade = ErrorKind("DBUpgrade")

	// PersistEntry indicates a database persistence error.
	PersistEntry = ErrorKind("PersistEntry")

	// DeleteEntry indicates a database entry delete error.
	DeleteEntry = ErrorKind("DeleteEntry")

	// FetchEntry indicates a database entry fetching error.
	FetchEntry = ErrorKind("FetchEntry")

	// Backup indicates database backup error.
	Backup = ErrorKind("Backup")

	// Parse indicates a parsing error.
	Parse = ErrorKind("Parse")

	// Decode indicates a decoding error.
	Decode = ErrorKind("Decode")

	// ValueFound indicates an unexpected value found.
	ValueFound = ErrorKind("ValueFound")

	// Unsupported indicates unsupported functionality.
	Unsupported = ErrorKind("Unsupported")

	// ------------------------------------------
	// Errors related to pool operations.
	// ------------------------------------------

	// GetWork indicates current work could not be fetched.
	GetWork = ErrorKind("GetWork")

	// GetBlock indicates a block could not be fetched.
	GetBlock = ErrorKind("GetBlock")

	// Disconnected indicates a disconnected resource.
	Disconnected = ErrorKind("Disconnected")

	// Listener indicates a miner listener error.
	Listener = ErrorKind("Listener")

	// HeaderInvalid indicates header creation failed.
	HeaderInvalid = ErrorKind("HeaderInvalid")

	// MinerUnknown indicates an unknown miner.
	MinerUnknown = ErrorKind("MinerUnknown")

	// DivideByZero indicates a division by zero error.
	DivideByZero = ErrorKind("DivideByZero")

	// HexLength indicates an invalid hex length.
	HexLength = ErrorKind("HexLength")

	// TxConf indicates a transaction confirmation error.
	TxConf = ErrorKind("TxConf")

	// BlockConf indicates a block confirmation error.
	BlockConf = ErrorKind("BlockConf")

	// BlockNotFound indicates a block not found error.
	BlockNotFound = ErrorKind("BlockNotFound")

	// ClaimShare indicates a share claim error.
	ClaimShare = ErrorKind("ClaimShare")

	// LimitExceeded indicates a rate limit exhaustion error.
	LimitExceeded = ErrorKind("LimitExceeded")

	// Difficulty indicates a low difficulty error.
	LowDifficulty = ErrorKind("LowDifficulty")

	// PoolDifficulty indicates a difficulty is lower than the
	// pool difficulty.
	PoolDifficulty = ErrorKind("PoolDifficulty")

	// PoolDifficulty indicates a difficulty is lower than the
	// network difficulty.
	NetworkDifficulty = ErrorKind("NetworkDifficulty")

	// WorkRejected indicates a rejected submitted work.
	WorkRejected = ErrorKind("WorkRejected")

	// PaymentSource indicates a payment source error.
	PaymentSource = ErrorKind("PaymentSource")

	// ShareRatio indicates a share ratio error.
	ShareRatio = ErrorKind("ShareRatio")

	// CreateHash indicates a hash creation error.
	CreateHash = ErrorKind("CreateHash")

	// Coinbase indicates a coinbase related error.
	Coinbase = ErrorKind("Coinbase")

	// CreateTx indicates a transaction creation error.
	CreateTx = ErrorKind("CreateTx")

	// SignTx indicates a transaction signing error.
	SignTx = ErrorKind("SignTx")

	// PublishTx indicates a transaction pubishing error.
	PublishTx = ErrorKind("PublishTx")

	// FetchTx indicates a fetch transaction error.
	FetchTx = ErrorKind("FetchTx")

	// TxOut indicates a transaction output related error.
	TxOut = ErrorKind("TxOut")

	// TxIn indicates a transaction input related error.
	TxIn = ErrorKind("TxIn")

	// ContextCancelled indicates a context cancellation related error.
	ContextCancelled = ErrorKind("ContextCancelled")

	// CreateAmount indicates an amount creation error.
	CreateAmount = ErrorKind("CreateAmount")

	// Rescan indicates an wallet rescan error.
	Rescan = ErrorKind("Rescan")
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

// Error satisfies the error interface and prints human-readable errors.
func (e Error) Error() string {
	return e.Description
}

// Unwrap returns the underlying wrapped error.
func (e Error) Unwrap() error {
	return e.Err
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
