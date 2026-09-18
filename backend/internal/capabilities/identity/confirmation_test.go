package identity

import (
	"strconv"
	"testing"
	"time"
)

func TestPasswordConfirmationBindsAccountPasswordAndSession(t *testing.T) {
	now := time.Unix(2000000000, 0)
	c := PasswordConfirmation{Key: []byte("test-key"), Now: func() time.Time { return now }}
	a := Account{ID: 7, PasswordHash: "current-password-hash"}
	auth := "Bearer session-one"
	expiry := strconv.FormatInt(now.Add(5*time.Minute).Unix(), 10)
	proof := expiry + "." + c.Digest(a.ID, a.PasswordHash, auth, expiry)
	if !c.Valid(a, auth, proof) {
		t.Fatal("valid confirmation rejected")
	}
	for _, tc := range []struct {
		account       Account
		authorization string
	}{
		{Account{ID: 8, PasswordHash: a.PasswordHash}, auth},
		{Account{ID: a.ID, PasswordHash: "changed"}, auth},
		{Account{ID: a.ID, PasswordHash: "!external"}, auth},
		{a, "Bearer session-two"}, {a, ""},
	} {
		if c.Valid(tc.account, tc.authorization, proof) {
			t.Fatal("confirmation crossed scope", tc)
		}
	}
	for _, offset := range []time.Duration{0, -time.Second, 301 * time.Second} {
		exp := strconv.FormatInt(now.Add(offset).Unix(), 10)
		if c.Valid(a, auth, exp+"."+c.Digest(a.ID, a.PasswordHash, auth, exp)) {
			t.Fatal("invalid lifetime accepted", offset)
		}
	}
	c.Key = nil
	if c.Valid(a, auth, proof) {
		t.Fatal("empty key accepted")
	}
}
