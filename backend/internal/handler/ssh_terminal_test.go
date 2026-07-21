package handler

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestSSHTerminalTicketIsNodeBoundAndSingleUse(t *testing.T) {
	store := newSSHTerminalTicketStore()
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	claims := authClaims{UserID: 7, Email: "admin@example.com", IsAdmin: true}

	token, expiresAt, err := store.issue(claims, 42)
	if err != nil {
		t.Fatalf("issue() error = %v", err)
	}
	if token == "" || !expiresAt.Equal(now.Add(sshTerminalTicketTTL)) {
		t.Fatalf("issue() = %q, %v", token, expiresAt)
	}
	ticket, ok := store.consume(token, 42)
	if !ok || ticket.NodeID != 42 || ticket.Claims.UserID != claims.UserID {
		t.Fatalf("consume() = %+v, %v", ticket, ok)
	}
	if _, ok := store.consume(token, 42); ok {
		t.Fatal("terminal ticket was accepted more than once")
	}
}

func TestSSHTerminalTicketRejectsWrongNodeAndExpiry(t *testing.T) {
	store := newSSHTerminalTicketStore()
	now := time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	claims := authClaims{UserID: 7, Email: "admin@example.com", IsAdmin: true}

	wrongNodeToken, _, err := store.issue(claims, 42)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.consume(wrongNodeToken, 41); ok {
		t.Fatal("node-bound ticket was accepted for another node")
	}
	if _, ok := store.consume(wrongNodeToken, 42); ok {
		t.Fatal("wrong-node attempt did not invalidate the ticket")
	}

	expiredToken, _, err := store.issue(claims, 42)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(sshTerminalTicketTTL)
	if _, ok := store.consume(expiredToken, 42); ok {
		t.Fatal("expired terminal ticket was accepted")
	}
}

func TestSSHTerminalSessionLimiterAppliesUserAndNodeLimits(t *testing.T) {
	limiter := newSSHTerminalSessionLimiter()
	releases := make([]func(), 0, sshTerminalMaxPerUser)
	for nodeID := uint(1); nodeID <= sshTerminalMaxPerUser; nodeID++ {
		release, ok := limiter.acquire(1, nodeID)
		if !ok {
			t.Fatalf("user session %d was unexpectedly rejected", nodeID)
		}
		releases = append(releases, release)
	}
	if _, ok := limiter.acquire(1, 99); ok {
		t.Fatal("per-user session limit was not enforced")
	}
	releases[0]()
	releases[0]()
	if release, ok := limiter.acquire(1, 99); !ok {
		t.Fatal("released user capacity was not reusable")
	} else {
		release()
	}

	nodeLimiter := newSSHTerminalSessionLimiter()
	first, ok := nodeLimiter.acquire(1, 9)
	if !ok {
		t.Fatal("first node session was rejected")
	}
	second, ok := nodeLimiter.acquire(2, 9)
	if !ok {
		t.Fatal("second node session was rejected")
	}
	if _, ok := nodeLimiter.acquire(3, 9); ok {
		t.Fatal("per-node session limit was not enforced")
	}
	first()
	second()
}

func TestSSHTerminalSameOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "http", host: "panel.example.com", origin: "http://panel.example.com", want: true},
		{name: "https with port", host: "panel.example.com:8443", origin: "https://panel.example.com:8443", want: true},
		{name: "missing origin", host: "panel.example.com", origin: "", want: false},
		{name: "foreign host", host: "panel.example.com", origin: "https://evil.example.com", want: false},
		{name: "unsupported scheme", host: "panel.example.com", origin: "file://panel.example.com", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "http://"+test.host+"/api/v1/nodes/1/ssh/terminal", nil)
			request.Host = test.host
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if got := sshTerminalSameOrigin(request); got != test.want {
				t.Fatalf("sshTerminalSameOrigin() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTerminalRemoteAddressDropsPort(t *testing.T) {
	if got := terminalRemoteAddress("192.0.2.8:52341"); got != "192.0.2.8" {
		t.Fatalf("terminalRemoteAddress() = %q", got)
	}
	if got := terminalRemoteAddress("[2001:db8::1]:22"); got != "2001:db8::1" {
		t.Fatalf("terminalRemoteAddress() = %q", got)
	}
}
