// pluginpackager creates signed offline packages. Private keys never enter the package.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/plugins"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	catalog := flag.String("catalog", "", "sign a market payload JSON instead of a plugin directory")
	source := flag.String("source", "", "plugin source directory with manifest.json")
	keyFile := flag.String("key", "", "base64 Ed25519 private key file")
	keyID := flag.String("key-id", "", "trusted publisher ID")
	out := flag.String("out", "", "output .zbplugin")
	keygen := flag.String("keygen", "", "create a private key here and public key at <path>.pub; refuses overwrite")
	flag.Parse()
	if *keygen != "" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		for p, b := range map[string][]byte{*keygen: priv, *keygen + ".pub": pub} {
			f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				return err
			}
			_, err = f.WriteString(base64.StdEncoding.EncodeToString(b) + "\n")
			closeErr := f.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	}
	if *source != "" && *catalog == "" && *keyFile == "" {
		directory, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		path, id, err := localSigningKey(filepath.Join(directory, "zboard", "plugin-signing"))
		if err != nil {
			return err
		}
		*keyFile = path
		if *keyID == "" {
			*keyID = id
		}
	}
	if (*source == "" && *catalog == "") || *keyFile == "" || *keyID == "" || *out == "" {
		return fmt.Errorf("source, key, key-id and out are required")
	}
	keyText, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(keyText)))
	if err != nil || len(key) != ed25519.PrivateKeySize || !bytes.Equal(ed25519.NewKeyFromSeed(key[:32]), key) {
		return fmt.Errorf("invalid Ed25519 private key")
	}
	if *catalog != "" {
		raw, err := os.ReadFile(*catalog)
		if err != nil {
			return err
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return err
		}
		raw = compact.Bytes()
		sig := plugins.Signature{Algorithm: "ed25519", KeyID: *keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw))}
		signature, err := json.Marshal(sig)
		if err != nil {
			return err
		}
		// Preserve the exact signed payload bytes, including escaped characters.
		output := append([]byte(`{"payload":`), raw...)
		output = append(output, []byte(`,"signature":`)...)
		output = append(output, signature...)
		output = append(output, '}')
		return os.WriteFile(*out, output, 0600)
	}
	raw, err := os.ReadFile(filepath.Join(*source, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest plugins.Manifest
	if err := plugins.DecodeStrict(raw, &manifest); err != nil {
		return err
	}
	files := map[string][]byte{}
	manifest.Files = map[string]string{}
	err = filepath.WalkDir(*source, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(*source, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !plugins.SafePath(rel) || (!strings.HasPrefix(rel, "ui/") && !strings.HasPrefix(rel, "runtimes/")) {
			return fmt.Errorf("unexpected package source: %s", rel)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = b
		sum := sha256.Sum256(b)
		manifest.Files[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	raw, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	files["manifest.json"] = raw
	sig := plugins.Signature{PublicKey: base64.StdEncoding.EncodeToString(ed25519.PrivateKey(key).Public().(ed25519.PublicKey)), Algorithm: "ed25519", KeyID: *keyID, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw))}
	files["signature.json"], err = json.Marshal(sig)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			return err
		}
		if _, err = f.Write(files[name]); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	pub := ed25519.PrivateKey(key).Public().(ed25519.PublicKey)
	if _, err := plugins.ReadPackage(buf.Bytes(), map[string]string{*keyID: base64.StdEncoding.EncodeToString(pub)}); err != nil {
		return err
	}
	return os.WriteFile(*out, buf.Bytes(), 0600)
}
