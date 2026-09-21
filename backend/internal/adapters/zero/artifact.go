package zero

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	artifactMaxBytes = 128 << 20
	binaryMaxBytes   = 128 << 20
)

type ArtifactRelease struct {
	URL       string
	SHA256    string
	Size      int64
	LocalPath string
}

type ArtifactLoader struct {
	Client *http.Client
}

func ValidateReleaseURL(value *url.URL) error {
	if value == nil || value.Scheme != "https" {
		return errors.New("Zero release URL must use HTTPS")
	}
	switch strings.ToLower(value.Hostname()) {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return nil
	default:
		return fmt.Errorf("Zero release host %q is not allowed", value.Hostname())
	}
}

func (l ArtifactLoader) LoadBinary(parent context.Context, release ArtifactRelease) ([]byte, string, error) {
	archive, err := l.loadArchive(parent, release)
	if err != nil {
		return nil, "", err
	}
	if len(archive) > artifactMaxBytes {
		return nil, "", errors.New("Zero artifact exceeds the size limit")
	}
	if release.Size <= 0 || int64(len(archive)) != release.Size {
		return nil, "", errors.New("Zero artifact size does not match release metadata")
	}
	archiveDigest := sha256.Sum256(archive)
	if hex.EncodeToString(archiveDigest[:]) != release.SHA256 {
		return nil, "", errors.New("Zero artifact SHA-256 does not match the signed release metadata")
	}
	return extractBinary(archive)
}

func (l ArtifactLoader) loadArchive(parent context.Context, release ArtifactRelease) ([]byte, error) {
	if release.LocalPath != "" {
		artifact, err := os.Open(release.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("open managed Zero artifact: %w", err)
		}
		archive, readErr := io.ReadAll(io.LimitReader(artifact, artifactMaxBytes+1))
		closeErr := artifact.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read managed Zero artifact: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close managed Zero artifact: %w", closeErr)
		}
		return archive, nil
	}
	client := l.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	parsed, err := url.Parse(release.URL)
	if err != nil {
		return nil, err
	}
	if err := ValidateReleaseURL(parsed); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, release.URL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "zboard-kernel-automation")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download Zero artifact: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download Zero artifact returned %s", response.Status)
	}
	archive, err := io.ReadAll(io.LimitReader(response.Body, artifactMaxBytes+1))
	if err != nil {
		return nil, err
	}
	return archive, nil
}

func extractBinary(archive []byte) ([]byte, string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, "", fmt.Errorf("open Zero artifact: %w", err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read Zero artifact: %w", err)
		}
		if header.Name != "zero" || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size <= 0 || header.Size > binaryMaxBytes {
			return nil, "", fmt.Errorf("Zero binary size %d bytes is outside the supported range (maximum %d bytes)", header.Size, binaryMaxBytes)
		}
		binary, err := io.ReadAll(io.LimitReader(tarReader, binaryMaxBytes+1))
		if err != nil {
			return nil, "", err
		}
		if int64(len(binary)) != header.Size {
			return nil, "", errors.New("Zero binary size does not match the archive header")
		}
		digest := sha256.Sum256(binary)
		return binary, hex.EncodeToString(digest[:]), nil
	}
	return nil, "", errors.New("Zero release archive does not contain the zero binary")
}
