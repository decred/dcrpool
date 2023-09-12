// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"errors"
	"math/big"
	"testing"
	"time"

	errs "github.com/decred/dcrpool/errors"
)

func testHashData(t *testing.T) {
	miner := ObeliskDCR1
	extraNonce1 := "ca750c60"
	ip := "127.0.0.1:5550"
	now := time.Now()
	hashRate := new(big.Rat).SetInt64(100)

	hashData := newHashData(miner, xID, ip, extraNonce1, hashRate)

	// Ensure hash data can be persisted.
	err := db.persistHashData(hashData)
	if err != nil {
		t.Fatal(err)
	}

	// Ensure hash data can be fetched.
	hashID := hashData.UUID
	fetchedHashData, err := db.fetchHashData(hashID)
	if err != nil {
		t.Fatal(err)
	}

	// Assert fetched values match expected values.
	if fetchedHashData.UpdatedOn != hashData.UpdatedOn {
		t.Fatalf("expected updated on value of %v, got %v",
			hashData.UpdatedOn, fetchedHashData.UpdatedOn)
	}

	if fetchedHashData.HashRate.RatString() != hashData.HashRate.RatString() {
		t.Fatalf("expected hash rate value of %v, got %v",
			hashData.HashRate, fetchedHashData.HashRate)
	}

	if fetchedHashData.IP != hashData.IP {
		t.Fatalf("expected ip value of %v, got %v",
			hashData.IP, fetchedHashData.IP)
	}

	if fetchedHashData.Miner != hashData.Miner {
		t.Fatalf("expected miner value of %v, got %v",
			hashData.UpdatedOn, fetchedHashData.UpdatedOn)
	}

	if fetchedHashData.AccountID != hashData.AccountID {
		t.Fatalf("expected account id value of %v, got %v",
			hashData.AccountID, fetchedHashData.AccountID)
	}

	// Ensure fetching a non-existent hash data returns an error.
	invalidHashID := hashDataID(yID, extraNonce1)
	_, err = db.fetchHashData(invalidHashID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected a value not found error for "+
			"non-existent hash data, got %v", err)
	}

	fiveMinutesAfter := now.Add(time.Minute * 5).UnixNano()
	fiveMinutesBefore := now.Add(-time.Minute * 5).UnixNano()

	// Ensure listing account hash data adheres to the minimum update
	// time constraint.
	dataset, err := db.listHashData(fiveMinutesAfter)
	if err != nil {
		t.Fatal(err)
	}

	if len(dataset) > 0 {
		t.Fatalf("expected no hash data, got %d", len(dataset))
	}

	dataset, err = db.listHashData(fiveMinutesBefore)
	if err != nil {
		t.Fatal(err)
	}

	if len(dataset) != 1 {
		t.Fatalf("expected one hash data, got %d", len(dataset))
	}

	// Ensure the hash id is a key of the hash data set returned.
	_, ok := dataset[hashID]
	if !ok {
		t.Fatalf("expected dataset to have %s as a hash data key", hashID)
	}

	newUpdatedOn := hashData.UpdatedOn + 100
	hashData.UpdatedOn = newUpdatedOn

	// Ensure hash data can be updated.
	err = db.updateHashData(hashData)
	if err != nil {
		t.Fatal(err)
	}

	fetchedHashData, err = db.fetchHashData(hashID)
	if err != nil {
		t.Fatal(err)
	}

	if fetchedHashData.UpdatedOn != hashData.UpdatedOn {
		t.Fatalf("expected updated on time to be %d, got %d",
			hashData.UpdatedOn, fetchedHashData.UpdatedOn)
	}

	// Ensure pruning hash data adheres to the minimum update time constraint.
	err = db.pruneHashData(fiveMinutesBefore)
	if err != nil {
		t.Fatalf("unexpected pruning error: %v", err)
	}

	_, err = db.fetchHashData(hashID)
	if err != nil {
		t.Fatalf("expected a valid hash data returned, got %v", err)
	}

	err = db.pruneHashData(fiveMinutesAfter)
	if err != nil {
		t.Fatalf("unexpected pruning error: %v", err)
	}

	_, err = db.fetchHashData(hashID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected a value not found error for "+
			"pruned hash data, got %v", err)
	}
}
