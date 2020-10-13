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
	// Confirmed processed payments are sourced from the payment bucket and
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
	// PoolFeesK is the key used to track pool fee payouts.
	PoolFeesK = "fees"
	// backup is the database backup file name.
	backupFile = "backup.kv"
)

// openDB creates a connection to the provided bolt storage, the returned
// connection storage should always be closed after use.
func openDB(storage string) (*bolt.DB, error) {
	const funcName = "openDB"
	db, err := bolt.Open(storage, 0600,
		&bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		desc := fmt.Sprintf("%s: unable to open db file: %v", funcName, err)
		return nil, dbError(ErrDBOpen, desc)
	}
	return db, nil
}

// createNestedBucket creates a nested child bucket of the provided parent.
func createNestedBucket(parent *bolt.Bucket, child []byte) error {
	const funcName = "createNestedBucket"
	_, err := parent.CreateBucketIfNotExists(child)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to create %s bucket: %v",
			funcName, string(child), err)
		return dbError(ErrBucketCreate, desc)
	}
	return nil
}

// createBuckets creates all storage buckets of the mining pool.
func createBuckets(db *bolt.DB) error {
	const funcName = "createBuckets"
	err := db.Update(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			pbkt, err = tx.CreateBucketIfNotExists(poolBkt)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to create %s bucket: %v",
					funcName, string(poolBkt), err)
				return dbError(ErrBucketCreate, desc)
			}
			vbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(vbytes, uint32(DBVersion))
			err = pbkt.Put(versionK, vbytes)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to persist version: %v",
					funcName, err)
				return dbError(ErrPersistEntry, desc)
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
	return db.View(func(tx *bolt.Tx) error {
		err := tx.CopyFile(file, 0600)
		if err != nil {
			desc := fmt.Sprintf("unable to backup db: %v", err)
			return poolError(ErrBackup, desc)
		}
		return nil
	})
}

// purge removes all existing data and recreates the db.
func purge(db *bolt.DB) error {
	const funcName = "purge"
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found",
				funcName, string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
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
		return nil, err
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
func deleteEntry(db *bolt.DB, bucket []byte, key string) error {
	const funcName = "deleteEntry"
	return db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("%s: bucket %s not found", funcName,
				string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}
		b := pbkt.Bucket(bucket)

		err := b.Delete([]byte(key))
		if err != nil {
			desc := fmt.Sprintf("%s: unable to delete entry with "+
				"key %s from bucket %s", funcName, key, string(poolBkt))
			return dbError(ErrDeleteEntry, desc)
		}
		return nil
	})
}

// fetchBucket is a helper function for getting the requested bucket.
func fetchBucket(tx *bolt.Tx, bucketID []byte) (*bolt.Bucket, error) {
	const funcName = "fetchBucket"
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return nil, dbError(ErrBucketNotFound, desc)
	}
	bkt := pbkt.Bucket(bucketID)
	if bkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(bucketID))
		return nil, dbError(ErrBucketNotFound, desc)
	}
	return bkt, nil
}

func persistPoolMode(db *bolt.DB, mode uint32) error {
	return db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, mode)
		return pbkt.Put(soloPool, b)
	})
}

func fetchCSRFSecret(db *bolt.DB) ([]byte, error) {
	var secret []byte

	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}
		v := pbkt.Get(csrfSecret)
		if v == nil {
			return dbError(ErrValueNotFound, "No csrf secret found")
		}

		// Byte slices returned from Bolt are only valid during a transaction.
		// Need to make a copy.
		secret = make([]byte, len(v))
		copy(secret, v)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return secret, nil
}

func persistCSRFSecret(db *bolt.DB, secret []byte) error {
	return db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		return pbkt.Put(csrfSecret, secret)
	})
}

func persistLastPaymentInfo(db *bolt.DB, height uint32, paidOn int64) error {
	funcName := "persistLastPaymentInfo"
	return db.Update(func(tx *bolt.Tx) error {
		pbkt, err := fetchPoolBucket(tx)
		if err != nil {
			return err
		}

		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, height)
		err = pbkt.Put(lastPaymentHeight, b)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist last payment height: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}

		err = pbkt.Put(lastPaymentPaidOn, nanoToBigEndianBytes(paidOn))
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist last payment "+
				"paid on time: %v", funcName, err)
			return dbError(ErrPersistEntry, desc)
		}

		return nil
	})
}

func loadLastPaymentInfo(db *bolt.DB) (uint32, uint64, error) {
	funcName := "loadLastPaymentInfo"
	var height uint32
	var paidOn uint64
	err := db.View(func(tx *bolt.Tx) error {
		pbkt, err := fetchPoolBucket(tx)
		if err != nil {
			return err
		}

		lastPaymentHeightB := pbkt.Get(lastPaymentHeight)
		if lastPaymentHeightB == nil {
			desc := fmt.Sprintf("%s: last payment height not initialized", funcName)
			return dbError(ErrFetchEntry, desc)
		}
		height = binary.LittleEndian.Uint32(lastPaymentHeightB)

		lastPaymentPaidOnB := pbkt.Get(lastPaymentPaidOn)
		if lastPaymentPaidOnB == nil {
			desc := fmt.Sprintf("%s: last payment paid-on not initialized", funcName)
			return dbError(ErrFetchEntry, desc)
		}
		paidOn = bigEndianBytesToNano(lastPaymentPaidOnB)

		return nil
	})

	if err != nil {
		return 0, 0, err
	}

	return height, paidOn, nil
}

func persistLastPaymentCreatedOn(db *bolt.DB, createdOn int64) error {
	funcName := "persistLastPaymentCreatedOn"
	return db.Update(func(tx *bolt.Tx) error {
		pbkt, err := fetchPoolBucket(tx)
		if err != nil {
			return err
		}
		err = pbkt.Put(lastPaymentCreatedOn, nanoToBigEndianBytes(createdOn))
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist last payment "+
				"created-on time: %v", funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

func loadLastPaymentCreatedOn(db *bolt.DB) (uint64, error) {
	funcName := "loadLastPaymentCreatedOn"
	var createdOn uint64
	err := db.View(func(tx *bolt.Tx) error {
		pbkt, err := fetchPoolBucket(tx)
		if err != nil {
			return err
		}
		lastPaymentCreatedOnB := pbkt.Get(lastPaymentCreatedOn)
		if lastPaymentCreatedOnB == nil {
			desc := fmt.Sprintf("%s: last payment created-on not initialized",
				funcName)
			return dbError(ErrFetchEntry, desc)
		}
		createdOn = bigEndianBytesToNano(lastPaymentCreatedOnB)
		return nil
	})

	if err != nil {
		return 0, err
	}

	return createdOn, nil
}
