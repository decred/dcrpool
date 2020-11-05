// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/decred/dcrd/dcrutil/v3"

	"github.com/lib/pq"

	"github.com/decred/dcrpool/errors"
)

// InitPostgresDB connects to the specified database and creates all tables
// required by dcrpool.
func InitPostgresDB(host string, port uint32, user, pass, dbName string) (*PostgresDB, error) {
	const funcName = "InitPostgresDB"

	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		host, port, user, pass, dbName)

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to open postgres: %v", funcName, err)
		return nil, errors.DBError(errors.DBOpen, desc)
	}

	// Send a Ping() to validate the db connection. This is because the Open()
	// func does not actually create a connection to the database, it just
	// validates the provided arguments.
	err = db.Ping()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to connect to postgres: %v", funcName, err)
		return nil, errors.DBError(errors.DBOpen, desc)
	}

	// Create all of the tables required by dcrpool.
	_, err = db.Exec(createTableMetadata)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(createTableAccounts)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(createTablePayments)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(createTableArchivedPayments)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(createTableJobs)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(createTableShares)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(createTableAcceptedWork)
	if err != nil {
		return nil, err
	}

	return &PostgresDB{db}, nil
}

// Close closes the postgres database connection.
func (db *PostgresDB) Close() error {
	return db.DB.Close()
}

// decodePaymentRows deserializes the provided SQL rows into a slice of Payment
// structs.
func decodePaymentRows(rows *sql.Rows) ([]*Payment, error) {
	var toReturn []*Payment
	for rows.Next() {
		var uuid, account, transactionID, sourceBlockHash, sourceCoinbase string
		var estimatedMaturity, height, paidOnHeight uint32
		var amount, createdon int64
		err := rows.Scan(&uuid, &account, &estimatedMaturity,
			&height, &amount, &createdon, &paidOnHeight, &transactionID,
			&sourceBlockHash, &sourceCoinbase)
		if err != nil {
			return nil, err
		}

		payment := &Payment{uuid, account, estimatedMaturity,
			height, dcrutil.Amount(amount), createdon, paidOnHeight, transactionID,
			&PaymentSource{sourceBlockHash, sourceCoinbase}}
		toReturn = append(toReturn, payment)
	}

	err := rows.Err()
	if err != nil {
		return nil, err
	}

	return toReturn, nil
}

// decodeWorkRows deserializes the provided SQL rows into a slice of
// AcceptedWork structs.
func decodeWorkRows(rows *sql.Rows) ([]*AcceptedWork, error) {
	var toReturn []*AcceptedWork
	for rows.Next() {
		var uuid, blockhash, prevhash, minedby, miner string
		var confirmed bool
		var height uint32
		var createdOn int64
		err := rows.Scan(&uuid, &blockhash, &prevhash, &height,
			&minedby, &miner, &createdOn, &confirmed)
		if err != nil {
			return nil, err
		}

		work := &AcceptedWork{uuid, blockhash, prevhash, height,
			minedby, miner, createdOn, confirmed}
		toReturn = append(toReturn, work)
	}

	err := rows.Err()
	if err != nil {
		return nil, err
	}

	return toReturn, nil
}

// decodeShareRows deserializes the provided SQL rows into a slice of Share
// structs.
func decodeShareRows(rows *sql.Rows) ([]*Share, error) {
	var toReturn []*Share
	for rows.Next() {
		var uuid, account, weight string
		var createdon int64
		err := rows.Scan(&uuid, &account, &weight, &createdon)
		if err != nil {
			return nil, err
		}

		weightRat, ok := new(big.Rat).SetString(weight)
		if !ok {
			desc := fmt.Sprintf("unable to decode rat string: %v", err)
			return nil, errors.DBError(errors.Parse, desc)
		}
		share := &Share{uuid, account, weightRat, createdon}
		toReturn = append(toReturn, share)
	}

	err := rows.Err()
	if err != nil {
		return nil, err
	}

	return toReturn, nil
}

// httpBackup is not implemented for postgres database.
func (db *PostgresDB) httpBackup(w http.ResponseWriter) error {
	return errors.New("httpBackup is not implemented for postgres database")
}

// Backup is not implemented for postgres database.
func (db *PostgresDB) Backup(fileName string) error {
	return errors.New("Backup is not implemented for postgres database")
}

// fetchPoolMode retrives the pool mode from the database. PoolMode is stored as
// a uint32 for historical reasons. 0 indicates Public, 1 indicates Solo.
func (db *PostgresDB) fetchPoolMode() (uint32, error) {
	const funcName = "fetchPoolMode"
	var poolmode uint32
	err := db.DB.QueryRow(selectPoolMode).Scan(&poolmode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for poolmode", funcName)
			return 0, errors.DBError(errors.ValueNotFound, desc)
		}

		return 0, err
	}
	return poolmode, nil
}

// persistPoolMode stores the pool mode in the database. PoolMode is stored as a
// uint32 for historical reasons. 0 indicates Public, 1 indicates Solo.
func (db *PostgresDB) persistPoolMode(mode uint32) error {
	_, err := db.DB.Exec(insertPoolMode, mode)
	return err
}

// fetchCSRFSecret retrieves the bytes used for the CSRF secret from the database.
func (db *PostgresDB) fetchCSRFSecret() ([]byte, error) {
	const funcName = "fetchCSRFSecret"
	var secret string
	err := db.DB.QueryRow(selectCSRFSecret).Scan(&secret)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for csrfsecret", funcName)
			return nil, errors.DBError(errors.ValueNotFound, desc)
		}

		return nil, err
	}

	decoded, err := hex.DecodeString(secret)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode csrf secret: %v",
			funcName, err)
		return nil, errors.DBError(errors.Parse, desc)
	}

	return decoded, nil
}

// persistCSRFSecret stores the bytes used for the CSRF secret in the database.
func (db *PostgresDB) persistCSRFSecret(secret []byte) error {
	_, err := db.DB.Exec(insertCSRFSecret, hex.EncodeToString(secret))
	return err
}

// persistLastPaymentInfo stores the last payment height and paidOn timestamp
// in the database.
func (db *PostgresDB) persistLastPaymentInfo(height uint32, paidOn int64) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec(insertLastPaymentHeight, height)
	if err != nil {
		tx.Rollback()
		return err
	}

	_, err = tx.Exec(insertLastPaymentPaidOn, paidOn)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// loadLastPaymentInfo retrieves the last payment height and paidOn timestamp
// from the database.
func (db *PostgresDB) loadLastPaymentInfo() (uint32, int64, error) {
	const funcName = "loadLastPaymentInfo"

	var height uint32
	err := db.DB.QueryRow(selectLastPaymentHeight).Scan(&height)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for lastpaymentheight",
				funcName)
			return 0, 0, errors.DBError(errors.ValueNotFound, desc)
		}

		return 0, 0, err
	}

	var paidOn int64
	err = db.DB.QueryRow(selectLastPaymentPaidOn).Scan(&paidOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for lastpaymentpaidon",
				funcName)
			return 0, 0, errors.DBError(errors.ValueNotFound, desc)
		}

		return 0, 0, err
	}

	return height, paidOn, nil
}

// persistLastPaymentCreatedOn stores the last payment createdOn timestamp in
// the database.
func (db *PostgresDB) persistLastPaymentCreatedOn(createdOn int64) error {
	_, err := db.DB.Exec(insertLastPaymentCreatedOn, createdOn)
	return err
}

// loadLastPaymentCreatedOn retrieves the last payment createdOn timestamp from
// the database.
func (db *PostgresDB) loadLastPaymentCreatedOn() (int64, error) {
	const funcName = "loadLastPaymentCreatedOn"
	var createdOn int64
	err := db.DB.QueryRow(selectLastPaymentCreatedOn).Scan(&createdOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for lastpaymentcreatedon",
				funcName)
			return 0, errors.DBError(errors.ValueNotFound, desc)
		}

		return 0, err
	}
	return createdOn, nil
}

// persistAccount saves the account to the database. Before persisting the
// account, it sets the createdOn timestamp. Returns an error if an account
// already exists with the same ID.
func (db *PostgresDB) persistAccount(acc *Account) error {
	const funcName = "persistAccount"
	_, err := db.DB.Exec(insertAccount, acc.UUID, acc.Address, uint64(time.Now().Unix()))
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: account %s already exists", funcName,
					acc.UUID)
				return errors.DBError(errors.ValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// fetchAccount fetches the account referenced by the provided id. Returns
// an error if the account is not found.
func (db *PostgresDB) fetchAccount(id string) (*Account, error) {
	const funcName = "fetchAccount"
	var uuid, address string
	var createdOn uint64
	err := db.DB.QueryRow(selectAccount, id).Scan(&uuid, &address, &createdOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no account found for id %s", funcName, id)
			return nil, errors.DBError(errors.ValueNotFound, desc)
		}

		return nil, err
	}
	return &Account{uuid, address, createdOn}, nil
}

// deleteAccount purges the referenced account from the database.
func (db *PostgresDB) deleteAccount(id string) error {
	_, err := db.DB.Exec(deleteAccount, id)
	return err
}

// fetchPayment fetches the payment referenced by the provided id. Returns an
// error if the payment is not found.
func (db *PostgresDB) fetchPayment(id string) (*Payment, error) {
	const funcName = "fetchPayment"
	var uuid, account, transactionID, sourceBlockHash, sourceCoinbase string
	var estimatedMaturity, height, paidOnHeight uint32
	var amount, createdOn int64

	err := db.DB.QueryRow(selectPayment, id).Scan(&uuid, &account, &estimatedMaturity,
		&height, &amount, &createdOn, &paidOnHeight, &transactionID,
		&sourceBlockHash, &sourceCoinbase)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no payment found for id %s", funcName, id)
			return nil, errors.DBError(errors.ValueNotFound, desc)
		}

		return nil, err
	}
	return &Payment{uuid, account, estimatedMaturity,
		height, dcrutil.Amount(amount), createdOn, paidOnHeight, transactionID,
		&PaymentSource{sourceBlockHash, sourceCoinbase}}, nil
}

// PersistPayment saves a payment to the database.
func (db *PostgresDB) PersistPayment(p *Payment) error {
	const funcName = "PersistPayment"

	_, err := db.DB.Exec(insertPayment,
		p.UUID, p.Account, p.EstimatedMaturity, p.Height, p.Amount, p.CreatedOn,
		p.PaidOnHeight, p.TransactionID, p.Source.BlockHash, p.Source.Coinbase)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: payment %s already exists", funcName,
					p.UUID)
				return errors.DBError(errors.ValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// updatePayment persists the updated payment to the database.
func (db *PostgresDB) updatePayment(p *Payment) error {
	_, err := db.DB.Exec(updatePayment,
		p.UUID, p.Account, p.EstimatedMaturity, p.Height, p.Amount, p.CreatedOn,
		p.PaidOnHeight, p.TransactionID, p.Source.BlockHash, p.Source.Coinbase)
	return err
}

// deletePayment purges the referenced payment from the database. Note that
// archived payments cannot be deleted.
func (db *PostgresDB) deletePayment(id string) error {
	_, err := db.DB.Exec(deletePayment, id)
	return err
}

// ArchivePayment removes the associated payment from active payments and archives it.
func (db *PostgresDB) ArchivePayment(p *Payment) error {
	const funcName = "ArchivePayment"

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec(deletePayment, p.UUID)
	if err != nil {
		tx.Rollback()
		return err
	}

	aPmt := NewPayment(p.Account, p.Source, p.Amount, p.Height,
		p.EstimatedMaturity)

	_, err = tx.Exec(insertArchivedPayment,
		aPmt.UUID, aPmt.Account, aPmt.EstimatedMaturity, aPmt.Height, aPmt.Amount,
		aPmt.CreatedOn, aPmt.PaidOnHeight, aPmt.TransactionID, aPmt.Source.BlockHash,
		p.Source.Coinbase)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

// fetchPaymentsAtHeight returns all payments sourcing from orphaned blocks at
// the provided height.
func (db *PostgresDB) fetchPaymentsAtHeight(height uint32) ([]*Payment, error) {
	rows, err := db.DB.Query(selectPaymentsAtHeight, height)
	if err != nil {
		return nil, err
	}

	return decodePaymentRows(rows)
}

// fetchPendingPayments fetches all unpaid payments.
func (db *PostgresDB) fetchPendingPayments() ([]*Payment, error) {
	rows, err := db.DB.Query(selectPendingPayments)
	if err != nil {
		return nil, err
	}

	return decodePaymentRows(rows)
}

// pendingPaymentsForBlockHash returns the number of pending payments with the
// provided block hash as their source.
func (db *PostgresDB) pendingPaymentsForBlockHash(blockHash string) (uint32, error) {
	var count uint32
	err := db.DB.QueryRow(countPaymentsAtBlockHash, blockHash).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// archivedPayments fetches all archived payments. List is ordered, most
// recent comes first.
func (db *PostgresDB) archivedPayments() ([]*Payment, error) {
	rows, err := db.DB.Query(selectArchivedPayments)
	if err != nil {
		return nil, err
	}

	return decodePaymentRows(rows)
}

// maturePendingPayments fetches all mature pending payments at the
// provided height.
func (db *PostgresDB) maturePendingPayments(height uint32) (map[string][]*Payment, error) {
	rows, err := db.DB.Query(selectMaturePendingPayments, height)
	if err != nil {
		return nil, err
	}

	payments, err := decodePaymentRows(rows)
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
func (db *PostgresDB) fetchShare(id string) (*Share, error) {
	const funcName = "fetchShare"
	var uuid, account, weight string
	var createdOn int64
	err := db.DB.QueryRow(selectShare, id).Scan(&uuid, &account, &weight, &createdOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no share found for id %s", funcName, id)
			return nil, errors.DBError(errors.ValueNotFound, desc)
		}

		return nil, err
	}

	weightRat, ok := new(big.Rat).SetString(weight)
	if !ok {
		desc := fmt.Sprintf("%s: unable to decode rat string: %v",
			funcName, err)
		return nil, errors.DBError(errors.Parse, desc)
	}

	return &Share{uuid, account, weightRat, createdOn}, nil
}

// PersistShare saves a share to the database. Returns an error if a share
// already exists with the same ID.
func (db *PostgresDB) PersistShare(share *Share) error {
	const funcName = "PersistShare"

	_, err := db.DB.Exec(insertShare, share.UUID, share.Account, share.Weight.RatString(), share.CreatedOn)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: share %s already exists", funcName,
					share.UUID)
				return errors.DBError(errors.ValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// ppsEligibleShares fetches all shares created before or at the provided time.
func (db *PostgresDB) ppsEligibleShares(max int64) ([]*Share, error) {
	rows, err := db.DB.Query(selectSharesOnOrBeforeTime, max)
	if err != nil {
		return nil, err
	}

	return decodeShareRows(rows)
}

// pplnsEligibleShares fetches all shares created after the provided time.
func (db *PostgresDB) pplnsEligibleShares(min int64) ([]*Share, error) {
	rows, err := db.DB.Query(selectSharesAfterTime, min)
	if err != nil {
		return nil, err
	}

	return decodeShareRows(rows)
}

// pruneShares removes shares with a createdOn time earlier than the provided
// time.
func (db *PostgresDB) pruneShares(minNano int64) error {
	_, err := db.DB.Exec(deleteShareCreatedBefore, minNano)
	return err
}

// fetchAcceptedWork fetches the accepted work referenced by the provided id.
// Returns an error if the work is not found.
func (db *PostgresDB) fetchAcceptedWork(id string) (*AcceptedWork, error) {
	const funcName = "fetchAcceptedWork"

	var uuid, blockhash, prevhash, minedby, miner string
	var confirmed bool
	var height uint32
	var createdOn int64
	err := db.DB.QueryRow(selectAcceptedWork, id).Scan(&uuid, &blockhash, &prevhash, &height,
		&minedby, &miner, &createdOn, &confirmed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no work found for id %s", funcName, id)
			return nil, errors.DBError(errors.ValueNotFound, desc)
		}

		return nil, err
	}

	return &AcceptedWork{uuid, blockhash, prevhash, height,
		minedby, miner, createdOn, confirmed}, nil
}

// persistAcceptedWork saves the accepted work to the database.
func (db *PostgresDB) persistAcceptedWork(work *AcceptedWork) error {
	const funcName = "persistAcceptedWork"

	_, err := db.DB.Exec(insertAcceptedWork, work.UUID, work.BlockHash, work.PrevHash,
		work.Height, work.MinedBy, work.Miner, work.CreatedOn, work.Confirmed)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: work %s already exists", funcName,
					work.UUID)
				return errors.DBError(errors.ValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// updateAcceptedWork persists modifications to an existing work. Returns an
// error if the work is not found.
func (db *PostgresDB) updateAcceptedWork(work *AcceptedWork) error {
	const funcName = "updateAcceptedWork"

	result, err := db.DB.Exec(updateAcceptedWork,
		work.UUID, work.BlockHash, work.PrevHash,
		work.Height, work.MinedBy, work.Miner, work.CreatedOn, work.Confirmed)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		desc := fmt.Sprintf("%s: work %s not found", funcName, work.UUID)
		return errors.DBError(errors.ValueNotFound, desc)
	}

	return nil
}

// deleteAcceptedWork removes the associated accepted work from the database.
func (db *PostgresDB) deleteAcceptedWork(id string) error {
	_, err := db.DB.Exec(deleteAcceptedWork, id)
	return err
}

// listMinedWork returns work data associated with all blocks mined by the pool
// regardless of whether they are confirmed or not.
//
// List is ordered, most recent comes first.
func (db *PostgresDB) listMinedWork() ([]*AcceptedWork, error) {
	rows, err := db.DB.Query(selectMinedWork)
	if err != nil {
		return nil, err
	}

	return decodeWorkRows(rows)
}

// fetchUnconfirmedWork returns all work which is not confirmed as mined with
// height less than the provided height.
func (db *PostgresDB) fetchUnconfirmedWork(height uint32) ([]*AcceptedWork, error) {
	rows, err := db.DB.Query(selectUnconfirmedWork, height)
	if err != nil {
		return nil, err
	}

	return decodeWorkRows(rows)
}

// fetchJob fetches the job referenced by the provided id. Returns an error if
// the job is not found.
func (db *PostgresDB) fetchJob(id string) (*Job, error) {
	const funcName = "fetchJob"
	var uuid, header string
	var height uint32
	err := db.DB.QueryRow(selectJob, id).Scan(&uuid, &header, &height)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no job found for id %s", funcName, id)
			return nil, errors.DBError(errors.ValueNotFound, desc)
		}

		return nil, err
	}
	return &Job{uuid, height, header}, nil
}

// persistJob saves the job to the database. Returns an error if an account
// already exists with the same ID.
func (db *PostgresDB) persistJob(job *Job) error {
	const funcName = "persistJob"

	_, err := db.DB.Exec(insertJob, job.UUID, job.Height, job.Header)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: job %s already exists", funcName,
					job.UUID)
				return errors.DBError(errors.ValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// deleteJob removes the associated job from the database.
func (db *PostgresDB) deleteJob(id string) error {
	_, err := db.DB.Exec(deleteJob, id)
	return err
}

// deleteJobsBeforeHeight removes all jobs with heights less than the provided
// height.
func (db *PostgresDB) deleteJobsBeforeHeight(height uint32) error {
	_, err := db.DB.Exec(deleteJobBeforeHeight, height)
	return err
}
