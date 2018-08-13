package database

import (
	"encoding/binary"
	"fmt"

	bolt "github.com/coreos/bbolt"
)

const (
	initialVersion = 0

	// DBVersion is the latest version of the database that is understood by the
	// program. Databases with recorded versions higher than this will fail to
	// open (meaning any upgrades prevent reverting to older software).
	DBVersion = initialVersion
)

// upgrades maps between old database versions and the upgrade function to
// upgrade the database to the next version.
var upgrades = [...]func(tx *bolt.Tx) error{}

// Upgrade checks whether the any upgrades are necessary before the database is
// ready for application usage.  If any are, they are performed.
func Upgrade(db *bolt.DB) error {
	var version uint32
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolbkt)
		if poolbkt == nil {
			return fmt.Errorf("'%s' bucket does not exist", string(poolbkt))
		}
		v := pbkt.Get(versionk)
		if v == nil {
			return fmt.Errorf("associated value for '%s' not found",
				string(versionk))
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

	return db.View(func(tx *bolt.Tx) error {
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
