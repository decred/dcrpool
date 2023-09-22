// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"

	errs "github.com/decred/dcrpool/errors"
)

const (
	// TransacionIDVersion is the second version of the database. It adds the
	// transactionId field to the payments struct for payment tracking purposes.
	transactionIDVersion = 1

	// BoltDBVersion is the latest version of the bolt database that is
	// understood by the program. Databases with recorded versions higher than
	// this will fail to open (meaning any upgrades prevent reverting to older
	// software).
	BoltDBVersion = transactionIDVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.
var upgrades = [...]func(tx *bolt.Tx) error{
	transactionIDVersion - 1: transactionIDUpgrade,
}

func fetchDBVersion(tx *bolt.Tx) (uint32, error) {
	const funcName = "fetchDBVersion"
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return 0, errs.DBError(errs.StorageNotFound, desc)
	}
	v := pbkt.Get(versionK)
	if v == nil {
		desc := fmt.Sprintf("%s: db version not set", funcName)
		return 0, errs.DBError(errs.ValueNotFound, desc)
	}

	return binary.LittleEndian.Uint32(v), nil
}

func setDBVersion(tx *bolt.Tx, newVersion uint32) error {
	const funcName = "setDBVersion"
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return errs.DBError(errs.StorageNotFound, desc)
	}

	vBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(vBytes, newVersion)
	err := pbkt.Put(versionK, vBytes)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to persist version: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}

	return nil
}

func transactionIDUpgrade(tx *bolt.Tx) error {
	const oldVersion = 0
	const newVersion = 1

	const funcName = "transactionIDUpgrade"

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := fmt.Sprintf("%s: inappropriately called", funcName)
		return errs.DBError(errs.DBUpgrade, desc)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return errs.DBError(errs.StorageNotFound, desc)
	}

	// Update all entries in the payment and payment archive buckets.
	//
	// All transaction ids for payments before the upgrade will be set to
	// an empty string.

	pmtbkt := pbkt.Bucket(paymentBkt)
	if pmtbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentBkt))
		return errs.DBError(errs.StorageNotFound, desc)
	}

	pmtCursor := pmtbkt.Cursor()
	for k, v := pmtCursor.First(); k != nil; k, v = pmtCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return errs.DBError(errs.Parse, desc)
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return errs.DBError(errs.Parse, desc)
		}

		err = pmtbkt.Put(k, pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return errs.DBError(errs.PersistEntry, desc)
		}
	}

	abkt := pbkt.Bucket(paymentArchiveBkt)
	if abkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentArchiveBkt))
		return errs.DBError(errs.StorageNotFound, desc)
	}

	aCursor := abkt.Cursor()
	for k, v := aCursor.First(); k != nil; k, v = aCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return errs.DBError(errs.Parse, desc)
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment: %v",
				funcName, err)
			return errs.DBError(errs.Parse, desc)
		}

		err = abkt.Put(k, pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return errs.DBError(errs.PersistEntry, desc)
		}
	}

	return setDBVersion(tx, newVersion)
}

// upgradeDB checks whether any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func upgradeDB(db *BoltDB) error {
	var version uint32
	err := db.DB.View(func(tx *bolt.Tx) error {
		var err error
		version, err = fetchDBVersion(tx)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	if version == BoltDBVersion {
		// No upgrades necessary.
		return nil
	}

	if version > BoltDBVersion {
		// Database is too new.
		return fmt.Errorf("expected database version <= %d, got %d", BoltDBVersion, version)
	}

	log.Infof("Upgrading database from version %d to %d", version, BoltDBVersion)

	return db.DB.Update(func(tx *bolt.Tx) error {
		// Execute all necessary upgrades in order.
		for _, upgrade := range upgrades[version:] {
			err := upgrade(tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
}
