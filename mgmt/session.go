package mgmt

import (
	"time"

	"github.com/segmentio/ksuid"
)

// Session represents a user authenticated mining session. The mining pool will
// keep one active session per account.
type Session struct {
	UUID       string `json:"uuid"`
	AccountID  string `json:"accountid"`
	CreatedOn  uint64 `json:"createdon"`
	ModifiedOn uint64 `json:"modifiedon"`
	ExpiresOn  uint64 `json:"expireson"`
}

// futureTime extends the provided base time to a  in the future.
func futureTime(date *time.Time, days time.Duration, hours time.Duration, minutes time.Duration, seconds time.Duration) time.Time {
	duration := ((time.Hour * 24) * days) + (time.Hour * hours) +
		(time.Minute * minutes) + (time.Second * seconds)
	return date.Add(duration)
}

// NewSession creates a session for the provided account.
func NewSession(account *Account) (*Session, error) {
	id, err := ksuid.NewRandom()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	expiry := futureTime(&now, 0, 6, 0, 0)
	session := &Session{
		UUID:       id.String(),
		AccountID:  account.UUID,
		CreatedOn:  uint64(now.Unix()),
		ModifiedOn: 0,
		ExpiresOn:  uint64(expiry.Unix()),
	}
	return session, nil
}

// ExtendSession prolongs the expiry time of the session by an hour.
func (session *Session) ExtendSession() {
	currExpiry := time.Unix(int64(session.ExpiresOn), 0)
	futureExpiry := futureTime(&currExpiry, 0, 1, 0, 0)
	session.ExpiresOn = uint64(futureExpiry.Unix())
}
