package zero

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type kernelRemoteDialerStub struct {
	session *kernelRemoteSessionStub
	dials   int
	err     error
}

func (d *kernelRemoteDialerStub) Dial(ctx context.Context) (KernelRemoteSession, error) {
	d.dials++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.err != nil {
		return nil, d.err
	}
	return d.session, nil
}

type kernelRemoteSessionStub struct {
	runs       []kernelRemoteRun
	uploads    []kernelRemoteUpload
	closed     int
	runErrorAt int
	uploadErr  int
}

type kernelRemoteRun struct {
	command    string
	privileged bool
}

type kernelRemoteUpload struct {
	path string
	mode string
	data []byte
}

func (s *kernelRemoteSessionStub) Run(command string, privileged bool) (string, error) {
	s.runs = append(s.runs, kernelRemoteRun{command: command, privileged: privileged})
	if s.runErrorAt == len(s.runs) {
		return "remote failure", errors.New("run failed")
	}
	return "ok", nil
}

func (s *kernelRemoteSessionStub) Upload(path, mode string, payload []byte) error {
	s.uploads = append(s.uploads, kernelRemoteUpload{path: path, mode: mode, data: append([]byte(nil), payload...)})
	if s.uploadErr == len(s.uploads) {
		return errors.New("upload failed")
	}
	return nil
}

func (s *kernelRemoteSessionStub) Close() error {
	s.closed++
	return nil
}

func TestKernelInstallerStagesAndActivatesThroughRemotePort(t *testing.T) {
	session := &kernelRemoteSessionStub{}
	dialer := &kernelRemoteDialerStub{session: session}
	installer := KernelInstaller{Dialer: dialer, NewStageID: func() string { return "stage-id" }}
	err := installer.Install(context.Background(), KernelInstallRequest{
		OperationID: 41, Binary: []byte("binary"), BinarySHA256: strings.Repeat("a", 64),
		RuntimeConfig: []byte(`{"inbounds":[]}`), ConnectorKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if dialer.dials != 1 || len(session.runs) != 2 || len(session.uploads) != 5 || session.closed != 1 {
		t.Fatalf("dials=%d runs=%d uploads=%d closed=%d", dialer.dials, len(session.runs), len(session.uploads), session.closed)
	}
	if session.runs[0].privileged || !strings.Contains(session.runs[0].command, "/tmp/zboard-zero-stage-id") {
		t.Fatalf("staging command=%+v", session.runs[0])
	}
	if !session.runs[1].privileged || !strings.Contains(session.runs[1].command, "ZBOARD_KERNEL_ACTIVATED") {
		t.Fatalf("activation command=%+v", session.runs[1])
	}
	want := []struct{ suffix, mode string }{{"/zero", "0700"}, {"/runtime.json", "0600"}, {"/zero.env", "0600"}, {"/zero.service", "0644"}, {"/cleanup-zero-node.sh", "0700"}}
	for index, expected := range want {
		if !strings.HasSuffix(session.uploads[index].path, expected.suffix) || session.uploads[index].mode != expected.mode {
			t.Fatalf("upload[%d]=%+v", index, session.uploads[index])
		}
	}
}

func TestKernelInstallerStopsBeforeActivationWhenStagingFails(t *testing.T) {
	session := &kernelRemoteSessionStub{uploadErr: 2}
	installer := KernelInstaller{Dialer: &kernelRemoteDialerStub{session: session}, NewStageID: func() string { return "stage-id" }}
	err := installer.Install(context.Background(), KernelInstallRequest{
		OperationID: 41, Binary: []byte("binary"), BinarySHA256: strings.Repeat("a", 64), RuntimeConfig: []byte("{}"), ConnectorKey: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "runtime.json") || len(session.runs) != 1 {
		t.Fatalf("runs=%d err=%v", len(session.runs), err)
	}
}

func TestKernelInstallerPropagatesCancellationBeforeDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dialer := &kernelRemoteDialerStub{session: &kernelRemoteSessionStub{}}
	err := (KernelInstaller{Dialer: dialer}).Install(ctx, KernelInstallRequest{
		OperationID: 41, Binary: []byte("binary"), BinarySHA256: strings.Repeat("a", 64), RuntimeConfig: []byte("{}"), ConnectorKey: "secret",
	})
	if !errors.Is(err, context.Canceled) || dialer.dials != 1 {
		t.Fatalf("dials=%d err=%v", dialer.dials, err)
	}
}

func TestKernelInstallerRollbackUsesPrivilegedRemoteSession(t *testing.T) {
	session := &kernelRemoteSessionStub{}
	installer := KernelInstaller{Dialer: &kernelRemoteDialerStub{session: session}}
	if err := installer.Rollback(context.Background(), 41); err != nil {
		t.Fatal(err)
	}
	if len(session.runs) != 1 || !session.runs[0].privileged || !strings.Contains(session.runs[0].command, "ZBOARD_KERNEL_ROLLED_BACK") || session.closed != 1 {
		t.Fatalf("runs=%+v closed=%d", session.runs, session.closed)
	}
}

func TestKernelInstallScriptsPersistAndConsumeRollbackMetadata(t *testing.T) {
	install := BuildKernelInstallScript("/tmp/stage", strings.Repeat("a", 64), 42)
	rollback := BuildKernelRollbackScript(42)
	for _, fragment := range []string{"$backup/old_active", "$backup/old_enabled", "systemctl disable zero"} {
		if !strings.Contains(install, fragment) || !strings.Contains(rollback, fragment) {
			t.Fatalf("install/rollback scripts are missing %q", fragment)
		}
	}
	if strings.Contains(rollback, `cp -a "$backup/zero" /usr/local/bin/zero`) {
		t.Fatal("rollback overwrites the running executable directly")
	}
}
