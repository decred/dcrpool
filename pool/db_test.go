package pool

import (
	"os"
	"testing"

	bolt "github.com/coreos/bbolt"
)

func testDatabase(t *testing.T, db *bolt.DB) {
	// Persist some accounts.
	accountA, err := persistAccount(db, "DsmQrsCimQ8QBXndN5xLYTRZpWdZpGJNpsh")
	if err != nil {
		t.Fatal(err)
	}

	_, err = persistAccount(db, "DsornJn4i4cbgQJF3sCQjNUGDi7HZrYcVcc")
	if err != nil {
		t.Fatal(err)
	}

	// Empty the accounts bucket.
	err = emptyBucket(db, accountBkt)
	if err != nil {
		t.Fatalf("emptyBucket error: %v", err)
	}

	// Ensure the accounts bucket is empty.
	_, err = FetchAccount(db, []byte(accountA.UUID))
	if err == nil {
		t.Fatalf("expected no value found error")
	}

	// Create a database backup.
	backupFile := "backup.db"
	defer func() {
		os.Remove(backupFile)
	}()

	err = backup(db, backupFile)
	if err != nil {
		t.Fatalf("backup error: %v", err)
	}
}
