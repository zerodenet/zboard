package zero

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const OfflineArtifactMaxBytes = 128 << 20
const offlineStoreMaxBytes = 1 << 30

var offlineArtifactID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type OfflineArtifactStore struct{ Root string }

// Import accepts a Linux x86_64 executable or a bounded official tar.gz archive.
// Uploaded code is never executed on the panel host. Content-addressed files are
// retained for persisted task retries across restarts, with a 1 GiB store limit.
func (s OfflineArtifactStore) Import(ctx context.Context, payload []byte) (string, int64, error) {
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	binary, err := offlineBinary(payload)
	if err != nil {
		return "", 0, err
	}
	digest := sha256.Sum256(binary)
	id := hex.EncodeToString(digest[:])
	if strings.TrimSpace(s.Root) == "" {
		return "", 0, errors.New("offline artifact storage is not configured")
	}
	if err := os.MkdirAll(s.Root, 0700); err != nil {
		return "", 0, fmt.Errorf("prepare offline artifact storage: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(s.Root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", 0, err
	}
	defer lock.Close()
	for {
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return "", 0, err
		}
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	target := filepath.Join(s.Root, id+".bin")
	if _, err := os.Lstat(target); err == nil {
		if _, err := s.Load(ctx, id); err != nil {
			return "", 0, err
		}
		return id, int64(len(binary)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", 0, err
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return "", 0, err
	}
	var size int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return "", 0, err
		}
		size += info.Size()
	}
	if size+int64(len(binary)) > offlineStoreMaxBytes {
		return "", 0, errors.New("offline artifact storage is full (1 GiB); remove unused artifacts before uploading")
	}
	file, err := os.CreateTemp(s.Root, ".upload-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(binary); err != nil {
		file.Close()
		return "", 0, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", 0, err
	}
	if err := file.Close(); err != nil {
		return "", 0, err
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(file.Name(), target); err != nil {
		return "", 0, err
	}
	// Ensure the task never references an artifact whose directory entry was not flushed.
	dir, err := os.Open(s.Root)
	if err != nil {
		return "", 0, err
	}
	syncErr := dir.Sync()
	dir.Close()
	if syncErr != nil {
		return "", 0, syncErr
	}
	return id, int64(len(binary)), nil
}

func (s OfflineArtifactStore) Load(ctx context.Context, id string) ([]byte, error) {
	if strings.TrimSpace(s.Root) == "" || !offlineArtifactID.MatchString(id) {
		return nil, errors.New("invalid offline artifact reference")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := filepath.Join(s.Root, id+".bin")
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > OfflineArtifactMaxBytes {
		return nil, errors.New("invalid stored offline artifact")
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	binary, err := io.ReadAll(io.LimitReader(file, OfflineArtifactMaxBytes+1))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(binary)
	if int64(len(binary)) != info.Size() || hex.EncodeToString(digest[:]) != id {
		return nil, errors.New("offline artifact SHA-256 mismatch")
	}
	if err := validateOfflineELF(binary); err != nil {
		return nil, err
	}
	return binary, ctx.Err()
}

func offlineBinary(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > OfflineArtifactMaxBytes {
		return nil, errors.New("kernel file must be between 1 byte and 128 MiB")
	}
	binary := payload
	if bytes.HasPrefix(payload, []byte{0x1f, 0x8b}) {
		gz, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		// Bound total expansion, including unrelated archive entries.
		limited := &io.LimitedReader{R: gz, N: 2*OfflineArtifactMaxBytes + 1}
		archive := tar.NewReader(limited)
		binary = nil
		for count := 0; ; count++ {
			header, err := archive.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("invalid kernel archive: %w", err)
			}
			if count >= 256 || limited.N <= 0 || header.Size < 0 || header.Size > OfflineArtifactMaxBytes {
				return nil, errors.New("kernel archive exceeds limits")
			}
			name := strings.TrimPrefix(header.Name, "./")
			if name != "zero" || header.Typeflag != tar.TypeReg {
				return nil, errors.New("kernel archive must contain only a regular zero executable")
			}
			if binary != nil {
				return nil, errors.New("kernel archive contains duplicate zero executables")
			}
			binary, err = io.ReadAll(io.LimitReader(archive, OfflineArtifactMaxBytes+1))
			if err != nil || int64(len(binary)) != header.Size {
				return nil, errors.New("incomplete kernel executable")
			}
		}
		// Read through the gzip trailer to verify its checksum.
		if _, err := io.Copy(io.Discard, limited); err != nil {
			return nil, fmt.Errorf("invalid kernel archive checksum: %w", err)
		}
		if limited.N <= 0 {
			return nil, errors.New("kernel archive exceeds expansion limit")
		}
	}
	if err := validateOfflineELF(binary); err != nil {
		return nil, err
	}
	return binary, nil
}

func validateOfflineELF(binary []byte) error {
	file, err := elf.NewFile(bytes.NewReader(binary))
	if err != nil {
		return errors.New("file is not a Linux x86_64 executable")
	}
	defer file.Close()
	if file.Class != elf.ELFCLASS64 || file.Data != elf.ELFDATA2LSB || file.Machine != elf.EM_X86_64 || (file.Type != elf.ET_EXEC && file.Type != elf.ET_DYN) || (file.OSABI != elf.ELFOSABI_NONE && file.OSABI != elf.ELFOSABI_LINUX) {
		return errors.New("only Linux x86_64 executables are supported")
	}
	if len(file.Progs) == 0 {
		return errors.New("kernel executable has no loadable segments")
	}
	loaded := false
	for _, prog := range file.Progs {
		if prog.Off > uint64(len(binary)) || prog.Filesz > uint64(len(binary))-prog.Off {
			return errors.New("truncated kernel executable")
		}
		if prog.Type == elf.PT_LOAD && prog.Flags&elf.PF_X != 0 {
			loaded = true
		}
	}
	if !loaded {
		return errors.New("kernel executable has no executable segment")
	}
	return nil
}
