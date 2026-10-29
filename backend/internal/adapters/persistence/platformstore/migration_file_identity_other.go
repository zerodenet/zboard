//go:build !unix && !windows

package platformstore

import "errors"

func migrationFileIdentity(string) (string, error) {
	return "", errors.New("stable SQLite migration file identity is unsupported on this operating system")
}
