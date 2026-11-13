package businessref

import (
	"regexp"
	"testing"
	"time"
)

func TestBusinessReferencesRoundTripWithoutTruncation(t *testing.T) {
	for _, id := range []uint{1, 48, 9999999999, 10000000000, ^uint(0)} {
		for _, p := range []byte{'O', 'T'} {
			value := format(p, id)
			if !regexp.MustCompile(`^(DD|TK)[0-9]+$`).MatchString(value) {
				t.Fatal(value)
			}
			actual, ok := Parse(value, p)
			if !ok || actual != id {
				t.Fatal(value, actual, ok)
			}
		}
	}
	if Order(48) != "DD0000000048" || Ticket(12) != "TK0000000012" || Order(0) != "" {
		t.Fatal("public format changed")
	}
}
func TestBusinessReferencesRejectWrongDomainsAndUnsafeSyntax(t *testing.T) {
	for _, s := range []string{"", "0", "O0", "T0000000001", "O-1", "+1", "1e3", "1.5", "O1 2", "O18446744073709551616", "O００１", "O"} {
		if _, ok := Parse(s, 'O'); ok {
			t.Fatal("invalid reference accepted", s)
		}
	}
	for _, s := range []string{"48", "o0000000048", " O0000000048 "} {
		if n, ok := Parse(s, 'O'); !ok || n != 48 {
			t.Fatal(s, n, ok)
		}
	}
}
func TestProvisionalReferencesAreNumericAndSeparateFromFinalNamespace(t *testing.T) {
	seen := map[string]bool{}
	for range 128 {
		s, e := Provisional('O')
		if e != nil || !regexp.MustCompile(`^D[0-9]{29}$`).MatchString(s) || seen[s] {
			t.Fatal(s, e)
		}
		seen[s] = true
	}
}

func TestBusinessReferenceCreationTimestampAndSafePrefixes(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 45, 30, 0, time.UTC)
	for _, kind := range []byte{'O', 'T'} {
		no := format(kind, 48, at)
		if !regexp.MustCompile(`^(DD|TK)20261113174530000048$`).MatchString(no) {
			t.Fatal(no)
		}
		if id, ok := Parse(no, kind); !ok || id != 48 {
			t.Fatal(no, id, ok)
		}
		large := format(kind, ^uint(0), at)
		if id, ok := Parse(large, kind); !ok || id != ^uint(0) {
			t.Fatal(large, id, ok)
		}
	}
}
