package pool

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

const (
	initialVersion = 0

	// TransacionIDVersion is the second version of the database. It adds the
	// transactionId field to the payments struct for payment tracking purposes.
	transactionIDVersion = 1

	// shareIDVersion is the second version of the database. It updates
	// the share id key and removes the created on time field.
	shareIDVersion = 2

	// paymentSourceVersion is the third version of the database. It adds
	// payment source tracking to payments.
	paymentSourceVersion = 3

	// removeTxfeeReserveVersion is the fourth version of the database.
	// It removes the tx fee reserve from the database.
	removeTxFeeReserveVersion = 4

	// shareCreatedOnVersion is the fifth version of the database.
	// It restores the created on time field for shares.
	shareCreatedOnVersion = 5

	// paymentUUIDVersion is the sixth version of the database.
	// It adds the UUID field to payments.
	paymentUUIDVersion = 6

	// LatestBoltDBVersion is the latest version of the bolt database that is
	// understood by the program. Databases with recorded versions higher than
	// this will fail to open (meaning any upgrades prevent reverting to older
	// software).
	LatestBoltDBVersion = paymentUUIDVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.
var upgrades = [...]func(tx *bolt.Tx) error{
	transactionIDVersion - 1:      transactionIDUpgrade,
	shareIDVersion - 1:            shareIDUpgrade,
	paymentSourceVersion - 1:      paymentSourceUpgrade,
	removeTxFeeReserveVersion - 1: removeTxFeeReserveUpgrade,
	shareCreatedOnVersion - 1:     shareCreatedOnUpgrade,
	paymentUUIDVersion - 1:        paymentUUIDUpgrade,
}

func fetchDBVersion(tx *bolt.Tx) (uint32, error) {
	const funcName = "fetchDBVersion"
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return 0, dbError(ErrBucketNotFound, desc)
	}
	v := pbkt.Get(versionK)
	if v == nil {
		desc := fmt.Sprintf("%s: db version not set", funcName)
		return 0, dbError(ErrValueNotFound, desc)
	}

	return binary.LittleEndian.Uint32(v), nil
}

func setDBVersion(tx *bolt.Tx, newVersion uint32) error {
	const funcName = "setDBVersion"
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	vBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(vBytes, newVersion)
	err := pbkt.Put(versionK, vBytes)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to persist version: %v", funcName, err)
		return dbError(ErrPersistEntry, desc)
	}

	return nil
}

func transactionIDUpgrade(tx *bolt.Tx) error {
	const oldVersion = 0
	const newVersion = 1

	const funcName = "transactionIDUpgrade"

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := fmt.Sprintf("%s: inappropriately called", funcName)
		return dbError(ErrDBUpgrade, desc)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	// Update all entries in the payment and payment archive buckets.
	//
	// All transaction ids for payments before the upgrade will be set to
	// an empty string.

	pmtbkt := pbkt.Bucket(paymentBkt)
	if pmtbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	pmtCursor := pmtbkt.Cursor()
	for k, v := pmtCursor.First(); k != nil; k, v = pmtCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		err = pmtbkt.Put(k, pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
	}

	abkt := pbkt.Bucket(paymentArchiveBkt)
	if abkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentArchiveBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	aCursor := abkt.Cursor()
	for k, v := aCursor.First(); k != nil; k, v = aCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		err = abkt.Put(k, pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
	}

	return setDBVersion(tx, newVersion)
}

func shareIDUpgrade(tx *bolt.Tx) error {
	const oldVersion = 1
	const newVersion = 2

	const funcName = "shareIDUpgrade"

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := fmt.Sprintf("%s: inappropriately called", funcName)
		return dbError(ErrDBUpgrade, desc)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	sbkt := pbkt.Bucket(shareBkt)
	if sbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(shareBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	toDelete := [][]byte{}
	c := sbkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var share Share
		err := json.Unmarshal(v, &share)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal share: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		createdOn := bigEndianBytesToNano(k)
		share.UUID = shareID(share.Account, int64(createdOn))

		sBytes, err := json.Marshal(share)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal share bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		err = sbkt.Put([]byte(share.UUID), sBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist share: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}

		toDelete = append(toDelete, k)
	}

	for _, entry := range toDelete {
		err := sbkt.Delete(entry)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to delete share: %v",
				funcName, err)
			return dbError(ErrDeleteEntry, desc)
		}
	}

	return setDBVersion(tx, newVersion)
}

func paymentSourceUpgrade(tx *bolt.Tx) error {
	const oldVersion = 2
	const newVersion = 3

	const funcName = "paymentSourceUpgrade"

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := fmt.Sprintf("%s: inappropriately called", funcName)
		return dbError(ErrDBUpgrade, desc)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	// Update all entries in the payment and payment archive buckets.
	//
	// Payment sources for payments before the upgrade will have an empty
	// string for coinbase and block hash fields.

	pmtbkt := pbkt.Bucket(paymentBkt)
	if pmtbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	zeroSource := &PaymentSource{}
	toDelete := [][]byte{}

	c := pmtbkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		payment.Source = zeroSource

		pBytes, err := json.Marshal(payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		key := paymentID(payment.Height, payment.CreatedOn, payment.Account)
		err = pmtbkt.Put([]byte(key), pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}

		toDelete = append(toDelete, k)
	}

	for _, entry := range toDelete {
		err := pmtbkt.Delete(entry)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to delete payment: %v",
				funcName, err)
			return dbError(ErrDeleteEntry, desc)
		}
	}

	abkt := pbkt.Bucket(paymentArchiveBkt)
	if abkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentArchiveBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	toDelete = [][]byte{}

	c = abkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			return err
		}

		payment.Source = zeroSource

		pBytes, err := json.Marshal(payment)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		key := paymentID(payment.Height, payment.CreatedOn, payment.Account)
		err = abkt.Put([]byte(key), pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}

		toDelete = append(toDelete, k)
	}

	for _, entry := range toDelete {
		err := abkt.Delete(entry)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to delete payment: %v",
				funcName, err)
			return dbError(ErrDeleteEntry, desc)
		}
	}

	return setDBVersion(tx, newVersion)
}

func removeTxFeeReserveUpgrade(tx *bolt.Tx) error {
	const oldVersion = 3
	const newVersion = 4

	const funcName = "removeTxFeeReserveUpgrade"

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := fmt.Sprintf("%s: inappropriately called", err)
		return dbError(ErrDBUpgrade, desc)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	err = pbkt.Delete([]byte("txfeereserve"))
	if err != nil {
		desc := fmt.Sprintf("%s: unable to remove tx fee reserve entry",
			funcName)
		return dbError(ErrDeleteEntry, desc)
	}

	return setDBVersion(tx, newVersion)
}

func shareCreatedOnUpgrade(tx *bolt.Tx) error {
	const oldVersion = 4
	const newVersion = 5

	const funcName = "shareCreatedOnUpgrade"

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := fmt.Sprintf("%s: inappropriately called", err)
		return dbError(ErrDBUpgrade, desc)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	sbkt := pbkt.Bucket(shareBkt)
	if sbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(shareBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	c := sbkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var share Share
		err := json.Unmarshal(v, &share)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal share: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		createdOnB := make([]byte, 8)
		_, err = hex.Decode(createdOnB, k[:16])
		if err != nil {
			desc := fmt.Sprintf("%s: unable to decode share "+
				"created-on bytes: %v", funcName, err)
			return dbError(ErrDecode, desc)
		}

		createdOn := bigEndianBytesToNano(createdOnB)
		share.CreatedOn = int64(createdOn)

		sBytes, err := json.Marshal(share)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal share bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		err = sbkt.Put([]byte(share.UUID), sBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist share: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
	}

	return setDBVersion(tx, newVersion)
}

func paymentUUIDUpgrade(tx *bolt.Tx) error {
	const oldVersion = 5
	const newVersion = 6

	const funcName = "paymentUUIDUpgrade"

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := fmt.Sprintf("%s: inappropriately called", err)
		return dbError(ErrDBUpgrade, desc)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(poolBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	pmtbkt := pbkt.Bucket(paymentBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	c := pmtbkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var pmt Payment
		err := json.Unmarshal(v, &pmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		UUID := string(k)
		pmt.UUID = UUID

		pBytes, err := json.Marshal(pmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		err = pmtbkt.Put(k, pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
	}

	abkt := pbkt.Bucket(paymentArchiveBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("%s: bucket %s not found", funcName,
			string(paymentArchiveBkt))
		return dbError(ErrBucketNotFound, desc)
	}

	c = abkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var pmt Payment
		err := json.Unmarshal(v, &pmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to unmarshal payment: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		pmt.UUID = string(k)

		pBytes, err := json.Marshal(pmt)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to marshal payment bytes: %v",
				funcName, err)
			return dbError(ErrParse, desc)
		}

		err = abkt.Put(k, pBytes)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to persist payment: %v",
				funcName, err)
			return dbError(ErrPersistEntry, desc)
		}
	}

	return setDBVersion(tx, newVersion)
}

// upgradeDB checks whether any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func upgradeDB(db *BoltDB) error {
	var version uint32
	err := db.DB.View(func(tx *bolt.Tx) error {
		var err error
		version, err = fetchDBVersion(tx)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	if version >= LatestBoltDBVersion {
		// No upgrades necessary.
		return nil
	}

	log.Infof("Upgrading database from version %d to %d", version, LatestBoltDBVersion)

	return db.DB.Update(func(tx *bolt.Tx) error {
		// Execute all necessary upgrades in order.
		for _, upgrade := range upgrades[version:] {
			err := upgrade(tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
}
