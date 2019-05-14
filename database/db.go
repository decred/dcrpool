// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package database

import (
	"encoding/binary"
	"fmt"
	"time"

	bolt "github.com/coreos/bbolt"
)

var (
	// PoolBkt is the main bucket of mining pool, all other buckets
	// are nested within it.
	PoolBkt = []byte("poolbkt")

	// AccountBkt stores all registered accounts for the mining pool.
	AccountBkt = []byte("accountbkt")

	// ShareBkt stores all client shares for the mining pool.
	ShareBkt = []byte("sharebkt")

	// JobBkt stores jobs delivered to clients, it is periodically pruned by the
	// current chain tip height.
	JobBkt = []byte("jobbkt")

	// WorkBkt stores work submissions from pool clients and confirmed mined
	// work from the pool, it is periodically pruned by the current chain tip
	// adjusted by the max reorg height and by chain reorgs.
	WorkBkt = []byte("workbkt")

	// PaymentBkt stores all payments.
	PaymentBkt = []byte("paymentbkt")

	// PaymentArchiveBkt stores all processed payments for auditing purposes.
	PaymentArchiveBkt = []byte("paymentarchivebkt")

	// VersionK is the key of the current version of the database.
	VersionK = []byte("version")

	// LastPaymentCreatedOn is the key of the last time a payment was
	// persisted.
	LastPaymentCreatedOn = []byte("lastpaymentcreatedon")

	// LastPaymentPaidOn is the key of the last time a payment was
	// paid.
	LastPaymentPaidOn = []byte("lastpaymentpaidon")

	// LastPaymentHeight is the key of the last payment height.
	LastPaymentHeight = []byte("lastpaymentheight")

	// TxFeeReserve is the key of the tx fee reserve.
	TxFeeReserve = []byte("txfeereserve")

	// SoloPool is the solo pool mode key.
	SoloPool = []byte("solopool")

	// CSRFSecret is the CSRF secret key.
	CSRFSecret = []byte("csrfsecret")
)

// ErrValueNotFound is returned when a provided database key does not map
// to any value.
func ErrValueNotFound(key []byte) error {
	return fmt.Errorf("associated value for key '%v' not found", string(key))
}

// ErrBucketNotFound is returned when a provided database bucket cannot be
// found.
func ErrBucketNotFound(bucket []byte) error {
	return fmt.Errorf("bucket '%v' not found", string(bucket))
}

// OpenDB creates a connection to the provided bolt storage, the returned
// connection storage should always be closed after use.
func OpenDB(storage string) (*bolt.DB, error) {
	db, err := bolt.Open(storage, 0600,
		&bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}
	return db, nil
}

// CreateBuckets creates all storage buckets of the mining pool.
func CreateBuckets(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(PoolBkt)
		if pbkt == nil {
			// Initial bucket layout creation.
			pbkt, err = tx.CreateBucketIfNotExists(PoolBkt)
			if err != nil {
				return fmt.Errorf("failed to create '%s' bucket: %v",
					string(PoolBkt), err)
			}

			// Persist the database version.
			vbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(vbytes, uint32(DBVersion))
			err = pbkt.Put(VersionK, vbytes)
			if err != nil {
				return fmt.Errorf("failed to put version: %v", err)
			}
		}

		// Create all other buckets nested within.
		_, err = pbkt.CreateBucketIfNotExists(AccountBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(AccountBkt), err)
		}

		_, err = pbkt.CreateBucketIfNotExists(ShareBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(ShareBkt), err)
		}

		_, err = pbkt.CreateBucketIfNotExists(WorkBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(WorkBkt), err)
		}

		_, err = pbkt.CreateBucketIfNotExists(JobBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(JobBkt), err)
		}

		_, err = pbkt.CreateBucketIfNotExists(PaymentBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(PaymentBkt), err)
		}

		_, err = pbkt.CreateBucketIfNotExists(PaymentArchiveBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(PaymentArchiveBkt), err)
		}

		return nil
	})
	return err
}

// Delete removes the specified key and its associated value from the provided
// bucket.
func Delete(db *bolt.DB, bucket, key []byte) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(PoolBkt)
		if pbkt == nil {
			return ErrBucketNotFound(bucket)
		}
		b := pbkt.Bucket(bucket)
		return b.Delete(key)
	})
	return err
}

// Backup saves a copy of the db to file.
func Backup(db *bolt.DB) error {
	// Backup the db file
	err := db.View(func(tx *bolt.Tx) error {
		now := time.Now().Format(time.RFC3339)
		file := fmt.Sprintf("dcrpool_backup@%v.kv", now)
		err := tx.CopyFile(file, 0600)
		return err
	})

	return err
}

// Purge removes all existing data and recreates the db.
func Purge(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(PoolBkt)
		if pbkt == nil {
			return ErrBucketNotFound(PoolBkt)
		}

		err := pbkt.DeleteBucket(AccountBkt)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' bucket: %v",
				string(AccountBkt), err)
		}

		err = pbkt.DeleteBucket(ShareBkt)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' bucket: %v",
				string(ShareBkt), err)
		}

		err = pbkt.DeleteBucket(WorkBkt)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' bucket: %v",
				string(WorkBkt), err)
		}

		err = pbkt.DeleteBucket(JobBkt)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' bucket: %v",
				string(JobBkt), err)
		}

		err = pbkt.DeleteBucket(PaymentBkt)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' bucket: %v",
				string(PaymentBkt), err)
		}

		err = pbkt.DeleteBucket(PaymentArchiveBkt)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' bucket: %v",
				string(PaymentArchiveBkt), err)
		}

		err = pbkt.Delete(TxFeeReserve)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' k/v: %v",
				string(TxFeeReserve), err)
		}

		err = pbkt.Delete(LastPaymentHeight)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' k/v: %v",
				string(LastPaymentHeight), err)
		}

		err = pbkt.Delete(LastPaymentPaidOn)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' k/v: %v",
				string(LastPaymentPaidOn), err)
		}

		err = pbkt.Delete(LastPaymentCreatedOn)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' k/v: %v",
				string(LastPaymentCreatedOn), err)
		}

		err = pbkt.Delete(SoloPool)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' k/v: %v",
				string(SoloPool), err)
		}

		err = pbkt.Delete(CSRFSecret)
		if err != nil {
			return fmt.Errorf("failed to delete '%v' k/v: %v",
				string(CSRFSecret), err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return CreateBuckets(db)
}
