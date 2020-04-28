package pool

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func persistAcceptedWork(db *bolt.DB, blockHash string, prevHash string,
	height uint32, minedBy string, miner string) (*AcceptedWork, error) {
	acceptedWork := NewAcceptedWork(blockHash, prevHash, height, minedBy, miner)
	err := acceptedWork.Create(db)
	if err != nil {
		return nil, fmt.Errorf("unable to persist accepted work: %v", err)
	}
	return acceptedWork, nil
}

func listMinedWorkByAccount(db *bolt.DB, accountID string) ([]*AcceptedWork, error) {
	minedWork := make([]*AcceptedWork, 0)

	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchWorkBucket(tx)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.Last(); k != nil; k, v = cursor.Prev() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				return err
			}

			if strings.Compare(work.MinedBy, accountID) == 0 && work.Confirmed {
				minedWork = append(minedWork, &work)
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return minedWork, nil
}

func testAcceptedWork(t *testing.T, db *bolt.DB) {
	workA, err := persistAcceptedWork(db,
		"00000000000000001e2065a7248a9b4d3886fe3ca3128eebedddaf35fb26e58c",
		"000000000000000007301a21efa98033e06f7eba836990394fff9f765f1556b1",
		396692, yID, "dr3")
	if err != nil {
		t.Fatal(err)
	}

	workB, err := persistAcceptedWork(db,
		"000000000000000025aa4a7ba8c3ece4608376bf84a82ec7e025991460097198",
		"00000000000000001e2065a7248a9b4d3886fe3ca3128eebedddaf35fb26e58c",
		396693, xID, "dr5")
	if err != nil {
		t.Fatal(err)
	}

	workC, err := persistAcceptedWork(db,
		"0000000000000000053236ce6c274aa49a1cc6e9d906e855725c79f69c1089d3",
		"000000000000000025aa4a7ba8c3ece4608376bf84a82ec7e025991460097198",
		396694, xID, "dr5")
	if err != nil {
		t.Fatal(err)
	}

	workD, err := persistAcceptedWork(db,
		"000000000000000020f9ab2b1e144a818d36a857aefda55363f5e86e01855c79",
		"0000000000000000053236ce6c274aa49a1cc6e9d906e855725c79f69c1089d3",
		396695, xID, "dr5")
	if err != nil {
		t.Fatal(err)
	}

	workE := NewAcceptedWork(
		"0000000000000000032e25218be722327ae3dccf9015756facb2f98931fda7b8",
		"00000000000000000476712b2f5df31bc62b9976066262af2d639a551853c056",
		431611, xID, "dcr1")

	// Ensure updating a non persisted accepted work returns an error.
	err = workE.Update(db)
	if err == nil {
		t.Fatal("Update: expected a no accepted work found error")
	}

	// Ensure creating an already existing accepted work returns an error.
	err = workD.Create(db)
	if err == nil {
		t.Fatal("Create: expected a duplicate accepted work error")
	}

	// Ensure fetching a non existent accepted work returns an error.
	id := AcceptedWorkID(workC.BlockHash, workD.Height)

	_, err = FetchAcceptedWork(db, id)
	if err == nil {
		t.Fatalf("FetchAcceptedWork: expected a non-existent accepted work error")
	}

	// Fetch an accepted work with its id.
	id = AcceptedWorkID(workC.BlockHash, workC.Height)

	fetchedWork, err := FetchAcceptedWork(db, id)
	if err != nil {
		t.Fatalf("FetchAcceptedWork error: %v", err)
	}

	if fetchedWork.BlockHash != workC.BlockHash {
		t.Fatalf("expected (%v) as fetched work block hash, got (%v)",
			fetchedWork.BlockHash, workC.BlockHash)
	}

	if fetchedWork.Height != workC.Height {
		t.Fatalf("expected (%v) as fetched work block height, got (%v)",
			fetchedWork.Height, workC.Height)
	}

	// Ensure unconfirmed work is returned
	minedWork, err := ListMinedWork(db)
	if err != nil {
		t.Fatalf("ListMinedWork error: %v", err)
	}

	if len(minedWork) != 4 {
		t.Fatalf("expected unconfirmed work to be returned")
	}

	// Confirm all accepted work a mined work.
	workA.Confirmed = true
	err = workA.Update(db)
	if err != nil {
		t.Fatalf("confirm workA error: %v ", err)
	}

	workB.Confirmed = true
	err = workB.Update(db)
	if err != nil {
		t.Fatalf("confirm workB error: %v ", err)
	}

	workC.Confirmed = true
	err = workC.Update(db)
	if err != nil {
		t.Fatalf("confirm workC error: %v ", err)
	}

	workD.Confirmed = true
	err = workD.Update(db)
	if err != nil {
		t.Fatalf("confirm workD error: %v ", err)
	}

	// Ensure accepted work are listed as mined since they are confirmed.
	minedWork, err = ListMinedWork(db)
	if err != nil {
		t.Fatalf("ListMinedWork error: %v", err)
	}

	if len(minedWork) != 4 {
		t.Fatalf("expected %v mined work, got %v", 4, len(minedWork))
	}

	// Ensure mined work cannot be pruned.
	err = pruneAcceptedWork(db, workD.Height+1)
	if err != nil {
		t.Fatalf("PruneAcceptedWork error: %v", err)
	}

	minedWork, err = ListMinedWork(db)
	if err != nil {
		t.Fatalf("ListMinedWork error: %v", err)
	}

	if len(minedWork) != 4 {
		t.Fatalf("expected %v mined work, got %v", 4, len(minedWork))
	}

	// Ensure account Y has only one associated mined work.
	minedWork, err = listMinedWorkByAccount(db, yID)
	if err != nil {
		t.Fatalf("ListMinedWork error: %v", err)
	}

	if len(minedWork) != 1 {
		t.Fatalf("expected %v mined work for account %v, got %v", 1,
			yID, len(minedWork))
	}

	// Update work A and B as unconfirmed.
	workA.Confirmed = false
	err = workA.Update(db)
	if err != nil {
		t.Fatalf("unconfirm workA error: %v ", err)
	}

	workB.Confirmed = false
	err = workB.Update(db)
	if err != nil {
		t.Fatalf("unconfirm workB error: %v ", err)
	}

	// Ensure unconfirmed work can be pruned.
	err = pruneAcceptedWork(db, workD.Height+1)
	if err != nil {
		t.Fatalf("PruneAcceptedWork error: %v", err)
	}

	_, err = FetchAcceptedWork(db, []byte(workA.UUID))
	if err == nil {
		t.Fatal("expected a work not found error")
	}

	// Delete work C and D.
	err = workC.Delete(db)
	if err != nil {
		t.Fatalf("delete workC error: %v ", err)
	}

	err = workD.Delete(db)
	if err != nil {
		t.Fatalf("delete workD error: %v ", err)
	}

	// Ensure there are no mined work.
	minedWork, err = ListMinedWork(db)
	if err != nil {
		t.Fatalf("ListMinedWork error: %v", err)
	}

	if len(minedWork) != 0 {
		t.Fatalf("expected %v mined work, got %v", 0, len(minedWork))
	}
}
