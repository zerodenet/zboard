package main

import (
	"os"
	"testing"
)

func TestLocalSigningIdentityIsReusedAndDamagedKeyIsNotReplaced(t *testing.T) {
	dir := t.TempDir()
	path, id, err := localSigningKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	next, nextID, err := localSigningKey(dir)
	if err != nil || path != next || id != nextID {
		t.Fatal("identity changed", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("key overwritten")
	}
	if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := localSigningKey(dir); err == nil {
		t.Fatal("corrupt key accepted")
	}
	after, _ = os.ReadFile(path)
	if string(after) != "damaged" {
		t.Fatal("damaged key silently replaced")
	}
}
