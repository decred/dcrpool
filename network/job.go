package network

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/dnldd/dcrpool/dividend"

	bolt "github.com/coreos/bbolt"
	"github.com/dnldd/dcrpool/database"
)

// Job represents cached copies of work delivered to clients.
type Job struct {
	UUID   string `json:"uuid"`
	Height uint32 `json:"height"`
	Header string `json:"header"`
}

// HeightToBigEndianBytes returns an 4-byte big endian representation of
// the provided block height.
func HeightToBigEndianBytes(height uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, height)
	return b
}

// BigEndianBytesToHeight returns the block height of the provided 4-byte big
// endian representation.
func BigEndianBytesToHeight(b []byte) uint32 {
	return binary.BigEndian.Uint32(b[0:4])
}

// GenerateJobID generates a unique job id of the provided block height.
func GenerateJobID(height uint32) (string, error) {
	buf := bytes.Buffer{}
	buf.Write(HeightToBigEndianBytes(height))
	buf.Write(dividend.NanoToBigEndianBytes(time.Now().UnixNano()))
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

// FetchJob fetches the job referenced by the provided id.
func FetchJob(db *bolt.DB, id []byte) (*Job, error) {
	var job Job
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.JobBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.JobBkt)
		}
		v := bkt.Get(id)
		if v == nil {
			return database.ErrValueNotFound(id)
		}
		err := json.Unmarshal(v, &job)
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
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.JobBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.JobBkt)
		}
		jobBytes, err := json.Marshal(job)
		if err != nil {
			return err
		}

		return bkt.Put([]byte(job.UUID), jobBytes)
	})
	return err
}

// Update persists the updated accepted work to the database.
func (job *Job) Update(db *bolt.DB) error {
	return job.Create(db)
}

// Delete removes the associated job from the database.
func (job *Job) Delete(db *bolt.DB) error {
	return database.Delete(db, database.JobBkt, []byte(job.UUID))
}

// PruneJobs removes all jobs entries with heights less than
// or equal to the provided height.
func PruneJobs(db *bolt.DB, height uint32) error {
	heightBE := HeightToBigEndianBytes(height)
	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}
		bkt := pbkt.Bucket(database.JobBkt)
		if bkt == nil {
			return database.ErrBucketNotFound(database.JobBkt)
		}

		c := bkt.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			height, err := hex.DecodeString(string(k[:8]))
			if err != nil {
				return err
			}

			if bytes.Compare(height, heightBE) <= 0 {
				err := c.Delete()
				if err != nil {
					return err
				}
			}
		}

		return nil
	})

	return err
}
