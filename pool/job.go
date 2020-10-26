// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
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

// jobID generates a unique job id of the provided block height.
func jobID(height uint32) string {
	var buf bytes.Buffer
	_, _ = buf.Write(heightToBigEndianBytes(height))
	_, _ = buf.Write(nanoToBigEndianBytes(time.Now().UnixNano()))
	return hex.EncodeToString(buf.Bytes())
}

// NewJob creates a job instance.
func NewJob(header string, height uint32) *Job {
	return &Job{
		UUID:   jobID(height),
		Height: height,
		Header: header,
	}
}

// fetchJob fetches the job referenced by the provided id.
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

// persistJob saves the job to the database.
func (db *BoltDB) persistJob(job *Job) error {
	const funcName = "persistJob"
	return db.DB.Update(func(tx *bolt.Tx) error {
		bkt, err := fetchBucket(tx, jobBkt)
		if err != nil {
			return err
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
func (db *BoltDB) deleteJob(job *Job) error {
	return deleteEntry(db, jobBkt, job.UUID)
}

// deleteJobsBeforeHeight removes all jobs with heights less than the provided height.
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
