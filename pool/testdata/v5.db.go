// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

// This file should compiled from the commit the file was introduced, otherwise
// it may not compile due to API changes, or may not create the database with
// the correct old version.  This file should not be updated for API changes.

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrpool/pool"
)

const dbname = "v5.db"

func main() {
	err := setup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}
	err = compress()
	if err != nil {
		fmt.Fprintf(os.Stderr, "compress: %v\n", err)
		os.Exit(1)
	}
}

func setup() error {
	xID := pool.AccountID("SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc")
	yID := pool.AccountID("Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS")

	db, err := pool.InitBoltDB(dbname, true)
	if err != nil {
		return err
	}

	amt, err := dcrutil.NewAmount(50.0)
	if err != nil {
		return err
	}

	zeroSource := &pool.PaymentSource{
		BlockHash: chainhash.Hash{0}.String(),
		Coinbase:  chainhash.Hash{0}.String(),
	}

	height := uint32(10)
	estMaturity := uint32(26)

	pmtA := pool.NewPayment(xID, zeroSource, amt, height, estMaturity)
	err = db.PersistPayment(pmtA)
	if err != nil {
		return err
	}

	pmtB := pool.NewPayment(yID, zeroSource, amt, height, estMaturity)
	err = db.PersistPayment(pmtB)
	if err != nil {
		return err
	}

	pmtC := pool.NewPayment(xID, zeroSource, amt, height-1, estMaturity-1)
	err = db.ArchivePayment(pmtC)
	if err != nil {
		return err
	}
	pmtD := pool.NewPayment(yID, zeroSource, amt, height-1, estMaturity-1)
	err = db.ArchivePayment(pmtD)
	if err != nil {
		return err
	}

	return nil
}

func compress() error {
	db, err := os.Open(dbname)
	if err != nil {
		return err
	}
	defer os.Remove(dbname)
	defer db.Close()
	dbgz, err := os.Create(dbname + ".gz")
	if err != nil {
		return err
	}
	defer dbgz.Close()
	gz := gzip.NewWriter(dbgz)
	_, err = io.Copy(gz, db)
	if err != nil {
		return err
	}
	return gz.Close()
}
