// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package errors

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
		{ValueNotFound, "ValueNotFound"},
		{BucketNotFound, "BucketNotFound"},
		{CreateStorage, "CreateStorage"},
		{DBOpen, "DBOpen"},
		{DBClose, "DBClose"},
		{DBUpgrade, "DBUpgrade"},
		{PersistEntry, "PersistEntry"},
		{DeleteEntry, "DeleteEntry"},
		{FetchEntry, "FetchEntry"},

		{Backup, "Backup"},
		{Parse, "Parse"},
		{Decode, "Decode"},
		{ValueFound, "ValueFound"},

		{GetWork, "GetWork"},
		{GetBlock, "GetBlock"},
		{Disconnected, "Disconnected"},
		{Listener, "Listener"},
		{HeaderInvalid, "HeaderInvalid"},
		{MinerUnknown, "MinerUnknown"},
		{DivideByZero, "DivideByZero"},
		{HexLength, "HexLength"},
		{TxConf, "TxConf"},
		{BlockConf, "BlockConf"},
		{ClaimShare, "ClaimShare"},
		{LimitExceeded, "LimitExceeded"},
		{Difficulty, "Difficulty"},
		{WorkRejected, "WorkRejected"},
		{PaymentSource, "PaymentSource"},
		{ShareRatio, "ShareRatio"},
		{CreateHash, "CreateHash"},
		{Coinbase, "Coinbase"},
		{CreateTx, "CreateTx"},
		{SignTx, "SignTx"},
		{PublishTx, "PublishTx"},
		{TxOut, "TxOut"},
		{TxIn, "TxIn"},
		{ContextCancelled, "ContextCancelled"},
		{CreateAmount, "CreateAmount"},
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

// TestErrorKindIsAs ensures both ErrorKind and Error can be identified as being
// a specific error kind via Is and unwrapped via As.
func TestErrorKindIsAs(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		target    error
		wantMatch bool
		wantAs    ErrorKind
	}{{
		name:      "ValueNotFound == ValueNotFound",
		err:       ValueNotFound,
		target:    ValueNotFound,
		wantMatch: true,
		wantAs:    ValueNotFound,
	}, {
		name:      "Error.ValueNotFound == ValueNotFound",
		err:       PoolError(ValueNotFound, ""),
		target:    ValueNotFound,
		wantMatch: true,
		wantAs:    ValueNotFound,
	}, {
		name:      "Error.ValueNotFound == Error.ValueNotFound",
		err:       PoolError(ValueNotFound, ""),
		target:    PoolError(ValueNotFound, ""),
		wantMatch: true,
		wantAs:    ValueNotFound,
	}, {
		name:      "ValueNotFound != BucketNotFound",
		err:       ValueNotFound,
		target:    BucketNotFound,
		wantMatch: false,
		wantAs:    ValueNotFound,
	}, {
		name:      "Error.ValueNotFound != BucketNotFound",
		err:       PoolError(ValueNotFound, ""),
		target:    BucketNotFound,
		wantMatch: false,
		wantAs:    ValueNotFound,
	}, {
		name:      "ValueNotFound != Error.BucketNotFound",
		err:       ValueNotFound,
		target:    PoolError(BucketNotFound, ""),
		wantMatch: false,
		wantAs:    ValueNotFound,
	}, {
		name:      "Error.ValueNotFound != Error.BucketNotFound",
		err:       PoolError(ValueNotFound, ""),
		target:    PoolError(BucketNotFound, ""),
		wantMatch: false,
		wantAs:    ValueNotFound,
	}, {
		name:      "Error.Parse != io.EOF",
		err:       PoolError(Parse, ""),
		target:    io.EOF,
		wantMatch: false,
		wantAs:    Parse,
	}}

	for _, test := range tests {
		// Ensure the error matches or not depending on the expected result.
		result := errors.Is(test.err, test.target)
		if result != test.wantMatch {
			t.Errorf("%s: incorrect error identification -- got %v, want %v",
				test.name, result, test.wantMatch)
			continue
		}

		// Ensure the underlying error kind can be unwrapped and is the
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
