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
	fetchAccount(id string) (*Account, error)
	persistAccount(acc *Account) error
	deleteAccount(id string) error

	// Payment
	fetchPayment(id string) (*Payment, error)
	PersistPayment(payment *Payment) error
	updatePayment(payment *Payment) error
	deletePayment(id string) error
	ArchivePayment(payment *Payment) error
	fetchPaymentsAtHeight(height uint32) ([]*Payment, error)
	fetchPendingPayments() ([]*Payment, error)
	pendingPaymentsForBlockHash(blockHash string) (uint32, error)
	archivedPayments() ([]*Payment, error)
	maturePendingPayments(height uint32) (map[string][]*Payment, error)

	// Share
	PersistShare(share *Share) error
	fetchShare(id string) (*Share, error)
	ppsEligibleShares(max int64) ([]*Share, error)
	pplnsEligibleShares(min int64) ([]*Share, error)
	pruneShares(minNano int64) error

	// AcceptedWork
	fetchAcceptedWork(id string) (*AcceptedWork, error)
	persistAcceptedWork(work *AcceptedWork) error
	updateAcceptedWork(work *AcceptedWork) error
	deleteAcceptedWork(work *AcceptedWork) error
	listMinedWork() ([]*AcceptedWork, error)
	fetchUnconfirmedWork(height uint32) ([]*AcceptedWork, error)

	// Job
	fetchJob(id string) (*Job, error)
	persistJob(job *Job) error
	deleteJob(id string) error
	deleteJobsBeforeHeight(height uint32) error
}

// BoltDB is a wrapper around bolt.DB which implements the Database interface.
type BoltDB struct {
	DB *bolt.DB
}
