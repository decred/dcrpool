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
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrpool/pool"
)

const dbname = "v2.db"

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
	xID, err := pool.AccountID("SsWKp7wtdTZYabYFYSc9cnxhwFEjA5g4pFc",
		chaincfg.SimNetParams())
	if err != nil {
		return err
	}
	yID, err := pool.AccountID("Ssp7J7TUmi5iPhoQnWYNGQbeGhu6V3otJcS",
		chaincfg.SimNetParams())
	if err != nil {
		return err
	}

	db, err := pool.InitDB(dbname, true)
	if err != nil {
		return err
	}

	height := uint32(40)
	estMaturity := uint32(56)
	amt, err := dcrutil.NewAmount(10)
	if err != nil {
		return err
	}

	pmtA := pool.NewPayment(xID, amt, height, estMaturity)
	pmtA.Create(db)
	if err != nil {
		return err
	}

	pmtB := pool.NewPayment(yID, amt.MulF64(0.5), height, estMaturity)
	pmtB.Create(db)
	if err != nil {
		return err
	}

	pmtC := pool.NewPayment(xID, amt.MulF64(0.7), height, estMaturity)
	pmtB.Create(db)
	if err != nil {
		return err
	}

	pmtC.PaidOnHeight = estMaturity + 1
	pmtC.TransactionID = chainhash.Hash{0}.String()
	pmtC.Create(db)
	if err != nil {
		return err
	}

	pmtD := pool.NewPayment(xID, amt.MulF64(0.9), height, estMaturity)
	pmtB.Create(db)
	if err != nil {
		return err
	}

	pmtD.PaidOnHeight = estMaturity + 1
	pmtD.TransactionID = chainhash.Hash{0}.String()
	pmtD.Create(db)
	if err != nil {
		return err
	}

	bundle := pool.PaymentBundle{
		Account:  xID,
		Payments: []*pool.Payment{pmtC, pmtD},
	}

	err = bundle.ArchivePayments(db)
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
