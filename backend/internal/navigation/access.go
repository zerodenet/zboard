package navigation

import (
	"net/url"
	"strings"
)

// PageAvailable uses the most specific registered page, including its detail
// routes. Custom shortcuts cannot reopen a hidden core/plugin page. Pages that
// have no menu registration keep their existing access checks.
func PageAvailable(all, visible []Node, path string) bool {
	u, err := url.Parse(path)
	if err != nil {
		return false
	}
	path = strings.TrimRight(u.Path, "/")
	if path == "" {
		path = "/"
	}
	longest := -1
	ids := map[string]bool{}
	for _, n := range all {
		if n.Owner == "custom" || n.Path == "" {
			continue
		}
		target, err := url.Parse(n.Path)
		if err != nil || target.RawQuery != "" || target.Fragment != "" {
			continue
		}
		base := strings.TrimRight(target.Path, "/")
		if base == "" {
			base = "/"
		}
		if path != base && (base == "/" || !strings.HasPrefix(path, base+"/")) {
			continue
		}
		if len(base) > longest {
			longest, ids = len(base), map[string]bool{}
		}
		if len(base) == longest {
			ids[n.ID] = true
		}
	}
	if longest < 0 {
		return true
	}
	for _, n := range visible {
		if ids[n.ID] {
			return true
		}
	}
	return false
}
