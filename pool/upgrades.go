package pool

import (
	"bytes"
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

	// DBVersion is the latest version of the database that is understood by the
	// program. Databases with recorded versions higher than this will fail to
	// open (meaning any upgrades prevent reverting to older software).
	DBVersion = paymentSourceVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.
var upgrades = [...]func(tx *bolt.Tx) error{
	transactionIDVersion - 1: transactionIDUpgrade,
	shareIDVersion - 1:       shareIDUpgrade,
	paymentSourceVersion - 1: paymentSourceUpgrade,
}

func fetchDBVersion(tx *bolt.Tx) (uint32, error) {
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return 0, MakeError(ErrBucketNotFound, desc, nil)
	}
	v := pbkt.Get(versionK)
	if v == nil {
		return 0, MakeError(ErrValueNotFound, "db version not set", nil)
	}

	return binary.LittleEndian.Uint32(v), nil
}

func setDBVersion(tx *bolt.Tx, newVersion uint32) error {
	pbkt := tx.Bucket(poolBkt)
	if poolBkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	vBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(vBytes, newVersion)
	err := pbkt.Put(versionK, vBytes)
	if err != nil {
		return err
	}

	return nil
}

func transactionIDUpgrade(tx *bolt.Tx) error {
	const oldVersion = 0
	const newVersion = 1

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := "transactionIDUpgrade inappropriately called"
		return MakeError(ErrDBUpgrade, desc, nil)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	// Update all entries in the payment and payment archive buckets.
	//
	// All transaction ids for payments before the upgrade will be set to
	// an empty string.

	pmtbkt := pbkt.Bucket(paymentBkt)
	if pmtbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	pmtCursor := pmtbkt.Cursor()
	for k, v := pmtCursor.First(); k != nil; k, v = pmtCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			return err
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			return err
		}

		err = pmtbkt.Put(k, pBytes)
		if err != nil {
			return err
		}
	}

	abkt := pbkt.Bucket(paymentArchiveBkt)
	if abkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentArchiveBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	aCursor := abkt.Cursor()
	for k, v := aCursor.First(); k != nil; k, v = aCursor.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			return err
		}

		pBytes, err := json.Marshal(payment)
		if err != nil {
			return err
		}

		err = abkt.Put(k, pBytes)
		if err != nil {
			return err
		}
	}

	return setDBVersion(tx, newVersion)
}

func shareIDUpgrade(tx *bolt.Tx) error {
	const oldVersion = 1
	const newVersion = 2

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := "shareIDUpgrade inappropriately called"
		return MakeError(ErrDBUpgrade, desc, nil)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	sbkt := pbkt.Bucket(shareBkt)
	if sbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(shareBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	id := func(account string, createdOn int64) string {
		buf := bytes.Buffer{}
		buf.Write(nanoToBigEndianBytes(createdOn))
		buf.WriteString(account)
		return hex.EncodeToString(buf.Bytes())
	}

	toDelete := [][]byte{}
	c := sbkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var share Share
		err := json.Unmarshal(v, &share)
		if err != nil {
			return err
		}

		createdOn := bigEndianBytesToNano(k)
		share.UUID = id(share.Account, int64(createdOn))

		sBytes, err := json.Marshal(share)
		if err != nil {
			return err
		}

		err = sbkt.Put([]byte(share.UUID), sBytes)
		if err != nil {
			return err
		}

		toDelete = append(toDelete, k)
	}

	for _, entry := range toDelete {
		err := sbkt.Delete(entry)
		if err != nil {
			return err
		}
	}

	return setDBVersion(tx, newVersion)
}

func paymentSourceUpgrade(tx *bolt.Tx) error {
	const oldVersion = 2
	const newVersion = 3

	dbVersion, err := fetchDBVersion(tx)
	if err != nil {
		return err
	}

	if dbVersion != oldVersion {
		desc := "paymentSourceUpgrade inappropriately called"
		return MakeError(ErrDBUpgrade, desc, nil)
	}

	pbkt := tx.Bucket(poolBkt)
	if pbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	// Update all entries in the payment and payment archive buckets.
	//
	// Payment sources for payments before the upgrade will have an empty
	// string for coinbase and block hash fields.

	pmtbkt := pbkt.Bucket(paymentBkt)
	if pmtbkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
	}

	zeroSource := &PaymentSource{}
	toDelete := [][]byte{}

	c := pmtbkt.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var payment Payment
		err := json.Unmarshal(v, &payment)
		if err != nil {
			return err
		}

		payment.Source = zeroSource

		pBytes, err := json.Marshal(payment)
		if err != nil {
			return err
		}

		key := paymentID(payment.Height, payment.CreatedOn, payment.Account)
		err = pmtbkt.Put(key, pBytes)
		if err != nil {
			return err
		}

		toDelete = append(toDelete, k)
	}

	for _, entry := range toDelete {
		err := pmtbkt.Delete(entry)
		if err != nil {
			return err
		}
	}

	abkt := pbkt.Bucket(paymentArchiveBkt)
	if abkt == nil {
		desc := fmt.Sprintf("bucket %s not found", string(paymentArchiveBkt))
		return MakeError(ErrBucketNotFound, desc, nil)
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
			return err
		}

		key := paymentID(payment.Height, payment.CreatedOn, payment.Account)
		err = abkt.Put(key, pBytes)
		if err != nil {
			return err
		}

		toDelete = append(toDelete, k)
	}

	for _, entry := range toDelete {
		err := abkt.Delete(entry)
		if err != nil {
			return err
		}
	}

	return setDBVersion(tx, newVersion)
}

// upgradeDB checks whether the any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func upgradeDB(db *bolt.DB) error {
	var version uint32
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if poolBkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}
		v := pbkt.Get(versionK)
		if v == nil {
			return MakeError(ErrValueNotFound, "db version not set", nil)
		}

		version = binary.LittleEndian.Uint32(v)

		return nil
	})

	if err != nil {
		return err
	}

	if version >= DBVersion {
		// No upgrades necessary.
		return nil
	}

	log.Infof("Upgrading database from version %d to %d", version, DBVersion)

	return db.Update(func(tx *bolt.Tx) error {
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
