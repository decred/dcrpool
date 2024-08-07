// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"bytes"
	"encoding/hex"
	"time"
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
