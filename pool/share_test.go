// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"math/big"
	"testing"
	"time"

	"github.com/decred/dcrpool/errors"
)

// persistShare creates a persisted share with the provided account and share
// weight.
func persistShare(db Database, account string, weight *big.Rat, createdOnNano int64) error {
	share := &Share{
		UUID:      shareID(account, createdOnNano),
		Account:   account,
		Weight:    weight,
		CreatedOn: createdOnNano,
	}
	err := db.PersistShare(share)
	if err != nil {
		return err
	}
	return nil
}

func testShares(t *testing.T) {
	account := "9e5b83c58170e46b2dee1315aa3b00efd96b5839498fda135b8eddb34f6b34ee"
	weight := new(big.Rat).SetFloat64(1.0)

	// Create a valid share.
	share := NewShare(account, weight)
	err := db.PersistShare(share)
	if err != nil {
		t.Fatalf("could not persist share: %v", err)
	}

	// Creating the same share twice should fail.
	err = db.PersistShare(share)
	if !errors.Is(err, errors.ValueFound) {
		t.Fatalf("expected value found error, got %v", err)
	}

	// Fetch share using its id.
	fetchedShare, err := db.fetchShare(share.UUID)
	if err != nil {
		t.Fatalf("unexpected error fetching share A: %v", err)
	}

	// Ensure fetched values match persisted values.
	if fetchedShare.UUID != share.UUID {
		t.Fatalf("expected %v as fetched share id, got %v",
			share.UUID, fetchedShare.UUID)
	}

	if fetchedShare.Account != share.Account {
		t.Fatalf("expected %v as fetched share account, got %v",
			share.Account, fetchedShare.Account)
	}

	if fetchedShare.CreatedOn != share.CreatedOn {
		t.Fatalf("expected %v as fetched share created on, got %v",
			share.CreatedOn, fetchedShare.CreatedOn)
	}

	if fetchedShare.Weight.Cmp(share.Weight) != 0 {
		t.Fatalf("expected %v as fetched share weight, got %v",
			share.Weight, fetchedShare.Weight)
	}

	// Expect error when fetching share which doesnt exist.
	_, err = db.fetchShare("not a real ID")
	if !errors.Is(err, errors.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}
}

func testPPSEligibleShares(t *testing.T) {
	now := time.Now()
	sixtyBefore := now.Add(-(time.Second * 60)).UnixNano()
	eightyBefore := now.Add(-(time.Second * 80)).UnixNano()
	tenAfter := now.Add(time.Second * 10).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)

	shareCount := 1
	expectedShareCount := 2

	err := persistShare(db, xID, weight, eightyBefore) // Share A
	if err != nil {
		t.Fatal(err)
	}

	err = persistShare(db, xID, weight, tenAfter) // Share B
	if err != nil {
		t.Fatal(err)
	}

	err = persistShare(db, yID, weight, sixtyBefore) // Share C
	if err != nil {
		t.Fatal(err)
	}

	err = persistShare(db, yID, weight, tenAfter) // Share D
	if err != nil {
		t.Fatal(err)
	}

	// Fetch eligible shares at minimum time.
	shares, err := db.ppsEligibleShares(sixtyBefore)
	if err != nil {
		t.Fatalf("PPSEligibleShares: unexpected error: %v", err)
	}

	// Ensure the returned share count is as expected.
	if len(shares) != expectedShareCount {
		t.Fatalf("PPS error: expected %v eligible PPS shares, got %v",
			expectedShareCount, len(shares))
	}

	forAccX := 0
	forAccY := 0
	for _, share := range shares {
		if share.Account == xID {
			forAccX++
		}

		if share.Account == yID {
			forAccY++
		}
	}

	// Ensure account x and account y both have shares returned.
	if forAccX == 0 || forAccY == 0 {
		t.Fatalf("PPS error: expected shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have equal number of shares.
	if forAccX != forAccY {
		t.Fatalf("PPS error: expected equal shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have shares equal to the share count.
	if forAccX != shareCount || forAccY != shareCount {
		t.Fatalf("PPS error: expected share counts of %v for account X and Y, "+
			"got %v (for x), %v (for y).", shareCount, forAccX, forAccY)
	}
}

func testPPLNSEligibleShares(t *testing.T) {
	now := time.Now()
	sixtyBefore := now.Add(-(time.Second * 60)).UnixNano()
	eightyBefore := now.Add(-(time.Second * 80)).UnixNano()
	tenAfter := now.Add(time.Second * 10).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)

	shareCount := 1
	expectedShareCount := 2

	// Create a share below the minimum exclusive PPLNS time for account x.
	err := persistShare(db, xID, weight, eightyBefore)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share below the minimum exclusive PPLNS time for account y.
	err = persistShare(db, yID, weight, eightyBefore)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, sixtyBefore)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share at minimum exclusive PPLNS time for account y.
	err = persistShare(db, yID, weight, sixtyBefore)
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above minimum exclusive PPLNS time for account x.
	err = persistShare(db, xID, weight, now.UnixNano())
	if err != nil {
		t.Fatal(err)
	}

	// Create a share above minimum exclusive PPLNS time for account y.
	err = persistShare(db, yID, weight, tenAfter)
	if err != nil {
		t.Fatal(err)
	}

	shares, err := db.pplnsEligibleShares(sixtyBefore)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure the returned number of shares is as expected.
	if len(shares) != expectedShareCount {
		t.Fatalf("PPLNS error: expected %v eligible PPLNS shares, got %v",
			expectedShareCount, len(shares))
	}

	forAccX := 0
	forAccY := 0
	for _, share := range shares {
		if share.Account == xID {
			forAccX++
		}

		if share.Account == yID {
			forAccY++
		}
	}

	// Ensure account x and account y both have shares returned.
	if forAccX == 0 || forAccY == 0 {
		t.Fatalf("PPLNS error: expected shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have equal number of shares.
	if forAccX != forAccY {
		t.Fatalf("PPLNS error: expected equal shares for account X and Y, "+
			"got %v (for x), %v (for y).", forAccX, forAccY)
	}

	// Ensure account x and account y have shares equal to the share count.
	if forAccX != shareCount || forAccY != shareCount {
		t.Fatalf("PPLNS error: expected share counts of %v for account X and Y, "+
			"got %v (for x), %v (for y).", shareCount, forAccX, forAccY)
	}
}

func testPruneShares(t *testing.T) {
	now := time.Now()
	sixtyBefore := now.Add(-(time.Second * 60)).UnixNano()
	thirtyBefore := now.Add(-(time.Second * 30)).UnixNano()
	eightyBefore := now.Add(-(time.Second * 80)).UnixNano()
	tenAfter := now.Add(time.Second * 10).UnixNano()
	weight := new(big.Rat).SetFloat64(1.0)

	err := persistShare(db, xID, weight, eightyBefore) // Share A
	if err != nil {
		t.Fatal(err)
	}

	err = persistShare(db, yID, weight, thirtyBefore) // Share B
	if err != nil {
		t.Fatal(err)
	}

	err = db.pruneShares(sixtyBefore)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure share A got pruned with share B remaining.
	shareAID := shareID(xID, eightyBefore)
	_, err = db.fetchShare(shareAID)
	if !errors.Is(err, errors.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}

	shareBID := shareID(yID, thirtyBefore)
	_, err = db.fetchShare(shareBID)
	if err != nil {
		t.Fatalf("unexpected error fetching share B: %v", err)
	}

	err = db.pruneShares(tenAfter)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure share B got pruned.
	_, err = db.fetchShare(shareBID)
	if !errors.Is(err, errors.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}
}
