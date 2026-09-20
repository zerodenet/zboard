// Package plugins owns extension lifecycle, never core business state transitions.
package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const MaxPackageBytes int64 = 32 << 20
const MaxConfigBytes = 64 << 10

var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)+$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var pagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Requirements struct {
	ZBoard      string   `json:"zboard"`
	Recommended string   `json:"recommended_zboard_version"`
	Tested      []string `json:"tested_zboard_versions"`
	Protocol    int      `json:"plugin_protocol"`
	Bridge      int      `json:"ui_bridge"`
}
type Page struct {
	ID      string `json:"id"`
	Surface string `json:"surface"`
	Title   string `json:"title"`
	Purpose string `json:"purpose,omitempty"`
}
type Slot struct {
	ID         string `json:"id"`
	Surface    string `json:"surface"`
	Slot       string `json:"slot"`
	Title      string `json:"title"`
	Entrypoint string `json:"entrypoint"`
}

var slotSurfaces = map[string]string{
	"auth.login.methods":          "public",
	"account.security.identities": "account",
	"admin.user.identities":       "admin",
}

type Components struct {
	UI     map[string]string `json:"ui,omitempty"`
	Server *struct {
		Executables map[string]string `json:"executables"`
	} `json:"server,omitempty"`
}
type Manifest struct {
	Data          *DataManifest `json:"data,omitempty"`
	SchemaVersion int           `json:"schema_version"`
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	Version       string        `json:"version"`
	Requires      Requirements  `json:"requires"`
	Capabilities  []string      `json:"capabilities"`
	Surfaces      []string      `json:"surfaces"`
	Components    Components    `json:"components"`
	Contributions struct {
		Pages []Page `json:"pages"`
		Slots []Slot `json:"slots,omitempty"`
	} `json:"contributions"`
	Files map[string]string `json:"files"`
}
type Compatibility struct {
	Compatible bool   `json:"compatible"`
	Tested     bool   `json:"tested"`
	Reason     string `json:"reason"`
	Warning    string `json:"warning,omitempty"`
}

func DecodeStrict(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errors.New("invalid JSON or unsupported fields")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing JSON is not allowed")
	}
	return nil
}
func SafePath(p string) bool {
	return p != "" && len(p) < 512 && strings.Count(p, "/") < 8 && p == path.Clean(p) && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, ".") && !strings.ContainsAny(p, "\\:\x00") && !strings.Contains(p, "../")
}
func (m Manifest) Validate() error {
	if m.SchemaVersion != 1 || !idPattern.MatchString(m.ID) || len(m.ID) > 160 || strings.TrimSpace(m.Name) == "" || len(m.Name) > 160 || len(m.Description) > 2000 {
		return errors.New("invalid plugin identity or schema")
	}
	if _, err := semver.StrictNewVersion(m.Version); err != nil || len(m.Version) > 64 {
		return errors.New("invalid plugin version")
	}
	if m.Requires.ZBoard != "" {
		if _, err := semver.NewConstraint(m.Requires.ZBoard); err != nil {
			return errors.New("invalid advisory host version range")
		}
	}
	if len(m.Capabilities) == 0 || len(m.Capabilities) > 4 {
		return errors.New("declare supported capabilities")
	}
	seen := map[string]bool{}
	for _, c := range m.Capabilities {
		if seen[c] || (c != "zboard.ui.page.v1" && c != "zboard.config.v1" && c != IdentityCapability && c != StorageCapability) {
			return fmt.Errorf("unsupported or duplicate capability: %s", c)
		}
		seen[c] = true
	}
	if len(m.Surfaces) > 3 || len(m.Contributions.Pages) > 24 || len(m.Contributions.Slots) > 24 || len(m.Files) == 0 || len(m.Files) > 512 {
		return errors.New("plugin contribution limits exceeded")
	}
	surfaces := map[string]bool{}
	for _, s := range m.Surfaces {
		if surfaces[s] || !slices.Contains([]string{"public", "account", "admin"}, s) {
			return errors.New("invalid surface")
		}
		surfaces[s] = true
	}
	for s, p := range m.Components.UI {
		if !surfaces[s] || !strings.HasPrefix(p, "ui/") || !SafePath(p) || !digestPattern.MatchString(m.Files[p]) {
			return errors.New("invalid UI entrypoint")
		}
	}
	if len(m.Components.UI) > 0 && !seen["zboard.ui.page.v1"] {
		return errors.New("UI capability required")
	}
	pages := map[string]bool{}
	for _, p := range m.Contributions.Pages {
		key := p.Surface + ":" + p.ID
		if pages[key] || !pagePattern.MatchString(p.ID) || !surfaces[p.Surface] || m.Components.UI[p.Surface] == "" || p.Title == "" || len(p.Title) > 160 {
			return errors.New("invalid page contribution")
		}
		if p.Purpose != "" && p.Purpose != "business" && p.Purpose != "configuration" {
			return errors.New("invalid page purpose")
		}
		if p.Purpose == "configuration" && (p.Surface != "admin" || !seen["zboard.config.v1"]) {
			return errors.New("configuration pages require admin config capability")
		}
		pages[key] = true
	}
	slots := map[string]bool{}
	for _, contribution := range m.Contributions.Slots {
		key := contribution.Surface + ":" + contribution.Slot + ":" + contribution.ID
		requiredSurface, known := slotSurfaces[contribution.Slot]
		if slots[key] || !pagePattern.MatchString(contribution.ID) || !known || contribution.Surface != requiredSurface || !surfaces[contribution.Surface] || contribution.Title == "" || len(contribution.Title) > 160 || !strings.HasPrefix(contribution.Entrypoint, "ui/") || !SafePath(contribution.Entrypoint) || !digestPattern.MatchString(m.Files[contribution.Entrypoint]) {
			return errors.New("invalid slot contribution")
		}
		if !seen[PageCapability] || !seen[IdentityCapability] {
			return errors.New("identity slots require UI and identity provider capabilities")
		}
		slots[key] = true
	}
	if seen[IdentityCapability] && (m.Components.Server == nil || !seen["zboard.config.v1"]) {
		return errors.New("identity provider requires a configurable server")
	}
	if m.Components.Server != nil {
		if !seen["zboard.config.v1"] || len(m.Components.Server.Executables) == 0 || len(m.Components.Server.Executables) > 12 {
			return errors.New("server requires config capability and runtime")
		}
		for platform, p := range m.Components.Server.Executables {
			if !pagePattern.MatchString(platform) || !strings.HasPrefix(p, "runtimes/") || !SafePath(p) || !digestPattern.MatchString(m.Files[p]) {
				return errors.New("invalid runtime declaration")
			}
		}
	}
	if len(m.Components.UI) == 0 && m.Components.Server == nil {
		return errors.New("plugin has no component")
	}
	for p, h := range m.Files {
		if !SafePath(p) || !digestPattern.MatchString(h) || p == "manifest.json" || p == "signature.json" || p == "package.zbplugin" {
			return errors.New("invalid file declaration")
		}
	}
	return m.validateData()
}
func (m Manifest) Compatibility(host string) Compatibility {
	if m.Requires.Protocol != 1 || m.Requires.Bridge != 1 {
		return Compatibility{Reason: "unsupported protocol version"}
	}
	if m.Components.Server != nil && m.Components.Server.Executables[runtime.GOOS+"-"+runtime.GOARCH] == "" {
		return Compatibility{Reason: "no runtime for this platform"}
	}
	// Product release numbers describe publisher test coverage, not the host API.
	// Admission is enforced by protocol, capabilities, platform and lifecycle.
	result := Compatibility{Compatible: true}
	v, err := semver.NewVersion(strings.TrimPrefix(host, "v"))
	if err != nil {
		result.Warning = "宿主发行版本无法识别，不影响插件接口准入。"
		return result
	}
	for _, tested := range m.Requires.Tested {
		if t, err := semver.NewVersion(tested); err == nil && t.String() == v.String() {
			result.Tested = true
			break
		}
	}
	if m.Requires.ZBoard != "" {
		c, err := semver.NewConstraint(m.Requires.ZBoard)
		if err != nil || !c.Check(v) {
			result.Warning = "当前发行版本不在发布者声明范围内；该声明仅供参考，不阻止安装或启用。"
		}
	}
	return result
}
