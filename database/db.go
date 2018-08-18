package database

import (
	"bytes"
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
	// UsernameIdxBkt is an index of all account emails.
	UsernameIdxBkt = []byte("usernameidxbkt")

	// VersionK is the key of the current version of the database.
	VersionK = []byte("version")
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
			pbkt.Put(VersionK, vbytes)
		}

		_, err = pbkt.CreateBucketIfNotExists(AccountBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(AccountBkt), err)
		}

		_, err = pbkt.CreateBucketIfNotExists(UsernameIdxBkt)
		if err != nil {
			return fmt.Errorf("failed to create '%v' bucket: %v",
				string(UsernameIdxBkt), err)
		}
		return nil
	})
	return err
}

// Delete removes the specified key and its associated value from the provided
// bucket.
func Delete(db *bolt.DB, bucket, key []byte) error {
	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucket)
		return b.Delete(key)
	})
	return err
}

// IndexExists asserts if a an index value exists in the provided bucket.
func IndexExists(db *bolt.DB, bucket, value []byte) (bool, error) {
	var exists bool
	err := db.View(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucket)
		if bkt == nil {
			return ErrBucketNotFound(bucket)
		}
		c := bkt.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			if bytes.Contains(value, v) {
				exists = true
				break
			}
		}
		return nil
	})
	return exists, err
}

// UpdateIndex updates an index entry in the provided bucket.
func UpdateIndex(db *bolt.DB, bucket []byte, key []byte, value []byte) error {
	// Update the username index.
	err := db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket(bucket)
		if bkt == nil {
			return ErrBucketNotFound(bucket)
		}
		err := bkt.Put(key, value)
		return err
	})
	return err
}
