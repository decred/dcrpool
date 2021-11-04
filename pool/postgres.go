// Copyright (c) 2020-2021 The Decred developers
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

	"github.com/decred/dcrd/dcrutil/v4"

	"github.com/lib/pq"

	errs "github.com/decred/dcrpool/errors"
)

// InitPostgresDB connects to the specified database and creates all tables
// required by dcrpool.
func InitPostgresDB(host string, port uint32, user, pass, dbName string, purgeDB bool) (*PostgresDB, error) {
	const funcName = "InitPostgresDB"

	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s "+
		"password=%s dbname=%s sslmode=disable",
		host, port, user, pass, dbName)

	db, err := sql.Open("postgres", psqlInfo)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to open postgres: %v", funcName, err)
		return nil, errs.DBError(errs.DBOpen, desc)
	}

	pdb := &PostgresDB{db}
	if purgeDB {
		err := pdb.purge()
		if err != nil {
			return nil, err
		}
	}

	// Send a Ping() to validate the db connection. This is because the Open()
	// func does not actually create a connection to the database, it just
	// validates the provided arguments.
	err = db.Ping()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to connect to postgres: %v", funcName, err)
		return nil, errs.DBError(errs.DBOpen, desc)
	}

	makeErr := func(table string, err error) error {
		desc := fmt.Sprintf("%s: unable to create %s table: %v",
			funcName, table, err)
		return errs.DBError(errs.CreateStorage, desc)
	}

	// Create all of the tables required by dcrpool.
	_, err = db.Exec(createTableMetadata)
	if err != nil {
		return nil, makeErr("metadata", err)
	}
	_, err = db.Exec(createTableAccounts)
	if err != nil {
		return nil, makeErr("accounts", err)
	}
	_, err = db.Exec(createTablePayments)
	if err != nil {
		return nil, makeErr("payments", err)
	}
	_, err = db.Exec(createTableArchivedPayments)
	if err != nil {
		return nil, makeErr("archived payments", err)
	}
	_, err = db.Exec(createTableJobs)
	if err != nil {
		return nil, makeErr("jobs", err)
	}
	_, err = db.Exec(createTableShares)
	if err != nil {
		return nil, makeErr("shares", err)
	}
	_, err = db.Exec(createTableAcceptedWork)
	if err != nil {
		return nil, makeErr("accepted work", err)
	}
	_, err = db.Exec(createTableHashData)
	if err != nil {
		return nil, makeErr("hashrate", err)
	}

	return &PostgresDB{db}, nil
}

// Close closes the postgres database connection.
func (db *PostgresDB) Close() error {
	funcName := "Close"
	err := db.DB.Close()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to close db: %v", funcName, err)
		return errs.DBError(errs.DBClose, desc)
	}
	return nil
}

// purge wipes all persisted data. This is intended for use with testnet and
// simnet testing purposes only.
func (db *PostgresDB) purge() error {
	funcName := "purge"
	_, err := db.DB.Exec(purgeDB)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to purge db: %v", funcName, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}

	return nil
}

// decodePaymentRows deserializes the provided SQL rows into a slice of Payment
// structs.
func decodePaymentRows(rows *sql.Rows) ([]*Payment, error) {
	const funcName = "decodePaymentRows"
	var toReturn []*Payment
	for rows.Next() {
		var uuid, account, transactionID, sourceBlockHash, sourceCoinbase string
		var estimatedMaturity, height, paidOnHeight uint32
		var amount, createdon int64
		err := rows.Scan(&uuid, &account, &estimatedMaturity,
			&height, &amount, &createdon, &paidOnHeight, &transactionID,
			&sourceBlockHash, &sourceCoinbase)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to scan payment entry: %v",
				funcName, err)
			return nil, errs.DBError(errs.Decode, desc)
		}

		payment := &Payment{uuid, account, estimatedMaturity,
			height, dcrutil.Amount(amount), createdon, paidOnHeight, transactionID,
			&PaymentSource{sourceBlockHash, sourceCoinbase}}
		toReturn = append(toReturn, payment)
	}

	err := rows.Err()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode payments: %v",
			funcName, err)
		return nil, errs.DBError(errs.Decode, desc)
	}

	return toReturn, nil
}

// decodeWorkRows deserializes the provided SQL rows into a slice of
// AcceptedWork structs.
func decodeWorkRows(rows *sql.Rows) ([]*AcceptedWork, error) {
	const funcName = "decodeWorkRows"
	var toReturn []*AcceptedWork
	for rows.Next() {
		var uuid, blockhash, prevhash, minedby, miner string
		var confirmed bool
		var height uint32
		var createdOn int64
		err := rows.Scan(&uuid, &blockhash, &prevhash, &height,
			&minedby, &miner, &createdOn, &confirmed)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to scan work entry: %v",
				funcName, err)
			return nil, errs.DBError(errs.Decode, desc)
		}

		work := &AcceptedWork{uuid, blockhash, prevhash, height,
			minedby, miner, createdOn, confirmed}
		toReturn = append(toReturn, work)
	}

	err := rows.Err()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode work: %v",
			funcName, err)
		return nil, errs.DBError(errs.Decode, desc)
	}

	return toReturn, nil
}

// decodeShareRows deserializes the provided SQL rows into a slice of Share
// structs.
func decodeShareRows(rows *sql.Rows) ([]*Share, error) {
	const funcName = "decodeShareRows"
	var toReturn []*Share
	for rows.Next() {
		var uuid, account, weight string
		var createdon int64
		err := rows.Scan(&uuid, &account, &weight, &createdon)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to scan share entry: %v",
				funcName, err)
			return nil, errs.DBError(errs.Decode, desc)
		}

		weightRat, ok := new(big.Rat).SetString(weight)
		if !ok {
			desc := fmt.Sprintf("unable to decode big.Rat string: %v", err)
			return nil, errs.DBError(errs.Parse, desc)
		}
		share := &Share{uuid, account, weightRat, createdon}
		toReturn = append(toReturn, share)
	}

	err := rows.Err()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode shares: %v",
			funcName, err)
		return nil, errs.DBError(errs.Decode, desc)
	}

	return toReturn, nil
}

// decodeHashDataRows deserializes the provided SQL rows into a slice of
// HashData structs.
func decodeHashDataRows(rows *sql.Rows) (map[string]*HashData, error) {
	const funcName = "decodeHashDataRows"

	toReturn := make(map[string]*HashData)
	for rows.Next() {
		var uuid, accountID, miner, ip, hashRate string
		var updatedOn int64
		err := rows.Scan(&uuid, &accountID, &miner, &ip,
			&hashRate, &updatedOn)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to scan hash data entry: %v",
				funcName, err)
			return nil, errs.DBError(errs.Decode, desc)
		}

		hashRat, ok := new(big.Rat).SetString(hashRate)
		if !ok {
			desc := fmt.Sprintf("%s: unable to decode big.Rat string: %v",
				funcName, err)
			return nil, errs.DBError(errs.Parse, desc)
		}

		hashData := &HashData{uuid, accountID, miner, ip, hashRat, updatedOn}
		toReturn[hashData.UUID] = hashData
	}

	err := rows.Err()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode hash data: %v",
			funcName, err)
		return nil, errs.DBError(errs.Decode, desc)
	}

	return toReturn, nil
}

// httpBackup is not implemented for postgres database.
func (db *PostgresDB) httpBackup(w http.ResponseWriter) error {
	return errs.DBError(errs.Unsupported, "httpBackup is not supported "+
		"for postgres database")
}

// Backup is not implemented for postgres database.
func (db *PostgresDB) Backup(fileName string) error {
	return errs.DBError(errs.Unsupported, "backup is not supported "+
		"for postgres database")
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
			return 0, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch pool mode: %v", funcName, err)
		return 0, errs.DBError(errs.FetchEntry, desc)
	}
	return poolmode, nil
}

// persistPoolMode stores the pool mode in the database. PoolMode is stored as a
// uint32 for historical reasons. 0 indicates Public, 1 indicates Solo.
func (db *PostgresDB) persistPoolMode(mode uint32) error {
	const funcName = "persistPoolMode"
	_, err := db.DB.Exec(insertPoolMode, mode)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to persist pool mode: %v",
			funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
}

// fetchCSRFSecret retrieves the bytes used for the CSRF secret from the database.
func (db *PostgresDB) fetchCSRFSecret() ([]byte, error) {
	const funcName = "fetchCSRFSecret"
	var secret string
	err := db.DB.QueryRow(selectCSRFSecret).Scan(&secret)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for csrfsecret", funcName)
			return nil, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch CSRF secret: %v",
			funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	decoded, err := hex.DecodeString(secret)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to decode csrf secret: %v",
			funcName, err)
		return nil, errs.DBError(errs.Parse, desc)
	}

	return decoded, nil
}

// persistCSRFSecret stores the bytes used for the CSRF secret in the database.
func (db *PostgresDB) persistCSRFSecret(secret []byte) error {
	const funcName = "persistCSRFSecret"
	_, err := db.DB.Exec(insertCSRFSecret, hex.EncodeToString(secret))
	if err != nil {
		desc := fmt.Sprintf("%s: unable to persist CSRF secrets: %v",
			funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return err
}

// persistLastPaymentInfo stores the last payment height and paidOn timestamp
// in the database.
func (db *PostgresDB) persistLastPaymentInfo(height uint32, paidOn int64) error {
	const funcName = "persistLastPaymentInfo"
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec(insertLastPaymentHeight, height)
	if err != nil {
		rErr := tx.Rollback()
		if rErr != nil {
			desc := fmt.Sprintf("%s: unable to rollback persisting last "+
				"payment height tx: %v, initial error: %v", funcName,
				rErr, err)
			return errs.DBError(errs.PersistEntry, desc)
		}

		desc := fmt.Sprintf("%s: unable to persist last payment height: %v",
			funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}

	_, err = tx.Exec(insertLastPaymentPaidOn, paidOn)
	if err != nil {
		rErr := tx.Rollback()
		if rErr != nil {
			desc := fmt.Sprintf("%s: unable to rollback persist last payment "+
				"paid on time tx: %v, initial error: %v", funcName,
				rErr, err)
			return errs.DBError(errs.PersistEntry, desc)
		}

		desc := fmt.Sprintf("%s: unable to persist last payment paid on "+
			"time: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
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
			return 0, 0, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to load last payment height: %v",
			funcName, err)
		return 0, 0, errs.DBError(errs.FetchEntry, desc)
	}

	var paidOn int64
	err = db.DB.QueryRow(selectLastPaymentPaidOn).Scan(&paidOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no value found for lastpaymentpaidon",
				funcName)
			return 0, 0, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to load last payment paid on "+
			"time: %v", funcName, err)
		return 0, 0, errs.DBError(errs.FetchEntry, desc)
	}

	return height, paidOn, nil
}

// persistLastPaymentCreatedOn stores the last payment createdOn timestamp in
// the database.
func (db *PostgresDB) persistLastPaymentCreatedOn(createdOn int64) error {
	const funcName = "persistLastPaymentCreatedOn"
	_, err := db.DB.Exec(insertLastPaymentCreatedOn, createdOn)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to persist last payment created "+
			"on time: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
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
			return 0, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to load last payment created "+
			"on time: %v", funcName, err)
		return 0, errs.DBError(errs.PersistEntry, desc)
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
				return errs.DBError(errs.ValueFound, desc)
			}
		}

		desc := fmt.Sprintf("%s: unable to persist account: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
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
			return nil, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch account with id (%s): %v",
			funcName, id, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}
	return &Account{uuid, address, createdOn}, nil
}

// deleteAccount purges the referenced account from the database.
func (db *PostgresDB) deleteAccount(id string) error {
	const funcName = "deleteAccount"
	_, err := db.DB.Exec(deleteAccount, id)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to delete account with id (%s): %v",
			funcName, id, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}
	return nil
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
			return nil, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch payment with id (%s): %v",
			funcName, id, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}
	return &Payment{uuid, account, estimatedMaturity, height,
		dcrutil.Amount(amount), createdOn, paidOnHeight, transactionID,
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
				return errs.DBError(errs.ValueFound, desc)
			}
		}

		desc := fmt.Sprintf("%s: unable to persist payment: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
}

// updatePayment persists the updated payment to the database.
func (db *PostgresDB) updatePayment(p *Payment) error {
	const funcName = "updatePayment"
	_, err := db.DB.Exec(updatePayment,
		p.UUID, p.Account, p.EstimatedMaturity, p.Height, p.Amount, p.CreatedOn,
		p.PaidOnHeight, p.TransactionID, p.Source.BlockHash, p.Source.Coinbase)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to update payment with id (%s): %v",
			funcName, p.UUID, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
}

// deletePayment purges the referenced payment from the database. Note that
// archived payments cannot be deleted.
func (db *PostgresDB) deletePayment(id string) error {
	const funcName = "deletePayment"
	_, err := db.DB.Exec(deletePayment, id)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to delete payment with id (%s): %v",
			funcName, id, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}
	return nil
}

// ArchivePayment removes the associated payment from active payments and archives it.
func (db *PostgresDB) ArchivePayment(p *Payment) error {
	const funcName = "ArchivePayment"

	tx, err := db.DB.Begin()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to begin payment archive tx: %v",
			funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}

	_, err = tx.Exec(deletePayment, p.UUID)
	if err != nil {
		rErr := tx.Rollback()
		if rErr != nil {
			desc := fmt.Sprintf("%s: unable to rollback payment deletion "+
				"tx %v, initial error: %v", funcName, rErr, err)
			return errs.DBError(errs.PersistEntry, desc)
		}

		desc := fmt.Sprintf("%s: unable to delete payment: %v", funcName, rErr)
		return errs.DBError(errs.DeleteEntry, desc)
	}

	aPmt := NewPayment(p.Account, p.Source, p.Amount, p.Height,
		p.EstimatedMaturity)
	aPmt.TransactionID = p.TransactionID
	aPmt.PaidOnHeight = p.PaidOnHeight

	_, err = tx.Exec(insertArchivedPayment,
		aPmt.UUID, aPmt.Account, aPmt.EstimatedMaturity, aPmt.Height, aPmt.Amount,
		aPmt.CreatedOn, aPmt.PaidOnHeight, aPmt.TransactionID, aPmt.Source.BlockHash,
		p.Source.Coinbase)
	if err != nil {
		rErr := tx.Rollback()
		if rErr != nil {
			desc := fmt.Sprintf("%s: unable to rollback archived payment "+
				"tx: %v, initial error: %v", funcName, rErr, err)
			return errs.DBError(errs.PersistEntry, desc)
		}

		desc := fmt.Sprintf("%s: unable to archive payment: %v", funcName, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}

	err = tx.Commit()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to commit archived payment tx: %v",
			funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
}

// fetchPaymentsAtHeight returns all payments sourcing from orphaned blocks at
// the provided height.
func (db *PostgresDB) fetchPaymentsAtHeight(height uint32) ([]*Payment, error) {
	const funcName = "fetchPaymentsAtHeight"
	rows, err := db.DB.Query(selectPaymentsAtHeight, height)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch payments at height %d: %v",
			funcName, height, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	return decodePaymentRows(rows)
}

// fetchPendingPayments fetches all unpaid payments.
func (db *PostgresDB) fetchPendingPayments() ([]*Payment, error) {
	const funcName = "fetchPendingPayments"
	rows, err := db.DB.Query(selectPendingPayments)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch pending payments: %v",
			funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	return decodePaymentRows(rows)
}

// pendingPaymentsForBlockHash returns the number of pending payments with the
// provided block hash as their source.
func (db *PostgresDB) pendingPaymentsForBlockHash(blockHash string) (uint32, error) {
	const funcName = "pendingPaymentsForBlockHash"
	var count uint32
	err := db.DB.QueryRow(countPaymentsAtBlockHash, blockHash).Scan(&count)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch pending payments for "+
			"blockhash (%s): %v", funcName, blockHash, err)
		return 0, errs.DBError(errs.FetchEntry, desc)
	}

	return count, nil
}

// archivedPayments fetches all archived payments. List is ordered, most
// recent comes first.
func (db *PostgresDB) archivedPayments() ([]*Payment, error) {
	const funcName = "archivedPayments"
	rows, err := db.DB.Query(selectArchivedPayments)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch archived payments: %v",
			funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	return decodePaymentRows(rows)
}

// maturePendingPayments fetches all mature pending payments at the
// provided height.
func (db *PostgresDB) maturePendingPayments(height uint32) (map[string][]*Payment, error) {
	const funcName = "maturePendingPayments"
	rows, err := db.DB.Query(selectMaturePendingPayments, height)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch mature pending payments: %v",
			funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
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
			return nil, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch share with id (%s): %v",
			funcName, id, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	weightRat, ok := new(big.Rat).SetString(weight)
	if !ok {
		desc := fmt.Sprintf("%s: unable to decode big.Rat string: %v",
			funcName, err)
		return nil, errs.DBError(errs.Parse, desc)
	}

	return &Share{uuid, account, weightRat, createdOn}, nil
}

// PersistShare saves a share to the database. Returns an error if a share
// already exists with the same ID.
func (db *PostgresDB) PersistShare(share *Share) error {
	const funcName = "PersistShare"

	_, err := db.DB.Exec(insertShare, share.UUID, share.Account,
		share.Weight.RatString(), share.CreatedOn)
	if err != nil {
		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: share %s already exists", funcName,
					share.UUID)
				return errs.DBError(errs.ValueFound, desc)
			}
		}

		desc := fmt.Sprintf("%s: unable to persist share: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
}

// ppsEligibleShares fetches all shares created before or at the provided time.
func (db *PostgresDB) ppsEligibleShares(max int64) ([]*Share, error) {
	const funcName = "ppsEligibleShares"
	rows, err := db.DB.Query(selectSharesOnOrBeforeTime, max)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch PPS eligible shares: %v",
			funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	return decodeShareRows(rows)
}

// pplnsEligibleShares fetches all shares created after the provided time.
func (db *PostgresDB) pplnsEligibleShares(min int64) ([]*Share, error) {
	const funcName = "pplnsEligibleShares"
	rows, err := db.DB.Query(selectSharesAfterTime, min)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch PPLNS eligible shares: %v",
			funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	return decodeShareRows(rows)
}

// pruneShares removes shares with a createdOn time earlier than the provided
// time.
func (db *PostgresDB) pruneShares(minNano int64) error {
	const funcName = "pruneShares"
	_, err := db.DB.Exec(deleteShareCreatedBefore, minNano)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to prune shares: %v", funcName, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}
	return nil
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
			return nil, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch accepted work with id "+
			"(%s): %v", funcName, id, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
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
				return errs.DBError(errs.ValueFound, desc)
			}
		}

		desc := fmt.Sprintf("%s: unable to persist accepted work: %v",
			funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
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
		desc := fmt.Sprintf("%s: unable to update accepted work with id "+
			"(%s): %v", funcName, work.UUID, err)
		return errs.DBError(errs.PersistEntry, desc)
	}

	if rowsAffected == 0 {
		desc := fmt.Sprintf("%s: work %s not found", funcName, work.UUID)
		return errs.DBError(errs.ValueNotFound, desc)
	}

	return nil
}

// deleteAcceptedWork removes the associated accepted work from the database.
func (db *PostgresDB) deleteAcceptedWork(id string) error {
	const funcName = "deleteAcceptedWork"
	_, err := db.DB.Exec(deleteAcceptedWork, id)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to delete account work with id "+
			"(%s): %v", funcName, id, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}
	return nil
}

// listMinedWork returns work data associated with all blocks mined by the pool
// regardless of whether they are confirmed or not.
//
// List is ordered, most recent comes first.
func (db *PostgresDB) listMinedWork() ([]*AcceptedWork, error) {
	const funcName = "listMinedWork"
	rows, err := db.DB.Query(selectMinedWork)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch mined work: %v", funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	return decodeWorkRows(rows)
}

// fetchUnconfirmedWork returns all work which is not confirmed as mined with
// height less than the provided height.
func (db *PostgresDB) fetchUnconfirmedWork(height uint32) ([]*AcceptedWork, error) {
	const funcName = "fetchUnconfirmedWork"
	rows, err := db.DB.Query(selectUnconfirmedWork, height)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to fetch unconfirmed work: %v", funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
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
			return nil, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch job with id (%s): %v",
			funcName, id, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
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
				return errs.DBError(errs.ValueFound, desc)
			}
		}

		desc := fmt.Sprintf("%s: unable to persist job: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
}

// deleteJob removes the associated job from the database.
func (db *PostgresDB) deleteJob(id string) error {
	const funcName = "deleteJob"
	_, err := db.DB.Exec(deleteJob, id)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to delete job entry with id "+
			"(%s): %v", funcName, id, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}
	return nil
}

// deleteJobsBeforeHeight removes all jobs with heights less than the provided
// height.
func (db *PostgresDB) deleteJobsBeforeHeight(height uint32) error {
	const funcName = "deleteJobsBeforeHeight"
	_, err := db.DB.Exec(deleteJobBeforeHeight, height)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to delete jobs before height %d: %v",
			funcName, height, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}
	return err
}

// persistHashData saves the provided hash data to the database.
func (db *PostgresDB) persistHashData(hashData *HashData) error {
	const funcName = "persistHashData"

	_, err := db.DB.Exec(insertHashData, hashData.UUID, hashData.AccountID,
		hashData.Miner, hashData.IP, hashData.HashRate.RatString(),
		hashData.UpdatedOn)
	if err != nil {

		var pqError *pq.Error
		if errors.As(err, &pqError) {
			if pqError.Code.Name() == "unique_violation" {
				desc := fmt.Sprintf("%s: hash rate %s already exists", funcName,
					hashData.UUID)
				return errs.DBError(errs.ValueFound, desc)
			}
		}

		desc := fmt.Sprintf("%s: unable to persist hash rate: %v", funcName, err)
		return errs.DBError(errs.PersistEntry, desc)
	}
	return nil
}

// updateHashData persists the updated hash data to the database.
func (db *PostgresDB) updateHashData(hashData *HashData) error {
	const funcName = "updateHashData"

	result, err := db.DB.Exec(updateHashData,
		hashData.UUID, hashData.AccountID, hashData.Miner,
		hashData.IP, hashData.HashRate.RatString(), hashData.UpdatedOn)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		desc := fmt.Sprintf("%s: unable to update accepted hash data with id "+
			"(%s): %v", funcName, hashData.UUID, err)
		return errs.DBError(errs.PersistEntry, desc)
	}

	if rowsAffected == 0 {
		desc := fmt.Sprintf("%s: hash data %s not found", funcName, hashData.UUID)
		return errs.DBError(errs.ValueNotFound, desc)
	}

	return nil
}

// fetchHashData fetches the hash data associated with the provided id.
func (db *PostgresDB) fetchHashData(id string) (*HashData, error) {
	const funcName = "fetchHashData"
	var uuid, accountID, miner, ip, hashRate string
	var updatedOn int64
	err := db.DB.QueryRow(selectHashData, id).Scan(&uuid, &accountID, &miner,
		&ip, &hashRate, &updatedOn)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			desc := fmt.Sprintf("%s: no hash data found for id %s", funcName, id)
			return nil, errs.DBError(errs.ValueNotFound, desc)
		}

		desc := fmt.Sprintf("%s: unable to fetch hash data with id (%s): %v",
			funcName, id, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	hashRat, ok := new(big.Rat).SetString(hashRate)
	if !ok {
		desc := fmt.Sprintf("%s: unable to decode big.Rat string: %v",
			funcName, err)
		return nil, errs.DBError(errs.Parse, desc)
	}

	return &HashData{uuid, accountID, miner, ip, hashRat, updatedOn}, nil
}

// listHashData fetches all hash data updated after the provided minimum time.
func (db *PostgresDB) listHashData(minNano int64) (map[string]*HashData, error) {
	const funcName = "listHashData"
	rows, err := db.DB.Query(listHashData, minNano)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to list hash data: %v",
			funcName, err)
		return nil, errs.DBError(errs.FetchEntry, desc)
	}

	hashData, err := decodeHashDataRows(rows)
	if err != nil {
		return nil, err
	}

	return hashData, nil
}

// pruneHashData prunes all hash data that have not been updated since
// the provided minimum time.
func (db *PostgresDB) pruneHashData(minNano int64) error {
	const funcName = "pruneHashData"

	_, err := db.DB.Exec(pruneHashData, minNano)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to prune hash data: %v", funcName, err)
		return errs.DBError(errs.DeleteEntry, desc)
	}

	return nil
}
