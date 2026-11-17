//go:build windows

package platformstore

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

func migrationFileIdentity(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil {
		return "", err
	}
	if info.FileIndexHigh == 0 && info.FileIndexLow == 0 {
		return "", errors.New("filesystem did not provide a stable Windows file index")
	}
	return fmt.Sprintf("windows:%d:%d:%d", info.VolumeSerialNumber, info.FileIndexHigh, info.FileIndexLow), nil
}
