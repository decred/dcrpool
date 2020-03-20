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
	"time"

	bolt "go.etcd.io/bbolt"
)

// Job represents cached copies of work delivered to clients.
type Job struct {
	UUID   string `json:"uuid"`
	Height uint32 `json:"height"`
	Header string `json:"header"`
}

// nanoToBigEndianBytes returns an 8-byte big endian representation of
// the provided nanosecond time.
func nanoToBigEndianBytes(nano int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(nano))
	return b
}

// GenerateJobID generates a unique job id of the provided block height.
func GenerateJobID(height uint32) (string, error) {
	buf := bytes.Buffer{}
	buf.Write(heightToBigEndianBytes(height))
	buf.Write(nanoToBigEndianBytes(time.Now().UnixNano()))
	return hex.EncodeToString(buf.Bytes()), nil
}

// NewJob creates a job instance.
func NewJob(header string, height uint32) (*Job, error) {
	id, err := GenerateJobID(height)
	if err != nil {
		return nil, err
	}

	return &Job{
		UUID:   id,
		Height: height,
		Header: header,
	}, nil
}

// fetchJobBucket is a helper function for getting the job bucket.
func fetchJobBucket(tx *bolt.Tx) (*bolt.Bucket, error) {
	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}
	bkt := pbkt.Bucket(jobBkt)
	if bkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(jobBkt))
		return nil, MakeError(ErrBucketNotFound, desc, nil)
	}

	return bkt, nil
}

// FetchJob fetches the job referenced by the provided id.
func FetchJob(db *bolt.DB, id []byte) (*Job, error) {
	var job Job
	err := db.View(func(tx *bolt.Tx) error {
		bkt, err := fetchJobBucket(tx)
		if err != nil {
			return err
		}

		v := bkt.Get(id)
		if v == nil {
			desc := fmt.Sprintf("no value found for job id %s", string(id))
			return MakeError(ErrValueNotFound, desc, nil)
		}
		err = json.Unmarshal(v, &job)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &job, err
}

// Create persists the job to the database.
func (job *Job) Create(db *bolt.DB) error {
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchJobBucket(tx)
		if err != nil {
			return err
		}

		jobBytes, err := json.Marshal(job)
		if err != nil {
			return err
		}

		return bkt.Put([]byte(job.UUID), jobBytes)
	})
	return err
}

// Update is not supported for jobs.
func (job *Job) Update(db *bolt.DB) error {
	desc := "job update not supported"
	return MakeError(ErrNotSupported, desc, nil)
}

// Delete removes the associated job from the database.
func (job *Job) Delete(db *bolt.DB) error {
	return deleteEntry(db, jobBkt, []byte(job.UUID))
}

// PruneJobs removes all jobs with heights less than the provided height.
func PruneJobs(db *bolt.DB, height uint32) error {
	heightBE := heightToBigEndianBytes(height)
	err := db.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchJobBucket(tx)
		if err != nil {
			return err
		}

		toDelete := [][]byte{}
		c := bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			height, err := hex.DecodeString(string(k[:8]))
			if err != nil {
				return err
			}

			if bytes.Compare(height, heightBE) < 0 {
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

	return err
}
