// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"errors"
	"testing"

	errs "github.com/decred/dcrpool/errors"
)

func testAcceptedWork(t *testing.T) {
	// Created some valid accepted work.
	const (
		xClient  = "cpux"
		x2Client = "cpux2"
		yClient  = "cpuy"
	)
	workA := NewAcceptedWork(
		"00000000000000001e2065a7248a9b4d3886fe3ca3128eebedddaf35fb26e58c",
		"000000000000000007301a21efa98033e06f7eba836990394fff9f765f1556b1",
		396692, yID, yClient)
	err := db.persistAcceptedWork(workA)
	if err != nil {
		t.Fatal(err)
	}

	workB := NewAcceptedWork(
		"000000000000000025aa4a7ba8c3ece4608376bf84a82ec7e025991460097198",
		"00000000000000001e2065a7248a9b4d3886fe3ca3128eebedddaf35fb26e58c",
		396693, xID, xClient)
	err = db.persistAcceptedWork(workB)
	if err != nil {
		t.Fatal(err)
	}

	workC := NewAcceptedWork(
		"0000000000000000053236ce6c274aa49a1cc6e9d906e855725c79f69c1089d3",
		"000000000000000025aa4a7ba8c3ece4608376bf84a82ec7e025991460097198",
		396694, xID, xClient)
	err = db.persistAcceptedWork(workC)
	if err != nil {
		t.Fatal(err)
	}

	workD := NewAcceptedWork(
		"000000000000000020f9ab2b1e144a818d36a857aefda55363f5e86e01855c79",
		"0000000000000000053236ce6c274aa49a1cc6e9d906e855725c79f69c1089d3",
		396695, xID, xClient)
	err = db.persistAcceptedWork(workD)
	if err != nil {
		t.Fatal(err)
	}

	workE := NewAcceptedWork(
		"0000000000000000032e25218be722327ae3dccf9015756facb2f98931fda7b8",
		"00000000000000000476712b2f5df31bc62b9976066262af2d639a551853c056",
		431611, xID, x2Client)

	// Ensure updating a non persisted accepted work returns an error.
	err = db.updateAcceptedWork(workE)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}

	// Ensure creating an already existing accepted work returns an error.
	err = db.persistAcceptedWork(workD)
	if !errors.Is(err, errs.ValueFound) {
		t.Fatalf("expected value found error, got %v", err)
	}

	// Ensure fetching a non existent accepted work returns an error.
	id := AcceptedWorkID(workC.BlockHash, workD.Height)
	_, err = db.fetchAcceptedWork(id)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}

	// Fetch an accepted work with its id.
	id = AcceptedWorkID(workC.BlockHash, workC.Height)
	fetchedWork, err := db.fetchAcceptedWork(id)
	if err != nil {
		t.Fatalf("fetchAcceptedWork error: %v", err)
	}

	// Ensure fetched values match persisted values.
	if fetchedWork.BlockHash != workC.BlockHash {
		t.Fatalf("expected (%v) as fetched work block hash, got (%v)",
			fetchedWork.BlockHash, workC.BlockHash)
	}

	if fetchedWork.UUID != workC.UUID {
		t.Fatalf("expected (%v) as fetched work block id, got (%v)",
			fetchedWork.UUID, workC.UUID)
	}

	if fetchedWork.PrevHash != workC.PrevHash {
		t.Fatalf("expected (%v) as fetched work block prevhash, got (%v)",
			fetchedWork.PrevHash, workC.PrevHash)
	}

	if fetchedWork.Height != workC.Height {
		t.Fatalf("expected (%v) as fetched work block height, got (%v)",
			fetchedWork.Height, workC.Height)
	}

	if fetchedWork.MinedBy != workC.MinedBy {
		t.Fatalf("expected (%v) as fetched work block minedby, got (%v)",
			fetchedWork.MinedBy, workC.MinedBy)
	}

	if fetchedWork.Miner != workC.Miner {
		t.Fatalf("expected (%v) as fetched work block miner, got (%v)",
			fetchedWork.Miner, workC.Miner)
	}

	if fetchedWork.CreatedOn != workC.CreatedOn {
		t.Fatalf("expected (%v) as fetched work block createdon, got (%v)",
			fetchedWork.CreatedOn, workC.CreatedOn)
	}

	if fetchedWork.Confirmed != workC.Confirmed {
		t.Fatalf("expected (%v) as fetched work block confirmed, got (%v)",
			fetchedWork.Confirmed, workC.Confirmed)
	}

	// Ensure unconfirmed work is returned
	minedWork, err := db.listMinedWork()
	if err != nil {
		t.Fatalf("listMinedWork error: %v", err)
	}

	if len(minedWork) != 4 {
		t.Fatalf("expected unconfirmed work to be returned")
	}

	// Confirm all accepted work a mined work.
	workA.Confirmed = true
	err = db.updateAcceptedWork(workA)
	if err != nil {
		t.Fatalf("confirm workA error: %v ", err)
	}

	workB.Confirmed = true
	err = db.updateAcceptedWork(workB)
	if err != nil {
		t.Fatalf("confirm workB error: %v ", err)
	}

	workC.Confirmed = true
	err = db.updateAcceptedWork(workC)
	if err != nil {
		t.Fatalf("confirm workC error: %v ", err)
	}

	workD.Confirmed = true
	err = db.updateAcceptedWork(workD)
	if err != nil {
		t.Fatalf("confirm workD error: %v ", err)
	}

	// Ensure accepted work are listed as mined since they are confirmed.
	minedWork, err = db.listMinedWork()
	if err != nil {
		t.Fatalf("listMinedWork error: %v", err)
	}

	if len(minedWork) != 4 {
		t.Fatalf("expected %v mined work, got %v", 4, len(minedWork))
	}

	// Ensure account Y has only one associated mined work.
	allWork, err := db.listMinedWork()
	if err != nil {
		t.Fatalf("listMinedWork error: %v", err)
	}

	minedWorkByAccount := make([]*AcceptedWork, 0)
	for _, work := range allWork {
		if work.MinedBy == yID && work.Confirmed {
			minedWorkByAccount = append(minedWorkByAccount, work)
		}
	}

	if len(minedWorkByAccount) != 1 {
		t.Fatalf("expected %v mined work for account %v, got %v", 1,
			yID, len(minedWork))
	}

	// Delete all work.
	err = db.deleteAcceptedWork(workA.UUID)
	if err != nil {
		t.Fatalf("delete workA error: %v ", err)
	}

	err = db.deleteAcceptedWork(workB.UUID)
	if err != nil {
		t.Fatalf("delete workB error: %v ", err)
	}
	err = db.deleteAcceptedWork(workC.UUID)
	if err != nil {
		t.Fatalf("delete workC error: %v ", err)
	}

	err = db.deleteAcceptedWork(workD.UUID)
	if err != nil {
		t.Fatalf("delete workD error: %v ", err)
	}

	// Ensure there are no mined work.
	minedWork, err = db.listMinedWork()
	if err != nil {
		t.Fatalf("listMinedWork error: %v", err)
	}

	if len(minedWork) != 0 {
		t.Fatalf("expected %v mined work, got %v", 0, len(minedWork))
	}
}
