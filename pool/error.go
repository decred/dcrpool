// Copyright (c) 2019-2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

// ErrorKind identifies a kind of error.  It has full support for errors.Is and
// errors.As, so the caller can directly check against an error kind when
// determining the reason for an error.
type ErrorKind string

// These constants are used to identify a specific error.
const (
	// ------------------------------------------
	// Errors related to database operations.
	// ------------------------------------------

	// ErrValueNotFound indicates no value found.
	ErrValueNotFound = ErrorKind("ErrValueNotFound")

	// ErrBucketNotFound indicates no bucket found.
	ErrBucketNotFound = ErrorKind("ErrBucketNotFound")

	// ErrBucketCreate indicates bucket creation failed.
	ErrBucketCreate = ErrorKind("ErrBucketCreate")

	// ErrDBOpen indicates a database open error.
	ErrDBOpen = ErrorKind("ErrDBOpen")

	// ErrDBUpgrade indicates a database upgrade error.
	ErrDBUpgrade = ErrorKind("ErrDBUpgrade")

	// ErrPersistEntry indicates a database persistence error.
	ErrPersistEntry = ErrorKind("ErrPersistEntry")

	// ErrDeleteEntry indicates a database entry delete error.
	ErrDeleteEntry = ErrorKind("ErrDeleteEntry")

	// ErrBackup indicates database backup error.
	ErrBackup = ErrorKind("ErrBackup")

	// ErrParse indicates a parsing error.
	ErrParse = ErrorKind("ErrParse")

	// ErrDecode indicates a decoding error.
	ErrDecode = ErrorKind("ErrDecode")

	// ErrValueFound indicates a an unexpected value found.
	ErrValueFound = ErrorKind("ErrValueFound")

	// ------------------------------------------
	// Errors related to pool operations.
	// ------------------------------------------

	// ErrGetWork indicates current work could not be fetched.
	ErrGetWork = ErrorKind("ErrGetWork")

	// ErrGetBlock indicates a block could not be fetched.
	ErrGetBlock = ErrorKind("ErrGetBlock")

	// ErrDisconnected indicates a disconnected resource.
	ErrDisconnected = ErrorKind("ErrDisconnected")

	// ErrListener indicates a miner listener error.
	ErrListener = ErrorKind("ErrListener")

	// ErrHeaderInvalid indicates header creation failed.
	ErrHeaderInvalid = ErrorKind("ErrHeaderInvalid")

	// ErrMinerUnknown indicates an unknown miner.
	ErrMinerUnknown = ErrorKind("ErrMinerUnknown")

	// ErrDivideByZero indicates a division by zero error.
	ErrDivideByZero = ErrorKind("ErrDivideByZero")

	// ErrHexLength indicates an invalid hex length.
	ErrHexLength = ErrorKind("ErrHexLength")

	// ErrTxConf indicates a transaction confirmation error.
	ErrTxConf = ErrorKind("ErrTxConf")

	// ErrBlockConf indicates a block confirmation error.
	ErrBlockConf = ErrorKind("ErrBlockConf")

	// ErrClaimShare indicates a share claim error.
	ErrClaimShare = ErrorKind("ErrClaimShare")

	// ErrLimitExceeded indicates a rate limit exhaustion error.
	ErrLimitExceeded = ErrorKind("ErrLimitExceeded")

	// ErrDifficulty indicates a difficulty related error.
	ErrDifficulty = ErrorKind("ErrDifficulty")

	// ErrWorkRejected indicates the rejected submitted work.
	ErrWorkRejected = ErrorKind("ErrWorkRejected")

	// ErrPaymentSource indicates a payment source error.
	ErrPaymentSource = ErrorKind("ErrPaymentSource")
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

// poolError creates an Error given a set of arguments. This hould only be
// used when creating error related to the mining pool and its processes.
func poolError(kind ErrorKind, desc string) Error {
	return Error{Err: kind, Description: desc}
}

// dbError creates an Error given a set of arguments. This should only be
// used when creating errors related to the database.
func dbError(kind ErrorKind, desc string) Error {
	return Error{Err: kind, Description: desc}
}

// msgError creates am Error given a set of arguments. This should only be
// used when creating errors related to sending, receiving and processing
// messages.
func msgError(kind ErrorKind, desc string) Error {
	return Error{Err: kind, Description: desc}
}
