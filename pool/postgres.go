// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/decred/dcrd/dcrutil/v3"

	"github.com/lib/pq"
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
		return nil, dbError(ErrDBOpen, desc)
	}

	// Send a Ping() to validate the db connection. This is because the Open()
	// func does not actually create a connection to the database, it just
	// validates the provided arguments.
	err = db.Ping()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to connect to postgres: %v", funcName, err)
		return nil, dbError(ErrDBOpen, desc)
	}

	err = createMetadataTable(db)
	if err != nil {
		return nil, err
	}

	err = createAccountsTable(db)
	if err != nil {
		return nil, err
	}

	err = createJobsTable(db)
	if err != nil {
		return nil, err
	}

	err = createSharesTable(db)
	if err != nil {
		return nil, err
	}

	err = createPaymentsTable(db)
	if err != nil {
		return nil, err
	}

	err = createArchivedPaymentsTable(db)
	if err != nil {
		return nil, err
	}

	err = createAcceptedWorkTable(db)
	if err != nil {
		return nil, err
	}

	return &PostgresDB{db}, nil
}

// Close closes the postgres database connection.
func (db *PostgresDB) Close() error {
	return db.DB.Close()
}

func createMetadataTable(db *sql.DB) error {
	const stmt = `CREATE TABLE IF NOT EXISTS metadata (
		key      TEXT PRIMARY KEY,
		value    TEXT NOT NULL
	);`

	_, err := db.Exec(stmt)
	return err
}

func createAccountsTable(db *sql.DB) error {
	const stmt = `CREATE TABLE IF NOT EXISTS accounts (
		uuid      TEXT PRIMARY KEY,
		address   TEXT NOT NULL,
		createdon INT8 NOT NULL
	);`

	_, err := db.Exec(stmt)
	return err
}

func createPaymentsTable(db *sql.DB) error {
	const stmt = `CREATE TABLE IF NOT EXISTS payments (
		uuid              TEXT PRIMARY KEY,
		account           TEXT NOT NULL,
		estimatedmaturity INT8 NOT NULL,
		height            INT8 NOT NULL,
		amount            INT8 NOT NULL,
		createdon         INT8 NOT NULL,
		paidonheight      INT8 NOT NULL,
		transactionid     TEXT NOT NULL,
		sourceblockhash   TEXT NOT NULL,
		sourcecoinbase    TEXT NOT NULL
	);`

	_, err := db.Exec(stmt)
	return err
}

func createArchivedPaymentsTable(db *sql.DB) error {
	const stmt = `CREATE TABLE IF NOT EXISTS archivedpayments (
		uuid              TEXT PRIMARY KEY,
		account           TEXT NOT NULL,
		estimatedmaturity INT8 NOT NULL,
		height            INT8 NOT NULL,
		amount            INT8 NOT NULL,
		createdon         INT8 NOT NULL,
		paidonheight      INT8 NOT NULL,
		transactionid     TEXT NOT NULL,
		sourceblockhash   TEXT NOT NULL,
		sourcecoinbase    TEXT NOT NULL
	);`

	_, err := db.Exec(stmt)
	return err
}

func createJobsTable(db *sql.DB) error {
	const stmt = `CREATE TABLE IF NOT EXISTS jobs (
		uuid   TEXT PRIMARY KEY,
		height INT8 NOT NULL,
		header TEXT NOT NULL
	);`

	_, err := db.Exec(stmt)
	return err
}

func createSharesTable(db *sql.DB) error {
	const stmt = `CREATE TABLE IF NOT EXISTS shares (
		uuid      TEXT PRIMARY KEY,
		account   TEXT NOT NULL,
		weight    TEXT NOT NULL,
		createdon INT8 NOT NULL
	);`

	_, err := db.Exec(stmt)
	return err
}

func createAcceptedWorkTable(db *sql.DB) error {
	const stmt = `CREATE TABLE IF NOT EXISTS acceptedwork (
		uuid      TEXT    PRIMARY KEY,
		blockhash TEXT    NOT NULL,
		prevhash  TEXT    NOT NULL,
		height    INT8    NOT NULL,
		minedby   TEXT    NOT NULL,
		miner     TEXT    NOT NULL,
		createdon INT8    NOT NULL,
		confirmed BOOLEAN NOT NULL
	);`

	_, err := db.Exec(stmt)
	return err
}

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
			return nil, dbError(ErrParse, desc)
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

func (db *PostgresDB) fetchPoolMode() (uint32, error) {
	const funcName = "fetchPoolMode"
	const stmt = `SELECT value FROM metadata WHERE key='poolmode';`
	var poolmode uint32
	err := db.DB.QueryRow(stmt).Scan(&poolmode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for poolmode", funcName)
			return 0, dbError(ErrValueNotFound, desc)
		}

		return 0, err
	}
	return poolmode, nil
}

func (db *PostgresDB) persistPoolMode(mode uint32) error {
	const stmt = `INSERT INTO metadata(key, value)
	VALUES ('poolmode', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	_, err := db.DB.Exec(stmt, mode)
	return err
}

func (db *PostgresDB) fetchCSRFSecret() ([]byte, error) {
	const funcName = "fetchCSRFSecret"
	const stmt = `SELECT value FROM metadata WHERE key='csrfsecret';`
	var secret string
	err := db.DB.QueryRow(stmt).Scan(&secret)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for csrfsecret", funcName)
			return nil, dbError(ErrValueNotFound, desc)
		}

		return nil, err
	}

	decoded, err := hex.DecodeString(secret)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode csrf secret: %v",
			funcName, err)
		return nil, dbError(ErrParse, desc)
	}

	return decoded, nil
}

func (db *PostgresDB) persistCSRFSecret(secret []byte) error {
	const stmt = `INSERT INTO metadata(key, value)
	VALUES ('csrfsecret', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	_, err := db.DB.Exec(stmt, hex.EncodeToString(secret))
	return err
}

func (db *PostgresDB) persistLastPaymentInfo(height uint32, paidOn int64) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}

	const stmt = `INSERT INTO metadata(key, value)
	VALUES ('lastpaymentheight', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	_, err = tx.Exec(stmt, height)
	if err != nil {
		tx.Rollback()
		return err
	}

	const stmt2 = `INSERT INTO metadata(key, value)
	VALUES ('lastpaymentpaidon', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	_, err = tx.Exec(stmt2, paidOn)
	if err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (db *PostgresDB) loadLastPaymentInfo() (uint32, int64, error) {
	const funcName = "loadLastPaymentInfo"
	const stmt = `SELECT value FROM metadata WHERE key='lastpaymentheight';`
	var height uint32
	err := db.DB.QueryRow(stmt).Scan(&height)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for lastpaymentheight",
				funcName)
			return 0, 0, dbError(ErrValueNotFound, desc)
		}

		return 0, 0, err
	}

	const stmt2 = `SELECT value FROM metadata WHERE key='lastpaymentpaidon';`
	var paidOn int64
	err = db.DB.QueryRow(stmt2).Scan(&paidOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for lastpaymentpaidon",
				funcName)
			return 0, 0, dbError(ErrValueNotFound, desc)
		}

		return 0, 0, err
	}

	return height, paidOn, nil
}

func (db *PostgresDB) persistLastPaymentCreatedOn(createdOn int64) error {
	const stmt = `INSERT INTO metadata(key, value)
	VALUES ('lastpaymentcreatedon', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	_, err := db.DB.Exec(stmt, createdOn)
	return err
}

func (db *PostgresDB) loadLastPaymentCreatedOn() (int64, error) {
	const funcName = "loadLastPaymentCreatedOn"
	const stmt = `SELECT value FROM metadata WHERE key='lastpaymentcreatedon';`
	var createdOn int64
	err := db.DB.QueryRow(stmt).Scan(&createdOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for lastpaymentcreatedon",
				funcName)
			return 0, dbError(ErrValueNotFound, desc)
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
	const stmt = `INSERT INTO accounts(uuid, address, createdon) VALUES ($1,$2,$3);`
	_, err := db.DB.Exec(stmt, acc.UUID, acc.Address, uint64(time.Now().Unix()))
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: account %s already exists", funcName,
					acc.UUID)
				return dbError(ErrValueFound, desc)
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
	const stmt = `SELECT uuid, address, createdon FROM accounts WHERE uuid=$1;`
	var uuid, address string
	var createdOn uint64
	err := db.DB.QueryRow(stmt, id).Scan(&uuid, &address, &createdOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no account found for id %s", funcName, id)
			return nil, dbError(ErrValueNotFound, desc)
		}

		return nil, err
	}
	return &Account{uuid, address, createdOn}, nil
}

// deleteAccount purges the referenced account from the database.
func (db *PostgresDB) deleteAccount(id string) error {
	const stmt = `DELETE FROM accounts WHERE uuid=$1;`
	_, err := db.DB.Exec(stmt, id)
	return err
}

// fetchPayment fetches the payment referenced by the provided id. Returns an
// error if the payment is not found.
func (db *PostgresDB) fetchPayment(id string) (*Payment, error) {
	const funcName = "fetchPayment"
	const stmt = `SELECT uuid, account, estimatedmaturity, height, amount, createdon,
			 paidonheight, transactionid, sourceblockhash, sourcecoinbase
			 FROM payments WHERE uuid=$1;`
	var uuid, account, transactionID, sourceBlockHash, sourceCoinbase string
	var estimatedMaturity, height, paidOnHeight uint32
	var amount, createdOn int64

	err := db.DB.QueryRow(stmt, id).Scan(&uuid, &account, &estimatedMaturity,
		&height, &amount, &createdOn, &paidOnHeight, &transactionID,
		&sourceBlockHash, &sourceCoinbase)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no payment found for id %s", funcName, id)
			return nil, dbError(ErrValueNotFound, desc)
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

	const stmt = `INSERT INTO payments(
		uuid, account, estimatedmaturity, height, amount, createdon,
		paidonheight, transactionid, sourceblockhash, sourcecoinbase
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);`
	_, err := db.DB.Exec(stmt,
		p.UUID, p.Account, p.EstimatedMaturity, p.Height, p.Amount, p.CreatedOn,
		p.PaidOnHeight, p.TransactionID, p.Source.BlockHash, p.Source.Coinbase)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: payment %s already exists", funcName,
					p.UUID)
				return dbError(ErrValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// updatePayment persists the updated payment to the database.
func (db *PostgresDB) updatePayment(p *Payment) error {
	const stmt = `UPDATE payments SET
		account=$2,
		estimatedmaturity=$3,
		height=$4,
		amount=$5,
		createdon=$6,
		paidonheight=$7,
		transactionid=$8,
		sourceblockhash=$9,
		sourcecoinbase=$10
		WHERE uuid=$1;`
	_, err := db.DB.Exec(stmt,
		p.UUID, p.Account, p.EstimatedMaturity, p.Height, p.Amount, p.CreatedOn,
		p.PaidOnHeight, p.TransactionID, p.Source.BlockHash, p.Source.Coinbase)
	return err
}

// deletePayment purges the referenced payment from the database. Note that
// archived payments cannot be deleted.
func (db *PostgresDB) deletePayment(id string) error {
	const stmt = `DELETE FROM payments WHERE uuid=$1;`
	_, err := db.DB.Exec(stmt, id)
	return err
}

// ArchivePayment removes the associated payment from active payments and archives it.
func (db *PostgresDB) ArchivePayment(p *Payment) error {
	const funcName = "ArchivePayment"

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}

	const stmt = `DELETE FROM payments WHERE uuid=$1;`
	_, err = tx.Exec(stmt, p.UUID)
	if err != nil {
		tx.Rollback()
		return err
	}

	aPmt := NewPayment(p.Account, p.Source, p.Amount, p.Height,
		p.EstimatedMaturity)

	const stmt2 = `INSERT INTO archivedpayments(
				uuid, account, estimatedmaturity, height, amount, createdon,
				paidonheight, transactionid, sourceblockhash, sourcecoinbase
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);`
	_, err = tx.Exec(stmt2,
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
	const stmt = `SELECT uuid, account, estimatedmaturity, height, amount, createdon,
			 paidonheight, transactionid, sourceblockhash, sourcecoinbase
			 FROM payments
			 WHERE paidonheight=0
			 AND $1>(estimatedmaturity+1);`

	rows, err := db.DB.Query(stmt, height)
	if err != nil {
		return nil, err
	}

	return decodePaymentRows(rows)
}

// fetchPendingPayments fetches all unpaid payments.
func (db *PostgresDB) fetchPendingPayments() ([]*Payment, error) {
	const stmt = `SELECT uuid, account, estimatedmaturity, height, amount, createdon,
			 paidonheight, transactionid, sourceblockhash, sourcecoinbase
			 FROM payments
			 WHERE paidonheight=0;`

	rows, err := db.DB.Query(stmt)
	if err != nil {
		return nil, err
	}

	return decodePaymentRows(rows)
}

// pendingPaymentsForBlockHash returns the number of pending payments with the
// provided block hash as their source.
func (db *PostgresDB) pendingPaymentsForBlockHash(blockHash string) (uint32, error) {
	const stmt = `SELECT count(1)
			 FROM payments
			 WHERE paidonheight=0
			 AND sourceblockhash=$1;`

	var count uint32
	err := db.DB.QueryRow(stmt, blockHash).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// archivedPayments fetches all archived payments. List is ordered, most
// recent comes first.
func (db *PostgresDB) archivedPayments() ([]*Payment, error) {
	const stmt = `SELECT uuid, account, estimatedmaturity, height, amount, createdon,
			 paidonheight, transactionid, sourceblockhash, sourcecoinbase
			 FROM archivedpayments
			ORDER BY height DESC;`

	rows, err := db.DB.Query(stmt)
	if err != nil {
		return nil, err
	}

	return decodePaymentRows(rows)
}

// maturePendingPayments fetches all mature pending payments at the
// provided height.
func (db *PostgresDB) maturePendingPayments(height uint32) (map[string][]*Payment, error) {
	const stmt = `SELECT uuid, account, estimatedmaturity, height, amount, createdon,
			 paidonheight, transactionid, sourceblockhash, sourcecoinbase
			 FROM payments
			 WHERE paidonheight=0
			 AND (estimatedmaturity+1)<=$1;`

	rows, err := db.DB.Query(stmt, height)
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
	const stmt = `SELECT uuid, account, weight, createdon FROM shares WHERE uuid=$1;`
	var uuid, account, weight string
	var createdOn int64
	err := db.DB.QueryRow(stmt, id).Scan(&uuid, &account, &weight, &createdOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no share found for id %s", funcName, id)
			return nil, dbError(ErrValueNotFound, desc)
		}

		return nil, err
	}

	weightRat, ok := new(big.Rat).SetString(weight)
	if !ok {
		desc := fmt.Sprintf("%s: unable to decode rat string: %v",
			funcName, err)
		return nil, dbError(ErrParse, desc)
	}

	return &Share{uuid, account, weightRat, createdOn}, nil
}

// PersistShare saves a share to the database. Returns an error if a share
// already exists with the same ID.
func (db *PostgresDB) PersistShare(share *Share) error {
	const funcName = "PersistShare"
	const stmt = `INSERT INTO shares(uuid, account, weight, createdon) VALUES ($1,$2,$3,$4);`
	_, err := db.DB.Exec(stmt, share.UUID, share.Account, share.Weight.RatString(), share.CreatedOn)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: share %s already exists", funcName,
					share.UUID)
				return dbError(ErrValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// ppsEligibleShares fetches all shares created before or at the provided time.
func (db *PostgresDB) ppsEligibleShares(max int64) ([]*Share, error) {
	const funcName = "ppsEligibleShares"
	const stmt = `SELECT uuid, account, weight, createdon FROM shares WHERE createdon <= $1`
	rows, err := db.DB.Query(stmt, max)
	if err != nil {
		return nil, err
	}

	return decodeShareRows(rows)
}

// pplnsEligibleShares fetches all shares created after the provided time.
func (db *PostgresDB) pplnsEligibleShares(min int64) ([]*Share, error) {
	const funcName = "pplnsEligibleShares"
	const stmt = `SELECT uuid, account, weight, createdon FROM shares WHERE createdon > $1`
	rows, err := db.DB.Query(stmt, min)
	if err != nil {
		return nil, err
	}

	return decodeShareRows(rows)
}

// pruneShares removes shares with a createdOn time earlier than the provided
// time.
func (db *PostgresDB) pruneShares(minNano int64) error {
	const stmt = `DELETE FROM shares WHERE createdon < $1`
	_, err := db.DB.Exec(stmt, minNano)
	return err
}

// fetchAcceptedWork fetches the accepted work referenced by the provided id.
// Returns an error if the work is not found.
func (db *PostgresDB) fetchAcceptedWork(id string) (*AcceptedWork, error) {
	const funcName = "fetchAcceptedWork"
	const stmt = `SELECT
		uuid, blockhash, prevhash, height, minedby, miner, createdon, confirmed
		FROM acceptedwork WHERE uuid=$1;`

	var uuid, blockhash, prevhash, minedby, miner string
	var confirmed bool
	var height uint32
	var createdOn int64
	err := db.DB.QueryRow(stmt, id).Scan(&uuid, &blockhash, &prevhash, &height,
		&minedby, &miner, &createdOn, &confirmed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no work found for id %s", funcName, id)
			return nil, dbError(ErrValueNotFound, desc)
		}

		return nil, err
	}

	return &AcceptedWork{uuid, blockhash, prevhash, height,
		minedby, miner, createdOn, confirmed}, nil
}

// persistAcceptedWork saves the accepted work to the database.
func (db *PostgresDB) persistAcceptedWork(work *AcceptedWork) error {
	const funcName = "persistAcceptedWork"

	const stmt = `INSERT INTO acceptedwork(
			uuid, blockhash, prevhash, height, minedby, miner, createdon, confirmed
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8);`
	_, err := db.DB.Exec(stmt, work.UUID, work.BlockHash, work.PrevHash,
		work.Height, work.MinedBy, work.Miner, work.CreatedOn, work.Confirmed)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: work %s already exists", funcName,
					work.UUID)
				return dbError(ErrValueFound, desc)
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
	const stmt = `UPDATE acceptedwork SET
		blockhash=$2,
		prevhash=$3,
		height=$4,
		minedby=$5,
		miner=$6,
		createdon=$7,
		confirmed=$8
		WHERE uuid=$1;`
	result, err := db.DB.Exec(stmt,
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
		return dbError(ErrValueNotFound, desc)
	}

	return nil
}

// deleteAcceptedWork removes the associated accepted work from the database.
func (db *PostgresDB) deleteAcceptedWork(id string) error {
	const stmt = `DELETE FROM acceptedwork WHERE uuid=$1;`
	_, err := db.DB.Exec(stmt, id)
	return err
}

// listMinedWork returns work data associated with all blocks mined by the pool
// regardless of whether they are confirmed or not.
//
// List is ordered, most recent comes first.
func (db *PostgresDB) listMinedWork() ([]*AcceptedWork, error) {
	const stmt = `SELECT
		uuid, blockhash, prevhash, height, minedby, miner, createdon, confirmed
		 FROM acceptedwork
		 ORDER BY height DESC;`

	rows, err := db.DB.Query(stmt)
	if err != nil {
		return nil, err
	}

	return decodeWorkRows(rows)

}

// fetchUnconfirmedWork returns all work which is not confirmed as mined with
// height less than the provided height.
func (db *PostgresDB) fetchUnconfirmedWork(height uint32) ([]*AcceptedWork, error) {
	const stmt = `SELECT
		uuid, blockhash, prevhash, height, minedby, miner, createdon, confirmed
		FROM acceptedwork
		WHERE $1>height
		AND confirmed=false;`

	rows, err := db.DB.Query(stmt, height)
	if err != nil {
		return nil, err
	}

	return decodeWorkRows(rows)
}

// fetchJob fetches the job referenced by the provided id. Returns an error if
// the job is not found.
func (db *PostgresDB) fetchJob(id string) (*Job, error) {
	const funcName = "fetchJob"
	const stmt = `SELECT uuid, header, height FROM jobs WHERE uuid=$1;`
	var uuid, header string
	var height uint32
	err := db.DB.QueryRow(stmt, id).Scan(&uuid, &header, &height)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no job found for id %s", funcName, id)
			return nil, dbError(ErrValueNotFound, desc)
		}

		return nil, err
	}
	return &Job{uuid, height, header}, nil
}

// persistJob saves the job to the database. Returns an error if an account
// already exists with the same ID.
func (db *PostgresDB) persistJob(job *Job) error {
	const funcName = "persistJob"
	const stmt = `INSERT INTO jobs(uuid, height, header) VALUES ($1,$2,$3);`
	_, err := db.DB.Exec(stmt, job.UUID, job.Height, job.Header)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: job %s already exists", funcName,
					job.UUID)
				return dbError(ErrValueFound, desc)
			}
		}

		return err
	}
	return nil
}

// deleteJob removes the associated job from the database.
func (db *PostgresDB) deleteJob(id string) error {
	const stmt = `DELETE FROM jobs WHERE uuid=$1;`
	_, err := db.DB.Exec(stmt, id)
	return err
}

// deleteJobsBeforeHeight removes all jobs with heights less than the provided
// height.
func (db *PostgresDB) deleteJobsBeforeHeight(height uint32) error {
	const stmt = `DELETE FROM jobs WHERE height < $1;`
	_, err := db.DB.Exec(stmt, height)
	return err
}
