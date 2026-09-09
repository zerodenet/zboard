package plugins

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Signature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
	Signature string `json:"signature"`
}
type Package struct {
	Manifest    Manifest
	RawManifest []byte
	Digest      string
	Publisher   string
	Files       map[string][]byte
}

func verifySignature(raw []byte, sig Signature, keys map[string]string) error {
	key, err := base64.StdEncoding.DecodeString(keys[sig.KeyID])
	if err != nil || len(key) != ed25519.PublicKeySize || sig.KeyID == "" {
		return errors.New("publisher is not trusted")
	}
	value, err := base64.StdEncoding.DecodeString(sig.Signature)
	if err != nil || sig.Algorithm != "ed25519" || !ed25519.Verify(ed25519.PublicKey(key), raw, value) {
		return errors.New("invalid publisher signature")
	}
	return nil
}
func ReadPackage(data []byte, keys map[string]string) (*Package, error) {
	if int64(len(data)) > MaxPackageBytes {
		return nil, errors.New("plugin package exceeds 32 MiB")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("invalid ZIP package")
	}
	if len(z.File) > 514 {
		return nil, errors.New("too many package files")
	}
	files := map[string][]byte{}
	total := int64(0)
	normalized := map[string]bool{}
	for _, f := range z.File {
		name := strings.ToLower(f.Name)
		if !SafePath(f.Name) || f.Mode()&os.ModeType != 0 || normalized[name] {
			return nil, errors.New("unsafe, duplicate or non-regular package path")
		}
		normalized[name] = true
		if f.UncompressedSize64 > 64<<20 {
			return nil, errors.New("package file too large")
		}
		total += int64(f.UncompressedSize64)
		if total > 64<<20 {
			return nil, errors.New("expanded package exceeds 64 MiB")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, int64(f.UncompressedSize64)+1))
		r.Close()
		if err != nil || uint64(len(b)) != f.UncompressedSize64 {
			return nil, errors.New("invalid package file content")
		}
		files[f.Name] = b
	}
	raw := files["manifest.json"]
	if len(raw) == 0 || len(raw) > 256<<10 {
		return nil, errors.New("manifest missing or too large")
	}
	var sig Signature
	if err := DecodeStrict(files["signature.json"], &sig); err != nil {
		return nil, errors.New("signed packages are required")
	}
	if err := verifySignature(raw, sig, keys); err != nil {
		return nil, err
	}
	var m Manifest
	if err := DecodeStrict(raw, &m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if len(files) != len(m.Files)+2 {
		return nil, errors.New("undeclared or missing package files")
	}
	for name, want := range m.Files {
		b, ok := files[name]
		if !ok {
			return nil, errors.New("declared file missing")
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != want {
			return nil, errors.New("package file hash mismatch")
		}
	}
	sum := sha256.Sum256(data)
	return &Package{Manifest: m, RawManifest: raw, Digest: hex.EncodeToString(sum[:]), Publisher: sig.KeyID, Files: files}, nil
}
func writePackage(root string, p *Package, data []byte) error {
	target := filepath.Join(root, "versions", p.Digest)
	if info, err := os.Lstat(target); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe version directory")
		}
		existing, readErr := os.ReadFile(filepath.Join(target, "package.zbplugin"))
		if readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		// All consumers verify the archive; repair a missing or damaged copy
		// atomically without trusting previously extracted executable files.
		file, err := os.CreateTemp(target, ".repair-")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		if _, err := file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return os.Rename(file.Name(), filepath.Join(target, "package.zbplugin"))
	}
	staging, err := os.MkdirTemp(filepath.Join(root, "versions"), ".import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for name, b := range p.Files {
		file := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		mode := os.FileMode(0600)
		if strings.HasPrefix(name, "runtimes/") {
			mode = 0700
		}
		if err := os.WriteFile(file, b, mode); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "package.zbplugin"), data, 0600); err != nil {
		return err
	}
	return os.Rename(staging, target)
}
func manifestJSON(m Manifest) string { b, _ := json.Marshal(m); return string(b) }
