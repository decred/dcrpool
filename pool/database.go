package pool

import (
	"net/http"

	bolt "go.etcd.io/bbolt"
)

// Database describes all of the functionality needed by a dcrpool database
// implementation.
type Database interface {
	// Utils
	httpBackup(w http.ResponseWriter) error
	backup(fileName string) error
	close() error

	// Pool metadata
	persistPoolMode(mode uint32) error
	fetchCSRFSecret() ([]byte, error)
	persistCSRFSecret(secret []byte) error
	persistLastPaymentInfo(height uint32, paidOn int64) error
	loadLastPaymentInfo() (uint32, int64, error)
	persistLastPaymentCreatedOn(createdOn int64) error
	loadLastPaymentCreatedOn() (int64, error)

	// Account
	FetchAccount(id string) (*Account, error)
	PersistAccount(acc *Account) error
	DeleteAccount(id string) error

	// Payment
	FetchPayment(id string) (*Payment, error)
	PersistPayment(payment *Payment) error
	UpdatePayment(payment *Payment) error
	DeletePayment(payment *Payment) error
	ArchivePayment(payment *Payment) error
	fetchPaymentsAtHeight(height uint32) ([]*Payment, error)
	fetchPendingPayments() ([]*Payment, error)
	pendingPaymentsForBlockHash(blockHash string) (uint32, error)
	archivedPayments() ([]*Payment, error)
	maturePendingPayments(height uint32) (map[string][]*Payment, error)

	// Share
	PersistShare(share *Share) error
	ppsEligibleShares(max int64) ([]*Share, error)
	pplnsEligibleShares(min int64) ([]*Share, error)
	pruneShares(minNano int64) error

	// AcceptedWork
	FetchAcceptedWork(id string) (*AcceptedWork, error)
	PersistAcceptedWork(work *AcceptedWork) error
	UpdateAcceptedWork(work *AcceptedWork) error
	DeleteAcceptedWork(work *AcceptedWork) error
	ListMinedWork() ([]*AcceptedWork, error)
	fetchUnconfirmedWork(height uint32) ([]*AcceptedWork, error)

	// Job
	FetchJob(id string) (*Job, error)
	PersistJob(job *Job) error
	DeleteJob(job *Job) error
	DeleteJobsBeforeHeight(height uint32) error
}

// BoltDB is a wrapper around bolt.DB which implements the Database interface.
type BoltDB struct {
	DB *bolt.DB
}
