//go:build unix

package platformstore

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func migrationFileIdentity(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return "", err
	}
	if stat.Ino == 0 {
		return "", errors.New("filesystem did not provide a stable file inode")
	}
	return fmt.Sprintf("unix:%d:%d", stat.Dev, stat.Ino), nil
}
