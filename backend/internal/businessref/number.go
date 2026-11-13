// Package businessref defines stable, human-facing business references. Numeric
// IDs remain storage keys, not credentials; every lookup must retain ownership.
package businessref

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// DD means order; TK means support ticket. Only D, T and K participate in
// generated prefixes: none can be confused with a decimal digit.
func Order(id uint, created ...time.Time) string  { return format('O', id, created...) }
func Ticket(id uint, created ...time.Time) string { return format('T', id, created...) }
func numberPrefix(kind byte) string {
	if kind == 'O' {
		return "DD"
	}
	if kind == 'T' {
		return "TK"
	}
	return string(kind)
}

var numberZone = time.FixedZone("UTC+8", 8*60*60)

func format(kind byte, id uint, created ...time.Time) string {
	if id == 0 {
		return ""
	}
	if len(created) > 0 && !created[0].IsZero() {
		return fmt.Sprintf("%s%s%06d", numberPrefix(kind), created[0].In(numberZone).Format("20060102150405"), id)
	}
	// Short aliases keep ID-only historical references usable. Full entity views
	// always pass their immutable creation time, never the display/current clock.
	return fmt.Sprintf("%s%010d", numberPrefix(kind), id)
}

// Parse accepts the public reference (case-insensitive) or an existing numeric
// ID. It rejects signs, exponent notation, fractions, overflow and other domains.
func Parse(value string, prefix byte) (uint, bool) {
	s := strings.TrimSpace(value)
	upper := strings.ToUpper(s)
	canonical := numberPrefix(prefix)
	if strings.HasPrefix(upper, canonical) {
		s = s[len(canonical):]
		if len(s) >= 20 {
			if _, e := time.Parse("20060102150405", s[:14]); e == nil {
				s = s[14:]
			}
		}
	} else if len(s) > 0 && (s[0] == prefix || s[0] == prefix+('a'-'A')) {
		// Read-only compatibility for links copied during the earlier implementation.
		s = s[1:]
	}
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, e := strconv.ParseUint(s, 10, strconv.IntSize)
	return uint(n), e == nil && n > 0
}

// Provisional is private to an uncommitted INSERT, before the database allocates
// its primary key. The same transaction replaces it with the final reference.
// It needs no global sequence, clock, distributed lock, or retained extra row.
func Provisional(prefix byte) (string, error) {
	// Temporary storage identifiers also avoid ambiguous letters, even though they
	// never leave the insertion transaction or become public business numbers.
	if prefix == 'O' {
		prefix = 'D'
	}
	if prefix == 'T' {
		prefix = 'K'
	}
	ceiling := new(big.Int).Exp(big.NewInt(10), big.NewInt(29), nil)
	n, e := rand.Int(rand.Reader, ceiling)
	if e != nil {
		return "", e
	}
	return fmt.Sprintf("%c%029s", prefix, n.String()), nil
}
