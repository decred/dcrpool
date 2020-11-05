// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
			binary.LittleEndian.PutUint32(vbytes, LatestBoltDBVersion)
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

// fetchAcceptedWork fetches the accepted work referenced by the provided id.
// Returns an error if the work is not found.
func (db *BoltDB) fetchAcceptedWork(id string) (*AcceptedWork, error) {
	const funcName = "fetchAcceptedWork"
	var work AcceptedWork
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}
		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no value for key %s", funcName, id)
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &work)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal accepted work: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &work, err
}

// persistAcceptedWork saves the accepted work to the database.
func (db *BoltDB) persistAcceptedWork(work *AcceptedWork) error {
	const funcName = "persistAcceptedWork"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		// Do not persist already existing accepted work.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v != nil {
			desc := fmt.Sprintf("%s: work %s already exists", funcName,
				work.UUID)
			return dbError(ErrValueFound, desc)
		}
		workBytes, err := json.Marshal(work)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal accepted "+
				"work bytes: %v", funcName, err)
			return dbError(ErrParse, desc)
		}

		err = bkt.Put(id, workBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist accepted work: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// updateAcceptedWork persists modifications to an existing work. Returns an
// error if the work is not found.
func (db *BoltDB) updateAcceptedWork(work *AcceptedWork) error {
	const funcName = "updateAcceptedWork"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		// Assert the work provided exists before updating.
		id := []byte(work.UUID)
		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("%s: work %s not found", funcName, work.UUID)
			return dbError(ErrValueNotFound, desc)
		}
		workBytes, err := json.Marshal(work)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal accepted "+
				"work bytes: %v", funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		err = bkt.Put(id, workBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist accepted work: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// deleteAcceptedWork removes the associated accepted work from the database.
func (db *BoltDB) deleteAcceptedWork(id string) error {
	return deleteEntry(db, workBkt, id)
}

// listMinedWork returns work data associated with all blocks mined by the pool
// regardless of whether they are confirmed or not.
//
// List is ordered, most recent comes first.
func (db *BoltDB) listMinedWork() ([]*AcceptedWork, error) {
	const funcName = "listMinedWork"
	minedWork := make([]*AcceptedWork, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.Last(); k != nil; k, v = cursor.Prev() {
			var work AcceptedWork
			err := json.Unmarshal(v, &work)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal accepted "+
					"work: %v", funcName, err)
				return poolError(ErrParse, desc)
			}
			minedWork = append(minedWork, &work)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return minedWork, nil
}

// fetchUnconfirmedWork returns all work which is not confirmed as mined with
// height less than the provided height.
func (db *BoltDB) fetchUnconfirmedWork(height uint32) ([]*AcceptedWork, error) {
	toReturn := make([]*AcceptedWork, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, workBkt)
		if err != nil {
			return err
		}

		heightBE := heightToBigEndianBytes(height)
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			heightB, err := hex.DecodeString(string(k[:8]))
			if err != nil {
				return err
			}

			if bytes.Compare(heightBE, heightB) > 0 {
				var work AcceptedWork
				err := json.Unmarshal(v, &work)
				if err != nil {
					return err
				}

				if !work.Confirmed {
					toReturn = append(toReturn, &work)
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return toReturn, nil
}

// fetchAccount fetches the account referenced by the provided id. Returns
// an error if the account is not found.
func (db *BoltDB) fetchAccount(id string) (*Account, error) {
	const funcName = "fetchAccount"
	var account Account
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, accountBkt)
		if err != nil {
			return err
		}

		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no account found for id %s", funcName, id)
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &account)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal account: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &account, err
}

// persistAccount saves the account to the database. Before persisting the
// account, it sets the createdOn timestamp. Returns an error if an account
// already exists with the same ID.
func (db *BoltDB) persistAccount(acc *Account) error {
	const funcName = "persistAccount"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, accountBkt)
		if err != nil {
			return err
		}

		// Do not persist already existing account.
		if bkt.Get([]byte(acc.UUID)) != nil {
			desc := fmt.Sprintf("%s: account %s already exists", funcName,
				acc.UUID)
			return dbError(ErrValueFound, desc)
		}

		acc.CreatedOn = uint64(time.Now().Unix())

		accBytes, err := json.Marshal(acc)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal account bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		err = bkt.Put([]byte(acc.UUID), accBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist account entry: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// deleteAccount purges the referenced account from the database.
func (db *BoltDB) deleteAccount(id string) error {
	return deleteEntry(db, accountBkt, id)
}

// fetchJob fetches the job referenced by the provided id. Returns an error if
// the job is not found.
func (db *BoltDB) fetchJob(id string) (*Job, error) {
	const funcName = "fetchJob"
	var job Job
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, jobBkt)
		if err != nil {
			return err
		}

		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no job found for id %s", funcName, id)
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &job)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal job bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &job, err
}

// persistJob saves the job to the database. Returns an error if an account
// already exists with the same ID.
func (db *BoltDB) persistJob(job *Job) error {
	const funcName = "persistJob"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, jobBkt)
		if err != nil {
			return err
		}

		// Do not persist already existing job.
		if bkt.Get([]byte(job.UUID)) != nil {
			desc := fmt.Sprintf("%s: job %s already exists", funcName,
				job.UUID)
			return dbError(ErrValueFound, desc)
		}

		jobBytes, err := json.Marshal(job)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal job bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		err = bkt.Put([]byte(job.UUID), jobBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist job entry: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// deleteJob removes the associated job from the database.
func (db *BoltDB) deleteJob(id string) error {
	return deleteEntry(db, jobBkt, id)
}

// deleteJobsBeforeHeight removes all jobs with heights less than the provided
// height.
func (db *BoltDB) deleteJobsBeforeHeight(height uint32) error {
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, jobBkt)
		if err != nil {
			return err
		}

		heightBE := heightToBigEndianBytes(height)
		toDelete := [][]byte{}
		c := bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			heightB, err := hex.DecodeString(string(k[:8]))
			if err != nil {
				return err
			}

			if bytes.Compare(heightB, heightBE) < 0 {
				toDelete = append(toDelete, k)
			}
		}

		for _, entry := range toDelete {
			err := bkt.Delete(entry)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// fetchPayment fetches the payment referenced by the provided id. Returns an
// error if the payment is not found.
func (db *BoltDB) fetchPayment(id string) (*Payment, error) {
	const funcName = "fetchPayment"
	var payment Payment
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no payment found for id %s", funcName, id)
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &payment, err
}

// PersistPayment saves a payment to the database.
func (db *BoltDB) PersistPayment(pmt *Payment) error {
	const funcName = "PersistPayment"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		b, err := json.Marshal(pmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		err = bkt.Put([]byte(pmt.UUID), b)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment bytes: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// updatePayment persists the updated payment to the database.
func (db *BoltDB) updatePayment(pmt *Payment) error {
	return db.PersistPayment(pmt)
}

// deletePayment purges the referenced payment from the database. Note that
// archived payments cannot be deleted.
func (db *BoltDB) deletePayment(id string) error {
	return deleteEntry(db, paymentBkt, id)
}

// ArchivePayment removes the associated payment from active payments and archives it.
func (db *BoltDB) ArchivePayment(pmt *Payment) error {
	const funcName = "ArchivePayment"
	return db.DB.Update(func(tx *bolt.Tx) error {
		pbkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		abkt, err := fetchBucket(tx, paymentArchiveBkt)
		if err != nil {
			return err
		}

		// Remove the active payment record.
		err = pbkt.Delete([]byte(pmt.UUID))
		if err != nil {
			return err
		}

		// Create a new payment to add to the archive.
		aPmt := NewPayment(pmt.Account, pmt.Source, pmt.Amount, pmt.Height,
			pmt.EstimatedMaturity)
		aPmtB, err := json.Marshal(aPmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		err = abkt.Put([]byte(aPmt.UUID), aPmtB)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to archive payment entry: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// fetchPaymentsAtHeight returns all payments sourcing from orphaned blocks at
// the provided height.
func (db *BoltDB) fetchPaymentsAtHeight(height uint32) ([]*Payment, error) {
	toReturn := make([]*Payment, 0)
	err := db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				return err
			}

			if payment.PaidOnHeight == 0 {
				// If a payment is spendable but does not get processed it
				// becomes eligible for pruning.
				spendableHeight := payment.EstimatedMaturity + 1
				if height > spendableHeight {
					toReturn = append(toReturn, &payment)
				}
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return toReturn, nil
}

// fetchPendingPayments fetches all unpaid payments.
func (db *BoltDB) fetchPendingPayments() ([]*Payment, error) {
	funcName := "fetchPendingPayments"
	payments := make([]*Payment, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}
		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal "+
					"payment: %v", funcName, err)
				return dbError(ErrParse, desc)
			}
			if payment.PaidOnHeight == 0 {
				payments = append(payments, &payment)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return payments, nil
}

// pendingPaymentsForBlockHash returns the number of pending payments with the
// provided block hash as their source.
func (db *BoltDB) pendingPaymentsForBlockHash(blockHash string) (uint32, error) {
	funcName := "pendingPaymentsForBlockHash"
	var count uint32
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
					funcName, err)
				return dbError(ErrParse, desc)
			}
			if payment.PaidOnHeight == 0 {
				if payment.Source.BlockHash == blockHash {
					count++
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// archivedPayments fetches all archived payments. List is ordered, most
// recent comes first.
func (db *BoltDB) archivedPayments() ([]*Payment, error) {
	funcName := "archivedPayments"
	pmts := make([]*Payment, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		abkt, err := fetchBucket(tx, paymentArchiveBkt)
		if err != nil {
			return err
		}

		c := abkt.Cursor()
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
					funcName, err)
				return dbError(ErrParse, desc)
			}
			pmts = append(pmts, &payment)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pmts, nil
}

// maturePendingPayments fetches all mature pending payments at the
// provided height.
func (db *BoltDB) maturePendingPayments(height uint32) (map[string][]*Payment, error) {
	funcName := "maturePendingPayments"
	payments := make([]*Payment, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, paymentBkt)
		if err != nil {
			return err
		}

		cursor := bkt.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var payment Payment
			err := json.Unmarshal(v, &payment)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
					funcName, err)
				return dbError(ErrParse, desc)
			}
			spendableHeight := payment.EstimatedMaturity + 1
			if payment.PaidOnHeight == 0 && spendableHeight <= height {
				payments = append(payments, &payment)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	pmts := make(map[string][]*Payment)
	for _, pmt := range payments {
		set, ok := pmts[pmt.Source.BlockHash]
		if !ok {
			set = make([]*Payment, 0)
		}
		set = append(set, pmt)
		pmts[pmt.Source.BlockHash] = set
	}
	return pmts, nil
}

// fetchShare fetches the share referenced by the provided id. Returns an error
// if the share is not found.
func (db *BoltDB) fetchShare(id string) (*Share, error) {
	const funcName = "fetchShare"
	var share Share
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}

		v := bkt.Get([]byte(id))
		if v == nil {
			desc := fmt.Sprintf("%s: no share found for id %s", funcName, id)
			return dbError(ErrValueNotFound, desc)
		}
		err = json.Unmarshal(v, &share)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal share bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &share, err
}

// PersistShare saves a share to the database. Returns an error if a share
// already exists with the same ID.
func (db *BoltDB) PersistShare(s *Share) error {
	const funcName = "PersistShare"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}

		// Do not persist already existing share.
		if bkt.Get([]byte(s.UUID)) != nil {
			desc := fmt.Sprintf("%s: share %s already exists", funcName,
				s.UUID)
			return dbError(ErrValueFound, desc)
		}

		sBytes, err := json.Marshal(s)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal share bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}
		err = bkt.Put([]byte(s.UUID), sBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist share entry: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
		return nil
	})
}

// ppsEligibleShares fetches all shares created before or at the provided time.
func (db *BoltDB) ppsEligibleShares(max int64) ([]*Share, error) {
	funcName := "ppsEligibleShares"
	eligibleShares := make([]*Share, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		c := bkt.Cursor()
		createdOnB := make([]byte, 8)
		maxB := nanoToBigEndianBytes(max)
		for k, v := c.First(); k != nil; k, v = c.Next() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				desc := fmt.Sprintf("%s: unable to decode share "+
					"created-on bytes: %v", funcName, err)
				return dbError(ErrDecode, desc)
			}

			if bytes.Compare(createdOnB, maxB) <= 0 {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					desc := fmt.Sprintf("%s: unable to unmarshal share: %v",
						funcName, err)
					return dbError(ErrParse, desc)
				}
				eligibleShares = append(eligibleShares, &share)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return eligibleShares, err
}

// pplnsEligibleShares fetches all shares created after the provided time.
func (db *BoltDB) pplnsEligibleShares(min int64) ([]*Share, error) {
	funcName := "pplnsEligibleShares"
	eligibleShares := make([]*Share, 0)
	err := db.DB.View(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		c := bkt.Cursor()
		createdOnB := make([]byte, 8)
		minB := nanoToBigEndianBytes(min)
		for k, v := c.Last(); k != nil; k, v = c.Prev() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				desc := fmt.Sprintf("%s: unable to decode share "+
					"created-on bytes: %v", funcName, err)
				return dbError(ErrDecode, desc)
			}

			if bytes.Compare(createdOnB, minB) > 0 {
				var share Share
				err := json.Unmarshal(v, &share)
				if err != nil {
					desc := fmt.Sprintf("%s: unable to unmarshal "+
						"share: %v", funcName, err)
					return dbError(ErrParse, desc)
				}
				eligibleShares = append(eligibleShares, &share)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return eligibleShares, err
}

// pruneShares removes shares with a createdOn time earlier than the provided
// time.
func (db *BoltDB) pruneShares(minNano int64) error {
	funcName := "pruneShares"
	minB := nanoToBigEndianBytes(minNano)

	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, shareBkt)
		if err != nil {
			return err
		}
		toDelete := [][]byte{}
		cursor := bkt.Cursor()
		createdOnB := make([]byte, 8)
		for k, _ := cursor.First(); k != nil; k, _ = cursor.Next() {
			_, err := hex.Decode(createdOnB, k[:16])
			if err != nil {
				desc := fmt.Sprintf("%s: unable to decode share created-on "+
					"bytes: %v", funcName, err)
				return dbError(ErrDecode, desc)
			}
			if bytes.Compare(minB, createdOnB) > 0 {
				toDelete = append(toDelete, k)
			}
		}
		for _, entry := range toDelete {
			err := bkt.Delete(entry)
			if err != nil {
				return err
			}
		}
		return nil
	})
}
