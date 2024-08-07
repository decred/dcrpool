// Copyright (c) 2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"errors"
	"testing"

	errs "github.com/decred/dcrpool/errors"
)

func testJob(t *testing.T) {
	// Create some valid jobs.
	jobA := NewJob("0700000093bdee7083c6e02147cf76724a685f0148636"+
		"b2faf96353d1cbf5c0a954100007991153ad03eb0e31ead44b75ebc9f760870098431d4e6"+
		"aa85e742cbad517ebd853b9bf059e8eeb91591e4a7d4005acc62e92bfd27b17309a5a41dd"+
		"24016428f0100000000000000000000003c000000dd742920204e00000000000038000000"+
		"66010000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 56)
	err := db.persistJob(jobA)
	if err != nil {
		t.Fatal(err)
	}

	jobB := NewJob("0700000047e9425eabcf920eecf0c00c7bc46c6062049"+
		"071c59edcb0e55c0226690800005695619a600321a8389d1bee5b3a207efc81e05c111d38"+
		"1e960c8bf05ca336b55b528e9d5044c52aa0c713ae152f3fdb592f6ee82fa1776440ca72a"+
		"2fc9f77760100000000000000000000003c000000dd742920204e00000000000039000000"+
		"a6030000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 57)
	err = db.persistJob(jobB)
	if err != nil {
		t.Fatal(err)
	}

	jobC := NewJob("070000005c05ebb5be0fa2b5785ab3ca72294e6e1865"+
		"9df687f77605d31388a52875000003b7a9efb7222e37102c0de947df02107d455b9d76af"+
		"76366d381a28f2dacdadabb7a2afa76f07917d676bd3033fc48f3ce77312729fa43d8a23"+
		"d680effcc019010000000000000000000a003c000000dd742920204e0000000000003a00"+
		"0000d9180000f371cc5d0000000000000000000000000000000000000000000000000000"+
		"00000000000000000000000000008000000100000000000005a0", 58)
	err = db.persistJob(jobC)
	if err != nil {
		t.Fatal(err)
	}

	// Creating the same job twice should fail.
	err = db.persistJob(jobA)
	if !errors.Is(err, errs.ValueFound) {
		t.Fatalf("expected value found error, got %v", err)
	}

	// Fetch a job using its id.
	fetchedJob, err := db.fetchJob(jobA.UUID)
	if err != nil {
		t.Fatalf("fetchJob err: %v", err)
	}

	// Ensure fetched values match persisted values.
	if fetchedJob.Header != jobA.Header {
		t.Fatalf("expected %v as fetched job header, got %v",
			jobA.Header, fetchedJob.Header)
	}

	if fetchedJob.UUID != jobA.UUID {
		t.Fatalf("expected %v as fetched job id, got %v",
			jobA.UUID, fetchedJob.UUID)
	}

	if fetchedJob.Height != jobA.Height {
		t.Fatalf("expected %v as fetched job height, got %v",
			jobA.Height, fetchedJob.Height)
	}

	// Delete jobs B and C.
	err = db.deleteJob(jobB.UUID)
	if err != nil {
		t.Fatalf("job delete error: %v", err)
	}

	err = db.deleteJob(jobC.UUID)
	if err != nil {
		t.Fatalf("job delete error: %v", err)
	}

	// Ensure the jobs were deleted.
	_, err = db.fetchJob(jobB.UUID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}
	_, err = db.fetchJob(jobC.UUID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}

	// Deleting an job which does not exist should not return an error.
	err = db.deleteJob(jobA.UUID)
	if err != nil {
		t.Fatalf("delete jobA error: %v ", err)
	}
}

func testDeleteJobsBeforeHeight(t *testing.T) {
	// Create some valid jobs.
	jobA := NewJob("0700000093bdee7083c6e02147cf76724a685f0148636"+
		"b2faf96353d1cbf5c0a954100007991153ad03eb0e31ead44b75ebc9f760870098431d4e6"+
		"aa85e742cbad517ebd853b9bf059e8eeb91591e4a7d4005acc62e92bfd27b17309a5a41dd"+
		"24016428f0100000000000000000000003c000000dd742920204e00000000000038000000"+
		"66010000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 56)
	err := db.persistJob(jobA)
	if err != nil {
		t.Fatal(err)
	}

	jobB := NewJob("0700000047e9425eabcf920eecf0c00c7bc46c6062049"+
		"071c59edcb0e55c0226690800005695619a600321a8389d1bee5b3a207efc81e05c111d38"+
		"1e960c8bf05ca336b55b528e9d5044c52aa0c713ae152f3fdb592f6ee82fa1776440ca72a"+
		"2fc9f77760100000000000000000000003c000000dd742920204e00000000000039000000"+
		"a6030000f171cc5d000000000000000000000000000000000000000000000000000000000"+
		"000000000000000000000008000000100000000000005a0", 57)
	err = db.persistJob(jobB)
	if err != nil {
		t.Fatal(err)
	}

	// Delete jobs below height 57.
	err = db.deleteJobsBeforeHeight(57)
	if err != nil {
		t.Fatalf("deleteJobsBeforeHeight error: %v", err)
	}

	// Ensure job A has been pruned with job B remaining.
	_, err = db.fetchJob(jobA.UUID)
	if !errors.Is(err, errs.ValueNotFound) {
		t.Fatalf("expected value not found error, got %v", err)
	}

	_, err = db.fetchJob(jobB.UUID)
	if err != nil {
		t.Fatalf("unexpected error fetching job B: %v", err)
	}
}
