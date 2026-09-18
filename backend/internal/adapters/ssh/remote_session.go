package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	cryptossh "golang.org/x/crypto/ssh"
)

var uploadModePattern = regexp.MustCompile(`^[0-7]{3,4}$`)

type CommandPreparer func(command string, privileged bool) (prepared string, stdin string, requestPTY bool, err error)

type RemoteSession struct {
	client  sessionClient
	prepare CommandPreparer
}

func NewRemoteSession(client *cryptossh.Client, prepare CommandPreparer) *RemoteSession {
	if client == nil {
		return &RemoteSession{prepare: prepare}
	}
	return newRemoteSession(sshClient{Client: client}, prepare)
}

func newRemoteSession(client sessionClient, prepare CommandPreparer) *RemoteSession {
	return &RemoteSession{client: client, prepare: prepare}
}

func (s *RemoteSession) Run(command string, privileged bool) (string, error) {
	return s.RunWithInput(command, privileged, "")
}

func (s *RemoteSession) RunWithInput(command string, privileged bool, input string) (string, error) {
	if s == nil || s.client == nil || s.prepare == nil {
		return "", errors.New("SSH remote session is unavailable")
	}
	prepared, stdin, requestPTY, err := s.prepare(command, privileged)
	if err != nil {
		return "", err
	}
	session, err := s.client.newSession()
	if err != nil {
		return "", err
	}
	defer session.close()
	if requestPTY {
		modes := cryptossh.TerminalModes{cryptossh.ECHO: 0, cryptossh.TTY_OP_ISPEED: 14400, cryptossh.TTY_OP_OSPEED: 14400}
		if err := session.requestPTY("xterm", 24, 80, modes); err != nil {
			return "", fmt.Errorf("request privilege terminal: %w", err)
		}
	}
	if stdin != "" {
		input = stdin + input
	}
	if input != "" {
		session.setStdin(strings.NewReader(input))
	}
	output, err := session.combinedOutput(prepared)
	return strings.TrimSpace(string(output)), err
}

func (s *RemoteSession) Upload(path, mode string, payload []byte) error {
	if s == nil || s.client == nil || strings.TrimSpace(path) == "" || len(payload) == 0 {
		return errors.New("empty SSH upload")
	}
	if !uploadModePattern.MatchString(mode) {
		return errors.New("invalid SSH upload mode")
	}
	session, err := s.client.newSession()
	if err != nil {
		return err
	}
	defer session.close()
	session.setStdin(bytes.NewReader(payload))
	command := "umask 077; cat > " + shellQuote(path) + " && chmod " + mode + " " + shellQuote(path)
	output, err := session.combinedOutput(command)
	if err != nil {
		return fmt.Errorf("%w: %s", err, truncateOutput(string(output)))
	}
	return nil
}

func (s *RemoteSession) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.close()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func truncateOutput(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 2000 {
		return string(runes[:2000]) + "…"
	}
	return value
}

type sessionClient interface {
	newSession() (commandSession, error)
	close() error
}

type commandSession interface {
	requestPTY(term string, height, width int, modes cryptossh.TerminalModes) error
	setStdin(io.Reader)
	combinedOutput(command string) ([]byte, error)
	close() error
}

type sshClient struct {
	*cryptossh.Client
}

func (c sshClient) newSession() (commandSession, error) {
	session, err := c.NewSession()
	if err != nil {
		return nil, err
	}
	return sshSession{Session: session}, nil
}

func (c sshClient) close() error {
	return c.Close()
}

type sshSession struct {
	*cryptossh.Session
}

func (s sshSession) requestPTY(term string, height, width int, modes cryptossh.TerminalModes) error {
	return s.RequestPty(term, height, width, modes)
}

func (s sshSession) setStdin(reader io.Reader) {
	s.Stdin = reader
}

func (s sshSession) combinedOutput(command string) ([]byte, error) {
	return s.CombinedOutput(command)
}

func (s sshSession) close() error {
	return s.Close()
}
