// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/binary"
	"fmt"

	bolt "go.etcd.io/bbolt"

	errs "github.com/decred/dcrpool/errors"
)

const (
	// initialVersion is the first version of the database, before any upgrades
	// have been applied.
	initialVersion = 1

	// BoltDBVersion is the latest version of the bolt database that is
	// understood by the program. Databases with recorded versions higher than
	// this will fail to open (meaning any upgrades prevent reverting to older
	// software).
	BoltDBVersion = initialVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.
var upgrades = [...]func(tx *bolt.Tx) error{
	// Add entry here when a database upgrade is necessary.
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
