// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestStratumErrorMarshalUnmarshal(t *testing.T) {
	stratumSet := map[string]struct {
		sErr *StratumError
		sB   []byte
	}{
		"valid stratum error": {
			sErr: NewStratumError(DuplicateShare,
				fmt.Errorf("share already submitted")),
			sB: []byte(`[22,"Duplicate share: share already submitted",null]`),
		},
		"no error message": {
			sErr: NewStratumError(LowDifficultyShare, nil),
			sB:   []byte(`[23,"Low difficulty share",null]`),
		},
		"unknown error code": {
			sErr: NewStratumError(50, nil),
			sB:   []byte(`[50,"Other/Unknown",null]`),
		},
	}

	for _, entry := range stratumSet {
		sErrB, err := entry.sErr.MarshalJSON()
		if err != nil {
			t.Fatalf("unable to marshal stratum error: %s", err)
		}

		// Ensure the error unmarshals to its expected bytes.
		if !bytes.Equal(sErrB, entry.sB) {
			t.Fatalf("expected '%s' for stratum error, got '%s'",
				string(entry.sB), string(sErrB))
		}

		// Ensure the error bytes marshals to its expected struct.
		var uErr StratumError
		err = json.Unmarshal(sErrB, &uErr)
		if err != nil {
			t.Fatalf("unable to unmarshal stratum error bytes: %s", err)
		}

		if uErr.Code != entry.sErr.Code {
			t.Fatalf("expected error code %d, got %d",
				uErr.Code, entry.sErr.Code)
		}

		if uErr.Message != entry.sErr.Message {
			t.Fatalf("expected error message %s, got %s",
				uErr.Message, entry.sErr.Message)
		}

		if uErr.Traceback != entry.sErr.Traceback {
			t.Fatalf("error traceback mismatch")
		}
	}
}
