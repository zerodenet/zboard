package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/jobs"
	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

func TestNodeCleanupRejectsInvalidIntentAndReusedHostBeforeDial(t *testing.T) {
	f := newTrafficReadFixture(t)
	if err := f.h.executeNodeCleanupRun(context.Background(), jobs.Run{Submission: jobs.Submission{Owner: "system", Handler: network.NodeCleanupHandler, Payload: `{}`}}); !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("invalid intent error=%v", err)
	}
	credential, err := f.h.credentialCipher.Encrypt("ssh-secret")
	if err != nil {
		t.Fatal(err)
	}
	privilege, err := f.h.credentialCipher.Encrypt("")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := "SHA256:" + base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	snapshot := network.NodeCleanupSnapshot{
		NodeID: 77, Host: "reused.example.test", Port: 22, User: "root", AuthMethod: "password",
		CredentialCiphertext: credential, PrivilegeMode: "none", PrivilegePasswordCiphertext: privilege, HostKeyFingerprint: fingerprint,
	}
	encoded, _ := json.Marshal(snapshot)
	encrypted, err := f.h.credentialCipher.Encrypt(string(encoded))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(network.NodeCleanupRunInput{Revision: "1", NodeID: 77, NodeName: "deleted", SnapshotCiphertext: encrypted})
	if err := f.h.db.Create(&model.Node{Name: "replacement", Config: "{}", SSHHost: snapshot.Host, SSHPort: snapshot.Port, SSHHostKeyFingerprint: fingerprint}).Error; err != nil {
		t.Fatal(err)
	}
	err = f.h.executeNodeCleanupRun(context.Background(), jobs.Run{Submission: jobs.Submission{Owner: "system", Handler: network.NodeCleanupHandler, Payload: string(payload)}})
	if err == nil || !strings.Contains(err.Error(), "used by another node") {
		t.Fatalf("reused host error=%v", err)
	}
}
