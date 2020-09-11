// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package pool

import (
	"errors"
	"io"
	"testing"
)

// TestErrorKindStringer tests the stringized output for the ErrorKind type.
func TestErrorKindStringer(t *testing.T) {
	tests := []struct {
		in   ErrorKind
		want string
	}{
		{ErrValueNotFound, "ErrValueNotFound"},
		{ErrBucketNotFound, "ErrBucketNotFound"},
		{ErrBucketCreate, "ErrBucketCreate"},
		{ErrDBOpen, "ErrDBOpen"},
		{ErrDBUpgrade, "ErrDBUpgrade"},
		{ErrPersistEntry, "ErrPersistEntry"},
		{ErrDeleteEntry, "ErrDeleteEntry"},
		{ErrNotSupported, "ErrNotSupported"},
		{ErrBackup, "ErrBackup"},
		{ErrParse, "ErrParse"},
		{ErrDecode, "ErrDecode"},
		{ErrID, "ErrID"},
		{ErrValueFound, "ErrValueFound"},

		{ErrGetWork, "ErrGetWork"},
		{ErrGetBlock, "ErrGetBlock"},
		{ErrWorkExists, "ErrWorkExists"},
		{ErrDisconnected, "ErrDisconnected"},
		{ErrListener, "ErrListener"},
		{ErrHeaderInvalid, "ErrHeaderInvalid"},
		{ErrMinerUnknown, "ErrMinerUnknown"},
		{ErrDivideByZero, "ErrDivideByZero"},
		{ErrHexLength, "ErrHexLength"},
		{ErrTxConf, "ErrTxConf"},
		{ErrBlockConf, "ErrBlockConf"},
		{ErrClaimShare, "ErrClaimShare"},
		{ErrLimitExceeded, "ErrLimitExceeded"},
		{ErrDifficulty, "ErrDifficulty"},
		{ErrWorkRejected, "ErrWorkRejected"},
		{ErrAccountExists, "ErrAccountExists"},
		{ErrPaymentSource, "ErrPaymentSource"},
	}

	for i, test := range tests {
		result := test.in.Error()
		if result != test.want {
			t.Errorf("%d: got: %s want: %s", i, result, test.want)
			continue
		}
	}
}

// TestError tests the error output for the Error type.
func TestError(t *testing.T) {
	tests := []struct {
		in   Error
		want string
	}{
		{Error{Description: "value not found"},
			"value not found",
		},
		{Error{Description: "human-readable error"},
			"human-readable error",
		},
	}

	for i, test := range tests {
		result := test.in.Error()
		if result != test.want {
			t.Errorf("%d: got: %s want: %s", i, result, test.want)
			continue
		}
	}
}

// TestErrorKindIsAs ensures both ErrorKind and Error can be identified as
// being a specific error kind via errors.Is and unwrapped
// via errors.As.
func TestErrorKindIsAs(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		target    error
		wantMatch bool
		wantAs    ErrorKind
	}{{
		name:      "ErrValueNotFound == ErrValueNotFound",
		err:       ErrValueNotFound,
		target:    ErrValueNotFound,
		wantMatch: true,
		wantAs:    ErrValueNotFound,
	}, {
		name:      "Error.ErrValueNotFound == ErrValueNotFound",
		err:       poolError(ErrValueNotFound, ""),
		target:    ErrValueNotFound,
		wantMatch: true,
		wantAs:    ErrValueNotFound,
	}, {
		name:      "Error.ErrValueNotFound == Error.ErrValueNotFound",
		err:       poolError(ErrValueNotFound, ""),
		target:    poolError(ErrValueNotFound, ""),
		wantMatch: true,
		wantAs:    ErrValueNotFound,
	}, {
		name:      "ErrValueNotFound != ErrBucketNotFound",
		err:       ErrValueNotFound,
		target:    ErrBucketNotFound,
		wantMatch: false,
		wantAs:    ErrValueNotFound,
	}, {
		name:      "Error.ErrValueNotFound != ErrBucketNotFound",
		err:       poolError(ErrValueNotFound, ""),
		target:    ErrBucketNotFound,
		wantMatch: false,
		wantAs:    ErrValueNotFound,
	}, {
		name:      "ErrValueNotFound != Error.ErrBucketNotFound",
		err:       ErrValueNotFound,
		target:    poolError(ErrBucketNotFound, ""),
		wantMatch: false,
		wantAs:    ErrValueNotFound,
	}, {
		name:      "Error.ErrValueNotFound != Error.ErrBucketNotFound",
		err:       poolError(ErrValueNotFound, ""),
		target:    poolError(ErrBucketNotFound, ""),
		wantMatch: false,
		wantAs:    ErrValueNotFound,
	}, {
		name:      "Error.ErrParse != io.EOF",
		err:       poolError(ErrParse, ""),
		target:    io.EOF,
		wantMatch: false,
		wantAs:    ErrParse,
	}}

	for _, test := range tests {
		// Ensure the error matches or not depending on the expected result.
		result := errors.Is(test.err, test.target)
		if result != test.wantMatch {
			t.Errorf("%s: incorrect error identification -- got %v, want %v",
				test.name, result, test.wantMatch)
			continue
		}

		// Ensure the underlying error kind can be unwrapped is and is the
		// expected kind.
		var kind ErrorKind
		if !errors.As(test.err, &kind) {
			t.Errorf("%s: unable to unwrap to error kind", test.name)
			continue
		}
		if kind != test.wantAs {
			t.Errorf("%s: unexpected unwrapped error kind -- got %v, want %v",
				test.name, kind, test.wantAs)
			continue
		}
	}
}
