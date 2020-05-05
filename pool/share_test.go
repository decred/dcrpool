// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

// persistShare creates a persisted share with the provided account and share
// weight.
func persistShare(db *bolt.DB, account string, weight *big.Rat,
	createdOnNano int64) error {
	share := &Share{
		UUID:    string(shareID(account, createdOnNano)),
		Account: account,
		Weight:  weight,
	}

	err := share.Create(db)
	if err != nil {
		return fmt.Errorf("unable to persist share: %v", err)
	}

	return nil
}

func testShares(t *testing.T, db *bolt.DB) {
	shareACreatedOn := time.Now().Add(-(time.Second * 10)).UnixNano()
	shareBCreatedOn := time.Now().Add(-(time.Second * 20)).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)
	err := persistShare(db, xID, weight, shareACreatedOn) // Share A
	if err != nil {
		t.Fatal(err)
	}

	err = persistShare(db, yID, weight, shareBCreatedOn) // Share B
	if err != nil {
		t.Fatal(err)
	}

	// Fetch share A and B.
	shareA, err := fetchShare(db, shareID(xID, shareACreatedOn))
	if err != nil {
		t.Fatalf("unexpected error fetching share A: %v", err)
	}
	shareB, err := fetchShare(db, shareID(yID, shareBCreatedOn))
	if err != nil {
		t.Fatalf("unexpected error fetching share B: %v", err)
	}

	// Ensure shares cannot be updated.
	shareA.Weight = new(big.Rat).SetFloat64(100.0)
	err = shareA.Update(db)
	if err == nil {
		t.Fatal("expected an unsupported functionality error")
	}

	// Ensure shares cannot be deleted.
	err = shareB.Delete(db)
	if err == nil {
		t.Fatal("expected an unsupported functionality error")
	}

	// Empty the share bucket.
	err = emptyBucket(db, shareBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}
}
