package network

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mieruEndpointConfigurationCipher struct{}

func (mieruEndpointConfigurationCipher) Encrypt(value string) (string, error) {
	return "enc:" + value, nil
}
func (mieruEndpointConfigurationCipher) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, "enc:") {
		return "", errors.New("invalid ciphertext")
	}
	return strings.TrimPrefix(value, "enc:"), nil
}

type mieruEndpointConfigurationRepositoryStub struct {
	rows    []MieruEndpointConfigurationSnapshot
	updates []MieruEndpointConfigurationUpdate
	err     error
	commits int
}

func (s *mieruEndpointConfigurationRepositoryStub) ListMieruEndpointConfigurations(context.Context) ([]MieruEndpointConfigurationSnapshot, error) {
	return s.rows, nil
}

func (s *mieruEndpointConfigurationRepositoryStub) CommitMieruEndpointConfigurations(_ context.Context, updates []MieruEndpointConfigurationUpdate) error {
	s.updates = updates
	s.commits++
	return s.err
}

func TestMieruEndpointConfigurationsNormalizesAndEncryptsBatch(t *testing.T) {
	repository := &mieruEndpointConfigurationRepositoryStub{rows: []MieruEndpointConfigurationSnapshot{{
		ID: 3, NodeID: 4, ServerCiphertext: `enc:{"type":"mieru","users":[]}`,
		ClientConfig: `{"type":"mieru","username":"old","password":"old","transport":"tcp"}`, MieruPrincipalReady: true,
	}}}
	service := MieruEndpointConfigurations{Repository: repository, Cipher: mieruEndpointConfigurationCipher{}, NewPassword: func() (string, error) { return "generated-secret", nil }}
	result, err := service.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Endpoints) != 1 || len(result.ChangedEndpointIDs) != 1 || result.ChangedEndpointIDs[0] != 3 || len(repository.updates) != 1 {
		t.Fatalf("result=%+v updates=%+v", result, repository.updates)
	}
	update := repository.updates[0]
	if !strings.Contains(update.ServerCiphertext, "generated-secret") || strings.Contains(update.ClientConfig, "old") || update.ExpectedServerCiphertext != repository.rows[0].ServerCiphertext {
		t.Fatalf("update=%+v", update)
	}
}

func TestMieruEndpointConfigurationsPreservesExistingPasswordAndSkipsStableRows(t *testing.T) {
	server, client, err := normalizeMieruEndpointConfigs(`{"type":"mieru"}`, `{"type":"mieru","transport":"tcp"}`, `{"users":[{"password":"existing-secret"}]}`, func() (string, error) {
		t.Fatal("password generator called")
		return "", nil
	})
	if err != nil || !strings.Contains(server, "existing-secret") || strings.Contains(client, "password") {
		t.Fatalf("server=%s client=%s error=%v", server, client, err)
	}
	repository := &mieruEndpointConfigurationRepositoryStub{rows: []MieruEndpointConfigurationSnapshot{{ID: 1, ServerCiphertext: "enc:" + server, ClientConfig: client}}}
	result, err := (MieruEndpointConfigurations{Repository: repository, Cipher: mieruEndpointConfigurationCipher{}}).Reconcile(context.Background())
	if err != nil || len(result.ChangedEndpointIDs) != 0 || len(repository.updates) != 0 {
		t.Fatalf("result=%+v updates=%+v error=%v", result, repository.updates, err)
	}
}

func TestMieruEndpointConfigurationsRejectsInvalidBoundaryAndPropagatesConflict(t *testing.T) {
	if _, err := (MieruEndpointConfigurations{}).Reconcile(context.Background()); !errors.Is(err, ErrMieruEndpointConfigurationUnavailable) {
		t.Fatalf("error=%v", err)
	}
	repository := &mieruEndpointConfigurationRepositoryStub{
		rows: []MieruEndpointConfigurationSnapshot{{ID: 1, ServerCiphertext: `enc:{}`, ClientConfig: `{}`}},
		err:  ErrMieruEndpointConfigurationConflict,
	}
	_, err := (MieruEndpointConfigurations{Repository: repository, Cipher: mieruEndpointConfigurationCipher{}, NewPassword: func() (string, error) { return "secret", nil }}).Reconcile(context.Background())
	if !errors.Is(err, ErrMieruEndpointConfigurationConflict) {
		t.Fatalf("error=%v", err)
	}
	if repository.commits != 3 {
		t.Fatalf("commit attempts=%d", repository.commits)
	}
}
