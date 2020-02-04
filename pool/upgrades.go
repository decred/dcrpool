package pool

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

const (
	initialVersion = 0

	// TransacionIDVersion is the second version of the database. It adds the
	// transactionId field to the payments struct for payment tracking purposes.
	transactionIDVersion = 1

	// DBVersion is the latest version of the database that is understood by the
	// program. Databases with recorded versions higher than this will fail to
	// open (meaning any upgrades prevent reverting to older software).
	DBVersion = transactionIDVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.
var upgrades = [...]func(tx *bolt.Tx) error{
	transactionIDVersion - 1: transactionIDUpgrade,
}

func fetchDBVersion(tx *bolt.Tx) (uint32, error) {
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return 0, MakeError(ErrBucketNotFound, desc, nil)
	}
	v := pbkt.Get(versionK)
	if v == nil {
		return 0, MakeError(ErrValueNotFound, "db version not set", nil)
	}

	return binary.LittleEndian.Uint32(v), nil
}

func setDBVersion(tx *bolt.Tx, newVersion uint32) error {
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	vBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(vBytes, newVersion)
	err := pbkt.Put(versionK, vBytes)
	if err != nil {
		return err
	}

	return nil
}

func transactionIDUpgrade(tx *bolt.Tx) error {
	const oldVersion = 0
	const newVersion = 1

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := "transactionIDUpgrade inappropriately called"
		return MakeError(ErrDBUpgrade, desc, nil)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	// Update all entries in the payment and payment archive buckets.
	//
	// All transaction ids for payments before the upgrade will be set to
	// an empty string.

	pmtbkt := pbkt.Bucket(paymentBkt)
	if pmtbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	pmtCursor := pmtbkt.Cursor()
	for k, v := pmtCursor.First(); k != nil; k, v = pmtCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			return err
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			return err
		}

		err = pmtbkt.Put(k, pBytes)
		if err != nil {
			return err
		}
	}

	abkt := pbkt.Bucket(paymentArchiveBkt)
	if abkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentArchiveBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	aCursor := abkt.Cursor()
	for k, v := aCursor.First(); k != nil; k, v = aCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			return err
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			return err
		}

		err = abkt.Put(k, pBytes)
		if err != nil {
			return err
		}
	}

	return setDBVersion(tx, newVersion)
}

// upgradeDB checks whether the any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func upgradeDB(db *bolt.DB) error {
	var version uint32
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if poolBkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}
		v := pbkt.Get(versionK)
		if v == nil {
			return MakeError(ErrValueNotFound, "db version not set", nil)
		}

		version = binary.LittleEndian.Uint32(v)

		return nil
	})

	if err != nil {
		return err
	}

	if version >= DBVersion {
		// No upgrades necessary.
		return nil
	}

	log.Infof("Upgrading database from version %d to %d", version, DBVersion)

	return db.Update(func(tx *bolt.Tx) error {
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
