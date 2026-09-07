package handler

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Optional external-corpus check, without network access in normal test runs.
func TestManagedRuleProviderRepositoryCompatibility(t *testing.T) {
	root := os.Getenv("ZBOARD_RULE_PROVIDER_TEST_DIR")
	if root == "" {
		t.Skip("set ZBOARD_RULE_PROVIDER_TEST_DIR to a Clash Provider directory")
	}
	compiler := os.Getenv("ZBOARD_SING_BOX_VALIDATE_BIN")
	passed, empty := 0, 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		doc, err := parseManagedRuleSource(raw, managedRuleSourceAuto)
		if err != nil {
			if strings.Contains(err.Error(), "没有有效规则") || strings.Contains(err.Error(), "payload 为空") {
				empty++
				t.Logf("EMPTY %s: %v", path, err)
				return nil
			}
			t.Errorf("parse %s: %v", path, err)
			return nil
		}
		roundTrip, err := parseManagedRuleSource(encodeManagedRuleClashYAML(doc), managedRuleSourceClashClassical)
		if err != nil || !bytes.Equal(encodeManagedCanonicalSource(doc), encodeManagedCanonicalSource(roundTrip)) {
			t.Errorf("round trip %s: %v", path, err)
		}
		output, err := encodeManagedRuleSingBox(doc)
		if err != nil {
			t.Errorf("encode %s: %v", path, err)
			return nil
		}
		if compiler != "" {
			dir := t.TempDir()
			source := filepath.Join(dir, "rules.json")
			if err := os.WriteFile(source, output, 0600); err != nil {
				return err
			}
			if output, err := exec.Command(compiler, "rule-set", "compile", source, "-o", filepath.Join(dir, "rules.srs")).CombinedOutput(); err != nil {
				t.Errorf("sing-box compile %s: %v %s", path, err, output)
			}
		}
		passed++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if passed == 0 {
		t.Fatal("no providers verified")
	}
	t.Logf("Providers imported and exported: %d; empty sources explicitly rejected: %d", passed, empty)
}
