package ssh

import (
	"errors"
	"io"
	"strings"
	"testing"

	cryptossh "golang.org/x/crypto/ssh"
)

type fakeClient struct {
	sessions []*fakeSession
	err      error
	closed   int
}

func (c *fakeClient) newSession() (commandSession, error) {
	if c.err != nil {
		return nil, c.err
	}
	session := &fakeSession{}
	c.sessions = append(c.sessions, session)
	return session, nil
}

func (c *fakeClient) close() error {
	c.closed++
	return nil
}

type fakeSession struct {
	command    string
	stdin      string
	pty        bool
	output     []byte
	err        error
	closed     int
	requestErr error
}

func (s *fakeSession) requestPTY(_ string, _, _ int, _ cryptossh.TerminalModes) error {
	s.pty = true
	return s.requestErr
}

func (s *fakeSession) setStdin(reader io.Reader) {
	payload, _ := io.ReadAll(reader)
	s.stdin = string(payload)
}

func (s *fakeSession) combinedOutput(command string) ([]byte, error) {
	s.command = command
	return s.output, s.err
}

func (s *fakeSession) close() error {
	s.closed++
	return nil
}

func TestRemoteSessionRunPreparesPrivilegeAndInput(t *testing.T) {
	client := &fakeClient{}
	remote := newRemoteSession(client, func(command string, privileged bool) (string, string, bool, error) {
		if command != "id -u" || !privileged {
			t.Fatalf("prepare input = %q, %v", command, privileged)
		}
		return "sudo command", "secret\n", true, nil
	})

	output, err := remote.RunWithInput("id -u", true, "payload\n")
	if err != nil || output != "" {
		t.Fatalf("RunWithInput() = %q, %v", output, err)
	}
	session := client.sessions[0]
	if session.command != "sudo command" || session.stdin != "secret\npayload\n" || !session.pty || session.closed != 1 {
		t.Fatalf("session = %+v", session)
	}
}

func TestRemoteSessionRunTrimsOutputAndPreservesError(t *testing.T) {
	client := &fakeClient{}
	remote := newRemoteSession(client, func(command string, _ bool) (string, string, bool, error) {
		return command, "", false, nil
	})
	expected := errors.New("exit status 1")
	client.sessions = nil
	// Configure the session after creation through a client wrapper.
	configured := &configuredClient{session: &fakeSession{output: []byte("  failure details \n"), err: expected}}
	remote = newRemoteSession(configured, remote.prepare)
	output, err := remote.Run("false", false)
	if output != "failure details" || !errors.Is(err, expected) {
		t.Fatalf("Run() = %q, %v", output, err)
	}
}

func TestRemoteSessionUploadQuotesPathAndValidatesMode(t *testing.T) {
	session := &fakeSession{}
	remote := newRemoteSession(&configuredClient{session: session}, func(command string, _ bool) (string, string, bool, error) {
		return command, "", false, nil
	})
	if err := remote.Upload("/tmp/a'b", "0600", []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if session.stdin != "payload" || session.command != `umask 077; cat > '/tmp/a'"'"'b' && chmod 0600 '/tmp/a'"'"'b'` {
		t.Fatalf("upload session = %+v", session)
	}
	if err := remote.Upload("/tmp/file", "0600; touch /tmp/injected", []byte("payload")); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if len(clientSessions(remote)) != 1 {
		t.Fatal("invalid upload opened an SSH session")
	}
}

func TestRemoteSessionUploadIncludesBoundedRemoteError(t *testing.T) {
	remoteErr := errors.New("exit status 1")
	session := &fakeSession{output: []byte(strings.Repeat("x", 2100)), err: remoteErr}
	remote := newRemoteSession(&configuredClient{session: session}, nil)
	err := remote.Upload("/tmp/file", "0600", []byte("payload"))
	if !errors.Is(err, remoteErr) || len([]rune(err.Error())) > 2050 || !strings.HasSuffix(err.Error(), "…") {
		t.Fatalf("Upload() error = %v", err)
	}
}

type configuredClient struct {
	session *fakeSession
	opened  int
	closed  int
}

func (c *configuredClient) newSession() (commandSession, error) {
	c.opened++
	return c.session, nil
}

func (c *configuredClient) close() error {
	c.closed++
	return nil
}

func clientSessions(remote *RemoteSession) []int {
	client, ok := remote.client.(*configuredClient)
	if !ok {
		return nil
	}
	return make([]int, client.opened)
}
