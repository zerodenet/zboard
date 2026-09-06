//go:build !linux

package main

func diskFreeBytes(string) (*uint64, error) {
	return nil, nil // Disk pressure acceptance runs on Linux, never inferred on a host.
}
