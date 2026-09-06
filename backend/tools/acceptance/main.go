// acceptance builds isolated fixtures and verifies load results. It is a test
// executable, never linked into or exposed by the panel's HTTP service.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type fixture struct {
	RunID          string                 `json:"run_id"`
	AdminEmail     string                 `json:"admin_email"`
	AdminPassword  string                 `json:"admin_password"`
	EncryptionKey  string                 `json:"encryption_key"`
	JWTSecret      string                 `json:"jwt_secret"`
	Nodes          []nodeIdentity         `json:"nodes"`
	Subscriptions  []subscriptionIdentity `json:"subscriptions"`
	HistoryRecords int                    `json:"history_records"`
}
type nodeIdentity struct {
	ID    uint   `json:"id"`
	Token string `json:"token"`
}
type subscriptionIdentity struct {
	ID           uint   `json:"id"`
	NodeID       uint   `json:"node_id"`
	Principal    string `json:"principal"`
	InitialBytes int64  `json:"initial_bytes"`
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}

func main() {
	mode := flag.String("mode", "seed", "seed, verify, sample-storage, profile-reads (isolated copy), or verify-spool (server stopped)")
	dir := flag.String("dir", "", "isolated test directory")
	nodes := flag.Int("nodes", 10, "fixture nodes")
	subs := flag.Int("subscriptions", 1000, "fixture subscriptions")
	records := flag.Int("records", 100000, "historical traffic records")
	flag.Parse()
	if *dir == "" {
		fail(fmt.Errorf("-dir is required"))
	}
	abs, err := filepath.Abs(*dir)
	if err != nil {
		fail(err)
	}
	switch *mode {
	case "seed":
		err = seed(abs, *nodes, *subs, *records)
	case "verify":
		err = verify(abs)
	case "verify-spool":
		err = verifySpool(abs)
	case "sample-storage":
		err = sampleStorage(abs)
	case "profile-reads":
		err = profileReads(abs)
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
