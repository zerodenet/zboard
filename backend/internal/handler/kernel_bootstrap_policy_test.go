package handler

import (
	"os"
	"strings"
	"testing"
)

func TestGeneratedConnectorCredentialIsPreparedBeforeZeroStartup(t *testing.T) {
	payload, err := os.ReadFile("kernel_automation.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(payload)
	prepared := strings.Index(source, "credentialActivated := false")
	installed := strings.Index(source, "h.installNodeKernel(node, operation.ID")
	if prepared < 0 || installed < 0 || prepared > installed {
		t.Fatalf("generated connector credential must be activated before Zero installation: prepared=%d installed=%d", prepared, installed)
	}
	if strings.Contains(source, "activate generated connector credential: %w") {
		t.Fatal("generated connector credential must not wait until post-install verification")
	}
	if !strings.Contains(source, "credentialErr := restoreCredential()") {
		t.Fatal("generated connector credential must participate in activation rollback")
	}
}
