// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/binary"
	"fmt"
	"time"

	bolt "github.com/coreos/bbolt"
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

	// txFeeReserve is the key of the tx fee reserve.
	txFeeReserve = []byte("txfeereserve")

	// soloPool is the solo pool mode key.
	soloPool = []byte("solopool")

	// csrfSecret is the CSRF secret key.
	csrfSecret = []byte("csrfsecret")

	// poolFeesK is the key used to track pool fee payouts.
	poolFeesK = "fees"
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

// createBuckets creates all storage buckets of the mining pool.
func createBuckets(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			// Initial bucket layout creation.
			pbkt, err = tx.CreateBucketIfNotExists(poolBkt)
			if err != nil {
				desc := fmt.Sprintf("failed to create %s bucket", string(poolBkt))
				return MakeError(ErrBucketCreate, desc, err)
			}

			// Persist the database version.
			vbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(vbytes, uint32(DBVersion))
			err = pbkt.Put(versionK, vbytes)
			if err != nil {
				return err
			}
		}

		// Create all other buckets nested within.
		_, err = pbkt.CreateBucketIfNotExists(accountBkt)
		if err != nil {
			desc := fmt.Sprintf("failed to create %s bucket", string(accountBkt))
			return MakeError(ErrBucketCreate, desc, err)
		}

		_, err = pbkt.CreateBucketIfNotExists(shareBkt)
		if err != nil {
			desc := fmt.Sprintf("failed to create %s bucket", string(shareBkt))
			return MakeError(ErrBucketCreate, desc, err)
		}

		_, err = pbkt.CreateBucketIfNotExists(workBkt)
		if err != nil {
			desc := fmt.Sprintf("failed to create %s bucket", string(workBkt))
			return MakeError(ErrBucketCreate, desc, err)
		}

		_, err = pbkt.CreateBucketIfNotExists(jobBkt)
		if err != nil {
			desc := fmt.Sprintf("failed to create %s bucket", string(jobBkt))
			return MakeError(ErrBucketCreate, desc, err)
		}

		_, err = pbkt.CreateBucketIfNotExists(paymentBkt)
		if err != nil {
			desc := fmt.Sprintf("failed to create %s bucket", string(paymentBkt))
			return MakeError(ErrBucketCreate, desc, err)
		}

		_, err = pbkt.CreateBucketIfNotExists(paymentArchiveBkt)
		if err != nil {
			desc := fmt.Sprintf("failed to create %s bucket", string(paymentArchiveBkt))
			return MakeError(ErrBucketCreate, desc, err)
		}

		return nil
	})
	return err
}

// backup saves a copy of the db to file.
func backup(db *bolt.DB, file string) error {
	// Backup the db file
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

		err = pbkt.Delete(txFeeReserve)
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

		err = pbkt.Delete(csrfSecret)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	return createBuckets(db)
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
