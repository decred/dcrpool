// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
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
	// BoltBackupFile is the database backup file name.
	BoltBackupFile = "backup.kv"
)

// openBoltDB creates a connection to the provided bolt storage, the returned
// connection storage should always be closed after use.
func openBoltDB(storage string) (*BoltDB, error) {
	const funcName = "openBoltDB"
	db, err := bolt.Open(storage, 0600,
		&bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		desc := fmt.Sprintf("%s: unable to open db file: %v", funcName, err)
		return nil, dbError(ErrDBOpen, desc)
	}
	return &BoltDB{db}, nil
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
func createBuckets(db *BoltDB) error {
	const funcName = "createBuckets"
	err := db.DB.Update(func(tx *bolt.Tx) error {
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

// Backup saves a copy of the db to file. The file will be saved in the same
// directory as the current db file.
func (db *BoltDB) Backup(backupFileName string) error {
	backupPath := filepath.Join(filepath.Dir(db.DB.Path()), backupFileName)
	return db.DB.View(func(tx *bolt.Tx) error {
		err := tx.CopyFile(backupPath, 0600)
		if err != nil {
			desc := fmt.Sprintf("unable to backup db: %v", err)
			return poolError(ErrBackup, desc)
		}
		return nil
	})
}

// InitBoltDB handles the creation and upgrading of a bolt database.
func InitBoltDB(dbFile string) (*BoltDB, error) {
	db, err := openBoltDB(dbFile)
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

	return db, nil
}

// deleteEntry removes the specified key and its associated value from
// the provided bucket.
func deleteEntry(db *BoltDB, bucket []byte, key string) error {
	const funcName = "deleteEntry"
	return db.DB.Update(func(tx *bolt.Tx) error {
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
	pbkt, err := fetchPoolBucket(tx)
	if err != nil {
		return nil, err
	}
	bkt := pbkt.Bucket(bucketID)
	if bkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(bucketID))
		return nil, dbError(ErrBucketNotFound, desc)
	}
	return bkt, nil
}

// fetchPoolBucket is a helper function for getting the pool bucket.
func fetchPoolBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	funcName := "fetchPoolBucket"
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return nil, dbError(ErrBucketNotFound, desc)
	}
	return pbkt, nil
}

// bigEndianBytesToNano returns nanosecond time from the provided
// big endian bytes.
func bigEndianBytesToNano(b []byte) uint64 {
	return binary.BigEndian.Uint64(b)
}

// nanoToBigEndianBytes returns an 8-byte big endian representation of
// the provided nanosecond time.
func nanoToBigEndianBytes(nano int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(nano))
	return b
}

// fetchPoolMode retrives the pool mode from the database. PoolMode is stored as
// a uint32 for historical reasons. 0 indicates Public, 1 indicates Solo.
func (db *BoltDB) fetchPoolMode() (uint32, error) {
	var mode uint32
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt, err := fetchPoolBucket(tx)
		if err != nil {
			return err
		}
		b := pbkt.Get(soloPool)
		if b == nil {
			return dbError(ErrValueNotFound, "no pool mode found")
		}
		mode = binary.LittleEndian.Uint32(b)
		return nil
	})
	if err != nil {
		return 0, err
	}

	return mode, nil
}

// persistPoolMode stores the pool mode in the database. PoolMode is stored as a
// uint32 for historical reasons. 0 indicates Public, 1 indicates Solo.
func (db *BoltDB) persistPoolMode(mode uint32) error {

	return db.DB.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, mode)
		return pbkt.Put(soloPool, b)
	})
}

// fetchCSRFSecret retrieves the bytes used for the CSRF secret from the database.
func (db *BoltDB) fetchCSRFSecret() ([]byte, error) {
	var secret []byte

	err := db.DB.View(func(tx *bolt.Tx) error {
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

// persistCSRFSecret stores the bytes used for the CSRF secret in the database.
func (db *BoltDB) persistCSRFSecret(secret []byte) error {
	return db.DB.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return dbError(ErrBucketNotFound, desc)
		}

		return pbkt.Put(csrfSecret, secret)
	})
}

// persistLastPaymentInfo stores the last payment height and paidOn timestamp
// in the database.
func (db *BoltDB) persistLastPaymentInfo(height uint32, paidOn int64) error {
	funcName := "persistLastPaymentInfo"
	return db.DB.Update(func(tx *bolt.Tx) error {
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

// loadLastPaymentInfo retrieves the last payment height and paidOn timestamp
// from the database.
func (db *BoltDB) loadLastPaymentInfo() (uint32, int64, error) {
	funcName := "loadLastPaymentInfo"
	var height uint32
	var paidOn int64
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt, err := fetchPoolBucket(tx)
		if err != nil {
			return err
		}

		lastPaymentHeightB := pbkt.Get(lastPaymentHeight)
		lastPaymentPaidOnB := pbkt.Get(lastPaymentPaidOn)

		if lastPaymentHeightB == nil || lastPaymentPaidOnB == nil {
			desc := fmt.Sprintf("%s: last payment info not initialized", funcName)
			return dbError(ErrValueNotFound, desc)
		}

		height = binary.LittleEndian.Uint32(lastPaymentHeightB)
		paidOn = int64(bigEndianBytesToNano(lastPaymentPaidOnB))

		return nil
	})

	if err != nil {
		return 0, 0, err
	}

	return height, paidOn, nil
}

// persistLastPaymentCreatedOn stores the last payment createdOn timestamp in
// the database.
func (db *BoltDB) persistLastPaymentCreatedOn(createdOn int64) error {
	funcName := "persistLastPaymentCreatedOn"
	return db.DB.Update(func(tx *bolt.Tx) error {
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

// loadLastPaymentCreatedOn retrieves the last payment createdOn timestamp from
// the database.
func (db *BoltDB) loadLastPaymentCreatedOn() (int64, error) {
	funcName := "loadLastPaymentCreatedOn"
	var createdOn int64
	err := db.DB.View(func(tx *bolt.Tx) error {
		pbkt, err := fetchPoolBucket(tx)
		if err != nil {
			return err
		}
		lastPaymentCreatedOnB := pbkt.Get(lastPaymentCreatedOn)
		if lastPaymentCreatedOnB == nil {
			desc := fmt.Sprintf("%s: last payment created-on not initialized",
				funcName)
			return dbError(ErrValueNotFound, desc)
		}
		createdOn = int64(bigEndianBytesToNano(lastPaymentCreatedOnB))
		return nil
	})

	if err != nil {
		return 0, err
	}

	return createdOn, nil
}

// Close closes the Bolt database.
func (db *BoltDB) Close() error {
	return db.DB.Close()
}

// httpBackup streams a backup of the entire database over the provided HTTP
// response writer.
func (db *BoltDB) httpBackup(w http.ResponseWriter) error {
	err := db.DB.View(func(tx *bolt.Tx) error {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="backup.db"`)
		w.Header().Set("Content-Length", strconv.Itoa(int(tx.Size())))
		_, err := tx.WriteTo(w)
		return err
	})
	return err
}
