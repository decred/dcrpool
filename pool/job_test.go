package pool

import (
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func persistJob(db *bolt.DB, header string, height uint32) (*Job, error) {
	job, err := NewJob(header, height)
	if err != nil {
		return nil, fmt.Errorf("unable to create job: %v", err)
	}
	err = job.Create(db)
	if err != nil {
		return nil, fmt.Errorf("unable to persist job: %v", err)
	}
	return job, nil
}

func testJob(t *testing.T, db *bolt.DB) {
	jobA, err := persistJob(db, "0700000093bdee7083c6e02147cf76724a685f0148636"+
		"b2faf96353d1cbf5c0a954100007991153ad03eb0e31ead44b75ebc9f760870098431d4e6"+
		"aa85e742cbad517ebd853b9bf059e8eeb91591e4a7d4005acc62e92bfd27b17309a5a41dd"+
		"24016428f0100000000000000000000003c000000dd742920204e00000000000038000000"+
		"66010000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 56)
	if err != nil {
		t.Fatal(err)
	}

	jobB, err := persistJob(db, "0700000047e9425eabcf920eecf0c00c7bc46c6062049"+
		"071c59edcb0e55c0226690800005695619a600321a8389d1bee5b3a207efc81e05c111d38"+
		"1e960c8bf05ca336b55b528e9d5044c52aa0c713ae152f3fdb592f6ee82fa1776440ca72a"+
		"2fc9f77760100000000000000000000003c000000dd742920204e00000000000039000000"+
		"a6030000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 57)
	if err != nil {
		t.Fatal(err)
	}

	jobC, err := persistJob(db, "070000005c05ebb5be0fa2b5785ab3ca72294e6e1865"+
		"9df687f77605d31388a52875000003b7a9efb7222e37102c0de947df02107d455b9d76af"+
		"76366d381a28f2dacdadabb7a2afa76f07917d676bd3033fc48f3ce77312729fa43d8a23"+
		"d680effcc019010000000000000000000a003c000000dd742920204e0000000000003a00"+
		"0000d9180000f371cc5d0000000000000000000000000000000000000000000000000000"+
		"00000000000000000000000000008000000100000000000005a0", 58)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch a job using its id.
	fetchedJob, err := FetchJob(db, []byte(jobA.UUID))
	if err != nil {
		t.Fatalf("FetchJob err: %v", err)
	}

	if fetchedJob == nil {
		t.Fatal("expected a non-nil job")
	}

	// Ensure jobs cannot be updated.
	jobB.Height = 60
	err = jobB.Update(db)
	if err == nil {
		t.Fatal("expected a not supported error")
	}

	jobB.Height = 57

	// Ensure jobs can be pruned.
	err = PruneJobs(db, 57)
	if err != nil {
		t.Fatalf("PruneJobs error: %v", err)
	}

	// Delete the last remaining job.
	err = jobC.Delete(db)
	if err != nil {
		t.Fatalf("job delete error: %v", err)
	}

	// Ensure the job was deleted.
	fetchedJob, err = FetchJob(db, []byte(jobC.UUID))
	if err == nil {
		t.Fatalf("expected a value not found error: %v", err)
	}

	if fetchedJob != nil {
		t.Fatal("expected a non-nil job")
	}
}
