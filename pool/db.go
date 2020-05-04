// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	// poolBkt is the main bucket of mining pool, all other buckets
	// are nested within it.
	poolBkt = []byte("poolbkt")
	// accountBkt stores all registered accounts for the mining pool.
	accountBkt = []byte("accountbkt")
	// shareBkt stores all client shares for the mining pool.
	shareBkt = []byte("sharebkt")
	// jobBkt stores jobs delivered to clients, it is periodically pruned by the
	// current chain tip height.
	jobBkt = []byte("jobbkt")
	// workBkt stores work submissions from pool clients and confirmed mined
	// work from the pool, it is periodically pruned by the current chain tip
	// adjusted by the max reorg height and by chain reorgs.
	workBkt = []byte("workbkt")
	// paymentBkt stores all payments. Confirmed processed payments are
	// archived periodically.
	paymentBkt = []byte("paymentbkt")
	// paymentArchiveBkt stores all processed payments for auditing purposes.
	// Confirmed processed payements are sourced from the payment bucket and
	// archived.
	paymentArchiveBkt = []byte("paymentarchivebkt")
	// versionK is the key of the current version of the database.
	versionK = []byte("version")
	// lastPaymentCreatedOn is the key of the last time a payment was
	// persisted.
	lastPaymentCreatedOn = []byte("lastpaymentcreatedon")
	// lastPaymentPaidOn is the key of the last time a payment was
	// paid.
	lastPaymentPaidOn = []byte("lastpaymentpaidon")
	// lastPaymentHeight is the key of the last payment height.
	lastPaymentHeight = []byte("lastpaymentheight")
	// soloPool is the solo pool mode key.
	soloPool = []byte("solopool")
	// csrfSecret is the CSRF secret key.
	csrfSecret = []byte("csrfsecret")
	// poolFeesK is the key used to track pool fee payouts.
	poolFeesK = "fees"
	// backup is the database backup file name.
	backupFile = "backup.kv"
)

// openDB creates a connection to the provided bolt storage, the returned
// connection storage should always be closed after use.
func openDB(storage string) (*bolt.DB, error) {
	db, err := bolt.Open(storage, 0600,
		&bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, MakeError(ErrDBOpen, "", err)
	}
	return db, nil
}

// createNestedBucket creates a nested child bucket of the provided parent.
func createNestedBucket(parent *bolt.Bucket, child []byte) error {
	_, err := parent.CreateBucketIfNotExists(child)
	if err != nil {
		desc := fmt.Sprintf("failed to create %s bucket", string(child))
		return MakeError(ErrBucketCreate, desc, err)
	}
	return nil
}

// createBuckets creates all storage buckets of the mining pool.
func createBuckets(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			pbkt, err = tx.CreateBucketIfNotExists(poolBkt)
			if err != nil {
				desc := fmt.Sprintf("failed to create %s bucket", string(poolBkt))
				return MakeError(ErrBucketCreate, desc, err)
			}
			vbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(vbytes, uint32(DBVersion))
			err = pbkt.Put(versionK, vbytes)
			if err != nil {
				return err
			}
		}

		err = createNestedBucket(pbkt, accountBkt)
		if err != nil {
			return err
		}
		err = createNestedBucket(pbkt, shareBkt)
		if err != nil {
			return err
		}
		err = createNestedBucket(pbkt, workBkt)
		if err != nil {
			return err
		}
		err = createNestedBucket(pbkt, jobBkt)
		if err != nil {
			return err
		}
		err = createNestedBucket(pbkt, paymentBkt)
		if err != nil {
			return err
		}
		return createNestedBucket(pbkt, paymentArchiveBkt)
	})
	return err
}

// backup saves a copy of the db to file.
func backup(db *bolt.DB, file string) error {
	err := db.View(func(tx *bolt.Tx) error {
		err := tx.CopyFile(file, 0600)
		return err
	})
	return err
}

// purge removes all existing data and recreates the db.
func purge(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}
		err := pbkt.DeleteBucket(accountBkt)
		if err != nil {
			return err
		}
		err = pbkt.DeleteBucket(shareBkt)
		if err != nil {
			return err
		}
		err = pbkt.DeleteBucket(workBkt)
		if err != nil {
			return err
		}
		err = pbkt.DeleteBucket(jobBkt)
		if err != nil {
			return err
		}
		err = pbkt.DeleteBucket(paymentBkt)
		if err != nil {
			return err
		}
		err = pbkt.DeleteBucket(paymentArchiveBkt)
		if err != nil {
			return err
		}
		err = pbkt.Delete(lastPaymentHeight)
		if err != nil {
			return err
		}
		err = pbkt.Delete(lastPaymentPaidOn)
		if err != nil {
			return err
		}
		err = pbkt.Delete(lastPaymentCreatedOn)
		if err != nil {
			return err
		}
		err = pbkt.Delete(soloPool)
		if err != nil {
			return err
		}
		return pbkt.Delete(csrfSecret)
	})
	if err != nil {
		return err
	}
	return createBuckets(db)
}

// InitDB handles the creation, upgrading and backup of the pool database.
func InitDB(dbFile string, isSoloPool bool) (*bolt.DB, error) {
	db, err := openDB(dbFile)
	if err != nil {
		return nil, MakeError(ErrDBOpen, "unable to open db file", err)
	}
	err = createBuckets(db)
	if err != nil {
		return nil, err
	}
	err = upgradeDB(db)
	if err != nil {
		return nil, err
	}

	var switchMode bool
	err = db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return err
		}
		v := pbkt.Get(soloPool)
		if v == nil {
			return nil
		}
		spMode := binary.LittleEndian.Uint32(v) == 1
		if isSoloPool != spMode {
			switchMode = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if switchMode {
		// Backup the current database and wipe it.
		backupPath := filepath.Join(filepath.Dir(db.Path()), backupFile)
		err := backup(db, backupPath)
		if err != nil {
			return nil, err
		}
		log.Infof("Pool mode changed, database backup created.")
		err = purge(db)
		if err != nil {
			return nil, err
		}
		log.Infof("Database wiped.")
	}
	return db, nil
}

// deleteEntry removes the specified key and its associated value from
// the provided bucket.
func deleteEntry(db *bolt.DB, bucket, key []byte) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}
		b := pbkt.Bucket(bucket)
		return b.Delete(key)
	})
	return err
}

// emptyBucket deletes all k/v pairs in the provided bucket.
func emptyBucket(db *bolt.DB, bucket []byte) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}
		b := pbkt.Bucket(bucket)
		toDelete := [][]byte{}
		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			toDelete = append(toDelete, k)
		}
		for _, k := range toDelete {
			err := b.Delete(k)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return err
}
