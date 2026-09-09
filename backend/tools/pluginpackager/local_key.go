package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Local builds retain one identity outside the source tree. Official release
// automation continues to supply its protected publisher key explicitly.
func localSigningKey(directory string) (string, string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", "", err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("unsafe local signing directory")
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return "", "", err
	}
	path := filepath.Join(directory, "publisher.key")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		_, key, genErr := ed25519.GenerateKey(rand.Reader)
		if genErr != nil {
			file.Close()
			os.Remove(path)
			return "", "", genErr
		}
		_, err = file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
		err = errors.Join(err, file.Close())
		if err != nil {
			os.Remove(path)
			return "", "", err
		}
	} else if !os.IsExist(err) {
		return "", "", err
	}
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", "", errors.New("local private key must be a regular private file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return "", "", errors.New("invalid local private key; refusing replacement")
	}
	if !bytes.Equal(ed25519.NewKeyFromSeed(key[:32]), key) {
		return "", "", errors.New("inconsistent local private key")
	}
	fingerprint := sha256.Sum256(key[32:])
	return path, "local-" + hex.EncodeToString(fingerprint[:16]), nil
}
