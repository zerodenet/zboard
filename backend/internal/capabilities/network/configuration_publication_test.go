package network

import (
	"context"
	"errors"
	"testing"
	"time"
)

type configurationPublicationRepositoryStub struct {
	start              ConfigurationPublicationStart
	beginRequest       ConfigurationPublicationRequest
	desired            string
	activated          bool
	restored           bool
	completion         ConfigurationPublicationCompletion
	failure            string
	failureOutput      string
	failurePersistence error
}

func (s *configurationPublicationRepositoryStub) BeginConfigurationPublication(_ context.Context, request ConfigurationPublicationRequest, _ time.Time) (ConfigurationPublicationStart, error) {
	s.beginRequest = request
	return s.start, nil
}
func (s *configurationPublicationRepositoryStub) SetConfigurationPublicationDesired(_ context.Context, deployment ConfigurationDeployment, desired string) (ConfigurationDeployment, error) {
	s.desired = desired
	deployment.DesiredConfigSHA256 = desired
	return deployment, nil
}
func (s *configurationPublicationRepositoryStub) ActivateConfigurationConnectorCredential(context.Context, ConfigurationDeployment, KernelEncryptedCredential) error {
	s.activated = true
	return nil
}
func (s *configurationPublicationRepositoryStub) RestoreConfigurationConnectorCredential(context.Context, ConfigurationDeployment, KernelConnectorSnapshot) error {
	s.restored = true
	return nil
}
func (s *configurationPublicationRepositoryStub) CompleteConfigurationPublication(_ context.Context, deployment ConfigurationDeployment, completion ConfigurationPublicationCompletion, _ time.Time) (ConfigurationDeployment, error) {
	s.completion = completion
	deployment.Status = "succeeded"
	return deployment, nil
}
func (s *configurationPublicationRepositoryStub) FailConfigurationPublication(_ context.Context, deployment ConfigurationDeployment, message, output string, _ time.Time) (ConfigurationDeployment, error) {
	s.failure, s.failureOutput = message, output
	deployment.Status = "failed"
	return deployment, s.failurePersistence
}

func TestConfigurationPublicationStateDelegatesFencedLifecycle(t *testing.T) {
	repository := &configurationPublicationRepositoryStub{start: ConfigurationPublicationStart{Deployment: ConfigurationDeployment{ID: 7, NodeID: 3}}}
	state := ConfigurationPublicationState{Repository: repository, Now: func() time.Time { return time.Unix(10, 0) }}
	started, err := state.Begin(context.Background(), ConfigurationPublicationRequest{NodeID: 3, TriggerEndpointID: 9, RequestedBy: 2})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := state.SetDesired(context.Background(), started.Deployment, " sha ")
	if err != nil {
		t.Fatal(err)
	}
	credential := KernelEncryptedCredential{Ciphertext: "encrypted", Prefix: "prefix"}
	if err := state.ActivateConnectorCredential(context.Background(), deployment, credential); err != nil {
		t.Fatal(err)
	}
	if err := state.RestoreConnectorCredential(context.Background(), deployment, KernelConnectorSnapshot{}); err != nil {
		t.Fatal(err)
	}
	completion := ConfigurationPublicationCompletion{ConfigSHA: " sha ", Output: " output ", LastHealthyAt: time.Unix(20, 0)}
	if _, err := state.Complete(context.Background(), deployment, completion); err != nil {
		t.Fatal(err)
	}
	if repository.beginRequest.TriggerEndpointID != 9 || repository.desired != "sha" || !repository.activated || !repository.restored || repository.completion.ConfigSHA != "sha" || repository.completion.Output != "output" {
		t.Fatalf("repository=%+v", repository)
	}
}

func TestConfigurationPublicationStatePreservesCauseAndCommitFailure(t *testing.T) {
	persistence := errors.New("database unavailable")
	repository := &configurationPublicationRepositoryStub{failurePersistence: persistence}
	state := ConfigurationPublicationState{Repository: repository}
	cause := errors.New("remote activation failed")
	_, err := state.Fail(context.Background(), ConfigurationDeployment{ID: 1, NodeID: 2}, cause, " remote output ")
	if !errors.Is(err, cause) || !errors.Is(err, persistence) || !errors.Is(err, ErrConfigurationPublicationCommit) || repository.failureOutput != "remote output" {
		t.Fatalf("failure=%q output=%q err=%v", repository.failure, repository.failureOutput, err)
	}
}

func TestConfigurationPublicationStateRejectsIncompleteBoundaryValues(t *testing.T) {
	state := ConfigurationPublicationState{}
	if _, err := state.Begin(context.Background(), ConfigurationPublicationRequest{NodeID: 1, TriggerEndpointID: 2}); !errors.Is(err, ErrConfigurationPublicationUnavailable) {
		t.Fatalf("begin error=%v", err)
	}
	state.Repository = &configurationPublicationRepositoryStub{}
	if _, err := state.SetDesired(context.Background(), ConfigurationDeployment{ID: 1}, ""); !errors.Is(err, ErrConfigurationPublicationUnavailable) {
		t.Fatalf("desired error=%v", err)
	}
}
