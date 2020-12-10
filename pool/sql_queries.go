// Copyright (c) 2020 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

const (
	createTableMetadata = `
	CREATE TABLE IF NOT EXISTS metadata (
		key      TEXT PRIMARY KEY,
		value    TEXT NOT NULL
	);`

	createTableAccounts = `
	CREATE TABLE IF NOT EXISTS accounts (
		uuid      TEXT PRIMARY KEY,
		address   TEXT NOT NULL,
		createdon INT8 NOT NULL
	);`

	createTablePayments = `
	CREATE TABLE IF NOT EXISTS payments (
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

	createTableArchivedPayments = `
	CREATE TABLE IF NOT EXISTS archivedpayments (
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

	createTableJobs = `
	CREATE TABLE IF NOT EXISTS jobs (
		uuid   TEXT PRIMARY KEY,
		height INT8 NOT NULL,
		header TEXT NOT NULL
	);`

	createTableShares = `
	CREATE TABLE IF NOT EXISTS shares (
		uuid      TEXT PRIMARY KEY,
		account   TEXT NOT NULL,
		weight    TEXT NOT NULL,
		createdon INT8 NOT NULL
	);`

	createTableAcceptedWork = `
	CREATE TABLE IF NOT EXISTS acceptedwork (
		uuid      TEXT    PRIMARY KEY,
		blockhash TEXT    NOT NULL,
		prevhash  TEXT    NOT NULL,
		height    INT8    NOT NULL,
		minedby   TEXT    NOT NULL,
		miner     TEXT    NOT NULL,
		createdon INT8    NOT NULL,
		confirmed BOOLEAN NOT NULL
	);`

	createTableHashData = `
	CREATE TABLE IF NOT EXISTS hashdata (
		uuid      TEXT    PRIMARY KEY,
		accountid TEXT    NOT NULL,
		miner     TEXT    NOT NULL,
		ip        TEXT    NOT NULL,
		hashrate  TEXT    NOT NULL,
		updatedon INT8    NOT NULL
	);`

	purgeDB = `DROP TABLE IF EXISTS 
		acceptedwork, 
		accounts, 
		archivedpayments, 
		jobs, 
		metadata, 
		payments, 
		shares,
		hashdata;`

	selectPoolMode = `
	SELECT value
	FROM metadata
	WHERE key='poolmode';`

	insertPoolMode = `
	INSERT INTO metadata(key, value)
	VALUES ('poolmode', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	selectCSRFSecret = `
	SELECT value
	FROM metadata
	WHERE key='csrfsecret';`

	insertCSRFSecret = `
	INSERT INTO metadata(key, value)
	VALUES ('csrfsecret', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	selectLastPaymentHeight = `
	SELECT value
	FROM metadata
	WHERE key='lastpaymentheight';`

	insertLastPaymentHeight = `
	INSERT INTO metadata(key, value)
	VALUES ('lastpaymentheight', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	selectLastPaymentPaidOn = `
	SELECT value
	FROM metadata
	WHERE key='lastpaymentpaidon';`

	insertLastPaymentPaidOn = `
	INSERT INTO metadata(key, value)
	VALUES ('lastpaymentpaidon', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	selectLastPaymentCreatedOn = `
	SELECT value
	FROM metadata
	WHERE key='lastpaymentcreatedon';`

	insertLastPaymentCreatedOn = `
	INSERT INTO metadata(key, value)
	VALUES ('lastpaymentcreatedon', $1)
	ON CONFLICT (key)
	DO UPDATE SET value=$1;`

	insertAccount = `
	INSERT INTO accounts(
		uuid, address, createdon
	) VALUES ($1,$2,$3);`

	selectAccount = `
	SELECT
		uuid, address, createdon
	FROM accounts
	WHERE uuid=$1;`

	deleteAccount = `DELETE FROM accounts WHERE uuid=$1;`

	insertPayment = `
	INSERT INTO payments(
		uuid,
		account,
		estimatedmaturity,
		height,
		amount,
		createdon,
		paidonheight,
		transactionid,
		sourceblockhash,
		sourcecoinbase
	)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);`

	selectPayment = `
	SELECT
		uuid,
		account,
		estimatedmaturity,
		height,
		amount,
		createdon,
		paidonheight,
		transactionid,
		sourceblockhash,
		sourcecoinbase
	FROM payments
	WHERE uuid=$1;`

	deletePayment = `DELETE FROM payments WHERE uuid=$1;`

	updatePayment = `
	UPDATE payments
	SET
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

	insertArchivedPayment = `
	INSERT INTO archivedpayments(
		uuid,
		account,
		estimatedmaturity,
		height,
		amount,
		createdon,
		paidonheight,
		transactionid,
		sourceblockhash,
		sourcecoinbase
	)
	VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10);`

	selectPaymentsAtHeight = `
	SELECT
		uuid,
		account,
		estimatedmaturity,
		height,
		amount,
		createdon,
		paidonheight,
		transactionid,
		sourceblockhash,
		sourcecoinbase
	FROM payments
	WHERE paidonheight=0
	AND $1>(estimatedmaturity+1);`

	selectPendingPayments = `
	SELECT
		uuid,
		account,
		estimatedmaturity,
		height,
		amount,
		createdon,
		paidonheight,
		transactionid,
		sourceblockhash,
		sourcecoinbase
	FROM payments
	WHERE paidonheight=0;`

	countPaymentsAtBlockHash = `
	SELECT count(1)
	FROM payments
	WHERE paidonheight=0
	AND sourceblockhash=$1;`

	selectArchivedPayments = `
	SELECT
		uuid,
		account,
		estimatedmaturity,
		height,
		amount,
		createdon,
		paidonheight,
		transactionid,
		sourceblockhash,
		sourcecoinbase
	FROM archivedpayments
	ORDER BY height DESC;`

	selectMaturePendingPayments = `
	SELECT
		uuid,
		account,
		estimatedmaturity,
		height,
		amount,
		createdon,
		paidonheight,
		transactionid,
		sourceblockhash,
		sourcecoinbase
	FROM payments
	WHERE paidonheight=0
	AND (estimatedmaturity+1)<=$1;`

	selectShare = `
	SELECT
		uuid, account, weight, createdon
	FROM shares
	WHERE uuid=$1;`

	insertShare = `
	INSERT INTO shares(
		uuid, account, weight, createdon
	)
	VALUES ($1,$2,$3,$4);`

	selectSharesOnOrBeforeTime = `
	SELECT
		uuid, account, weight, createdon
	FROM shares
	WHERE createdon <= $1`

	selectSharesAfterTime = `
	SELECT
		uuid, account, weight, createdon
	FROM shares
	WHERE createdon > $1`

	deleteShareCreatedBefore = `DELETE FROM shares WHERE createdon < $1`

	selectAcceptedWork = `
	SELECT
		uuid,
		blockhash,
		prevhash,
		height,
		minedby,
		miner,
		createdon,
		confirmed
	FROM acceptedwork
	WHERE uuid=$1;`

	insertAcceptedWork = `
	INSERT INTO acceptedwork(
		uuid,
		blockhash,
		prevhash,
		height,
		minedby,
		miner,
		createdon,
		confirmed
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8);`

	updateAcceptedWork = `
	UPDATE acceptedwork
	SET
		blockhash=$2,
		prevhash=$3,
		height=$4,
		minedby=$5,
		miner=$6,
		createdon=$7,
		confirmed=$8
		WHERE uuid=$1;`

	deleteAcceptedWork = `DELETE FROM acceptedwork WHERE uuid=$1;`

	selectMinedWork = `
	SELECT
		uuid,
		blockhash,
		prevhash,
		height,
		minedby,
		miner,
		createdon,
		confirmed
	FROM acceptedwork
	ORDER BY height DESC;`

	selectUnconfirmedWork = `
	SELECT
		uuid,
		blockhash,
		prevhash,
		height,
		minedby,
		miner,
		createdon,
		confirmed
	FROM acceptedwork
	WHERE $1>height
	AND confirmed=false;`

	selectJob = `SELECT uuid, header, height FROM jobs WHERE uuid=$1;`

	insertJob = `INSERT INTO jobs(uuid, height, header) VALUES ($1,$2,$3);`

	deleteJob = `DELETE FROM jobs WHERE uuid=$1;`

	deleteJobBeforeHeight = `DELETE FROM jobs WHERE height < $1;`

	selectHashData = `SELECT 
		uuid, 
		accountid, 
		miner, 
		ip, 
		hashrate, 
		updatedon 
		FROM hashdata 
		WHERE uuid=$1;`

	listHashData = `SELECT 
		uuid, 
		accountid, 
		miner, 
		ip, 
		hashrate, 
		updatedon 
		FROM hashdata 
		WHERE updatedon > $1;`

	pruneHashData = `DELETE FROM hashdata WHERE updatedon < $1;`

	insertHashData = `INSERT INTO hashdata(
		uuid, 
		accountid, 
		miner, 
		ip, 
		hashrate, 
		updatedon) VALUES ($1,$2,$3, $4, $5, $6);`

	updateHashData = `
		UPDATE hashdata
		SET
			accountid=$2,
			miner=$3,
			ip=$4,
			hashrate=$5,
			updatedon=$6
			WHERE uuid=$1;`
)
