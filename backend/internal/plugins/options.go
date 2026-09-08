package plugins

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
)

type Options struct {
	Directory         string            `json:"directory,optional"`
	CatalogURL        string            `json:"catalog_url,optional"`
	TrustedPublishers map[string]string `json:"trusted_publishers,optional"`
}

func (o *Options) Validate() error {
	if o.Directory == "" {
		o.Directory = "data/plugins"
	}
	abs, err := filepath.Abs(o.Directory)
	if err != nil {
		return err
	}
	o.Directory = abs
	for id, key := range o.TrustedPublishers {
		b, err := base64.StdEncoding.DecodeString(key)
		if id == "" || len(id) > 160 || strings.ContainsAny(id, "\r\n") || err != nil || len(b) != 32 {
			return errors.New("plugins.trusted_publishers requires Ed25519 public keys")
		}
	}
	if o.CatalogURL != "" {
		if _, err := safeRemoteURL(o.CatalogURL); err != nil {
			return errors.New("plugins.catalog_url must be a public HTTPS URL")
		}
	}
	return nil
}
