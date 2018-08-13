package database

import (
	"encoding/binary"
	"fmt"
	"time"

	bolt "github.com/coreos/bbolt"
)

var (
	// poolbkt is the main bucket of mining pool, all other buckets
	// are nested within it.
	poolbkt = []byte("poolbkt")
	// accountbkt stores all registered accounts for the mining pool.
	accountbkt = []byte("accountbkt")

	// versionk is the key of the current version of the database.
	versionk = []byte("version")
)

// OpenDB creates a connection to the provided bolt storage, the returned
// connection storage should always be closed after use.
func OpenDB(storage string) (*bolt.DB, error) {
	db, err := bolt.Open(storage, 0600,
		&bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("Failed to open database: %v", err)
	}
	return db, nil
}

// CreateBuckets creates all storage buckets of the mining pool.
func CreateBuckets(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		var err error
		pbkt := tx.Bucket(poolbkt)
		if pbkt == nil {
			// Initial bucket layout creation.
			pbkt, err = tx.CreateBucketIfNotExists(poolbkt)
			if err != nil {
				return fmt.Errorf("Failed to create '%s' bucket: %v",
					string(poolbkt), err)
			}

			// Persist the database version.
			vbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(vbytes, uint32(DBVersion))
			pbkt.Put(versionk, vbytes)
		}

		_, err = pbkt.CreateBucketIfNotExists(accountbkt)
		if err != nil {
			return fmt.Errorf("Failed to create '%s' bucket: %v",
				string(accountbkt), err)
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
