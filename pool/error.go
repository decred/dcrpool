// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import "fmt"

// ErrorCode identifies a kind of error.
type ErrorCode int

// These constants are used to identify a specific error.
const (
	// ErrValueNotFound indicates a key does not map to any value.
	ErrValueNotFound ErrorCode = iota

	// ErrValueNotFound indicates a bucket key does not map to a bucket.
	ErrBucketNotFound

	// ErrBucketCreate indicates bucket creation failed.
	ErrBucketCreate

	// ErrDBOpen indicates database open error.
	ErrDBOpen

	// ErrWorkExists indicates an already existing work.
	ErrWorkExists

	// ErrWorkNotFound indicates non-existent work.
	ErrWorkNotFound

	// ErrWrongInputLength indicates an incorrect input size.
	ErrWrongInputLength

	// ErrDifficultyNotFound indicates a non-existent miner pool difficulty.
	ErrDifficultyNotFound

	// ErrCalcPoolTarget indicates a pool target calculation error.
	ErrCalcPoolTarget

	// ErrParse indicates a message parsing error.
	ErrParse

	// ErrDecode indicates a decode error.
	ErrDecode

	// ErrNotSupported indicates not supported functionality.
	ErrNotSupported

	// ErrDivideByZero indicates a division by zero error.
	ErrDivideByZero

	// ErrDBUpgrade indicates a database upgrade error.
	ErrDBUpgrade

	// ErrOther indicates a miscellenious error.
	ErrOther
)

// Map of ErrorCode values back to their constant names for pretty printing.
var errorCodeStrings = map[ErrorCode]string{
	ErrValueNotFound:      "ErrValueNotFound",
	ErrBucketNotFound:     "ErrBucketNotFound",
	ErrBucketCreate:       "ErrBucketCreate",
	ErrDBOpen:             "ErrDBOpen",
	ErrWorkExists:         "ErrWorkExists",
	ErrWorkNotFound:       "ErrWorkNotfound",
	ErrWrongInputLength:   "ErrWrongInputLength",
	ErrDifficultyNotFound: "ErrDifficultyNotFound",
	ErrCalcPoolTarget:     "ErrCalcPoolTarget",
	ErrParse:              "ErrParse",
	ErrDecode:             "ErrDecode",
	ErrNotSupported:       "ErrNotSupported",
	ErrDivideByZero:       "ErrDivideByZero",
	ErrDBUpgrade:          "ErrDBUpgrade",
	ErrOther:              "ErrOther",
}

// String returns the ErrorCode as a human-readable name.
func (e ErrorCode) String() string {
	if s := errorCodeStrings[e]; s != "" {
		return s
	}
	return fmt.Sprintf("Unknown ErrorCode (%d)", int(e))
}

// Error extends the base error type by adding an error description and an
// error code.
//
// The caller can use type assertions to determine if an error is an Error and
// access the ErrorCode field to ascertain the specific reason for the failure.
type Error struct {
	ErrorCode   ErrorCode
	Description string
	Err         error
}

// Error satisfies the error interface and prints human-readable errors.
func (e Error) Error() string {
	if e.Err != nil {
		return e.Description + ": " + e.Err.Error()
	}
	return e.Description
}

// MakeError creates an Error given a set of arguments.
func MakeError(c ErrorCode, desc string, err error) Error {
	return Error{ErrorCode: c, Description: desc, Err: err}
}

// IsError returns whether err is an Error with a matching error code.
func IsError(err error, code ErrorCode) bool {
	e, ok := err.(Error)
	return ok && e.ErrorCode == code
}
