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
	"math/big"
	"os"

	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrpool/pool"
)

const dbname = "v1.db"

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

	weight := new(big.Rat).SetFloat64(1.0)
	db, err := pool.InitDB(dbname, true)
	if err != nil {
		return err
	}

	shareX := pool.NewShare(xID, weight)
	err = shareX.Create(db)
	if err != nil {
		return err
	}
	shareY := pool.NewShare(yID, weight)
	err = shareY.Create(db)
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
