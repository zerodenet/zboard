package ssh

import (
	"errors"
	"io"

	cryptossh "golang.org/x/crypto/ssh"
)

type Terminal struct {
	session *cryptossh.Session
	stdin   io.WriteCloser
}

func OpenTerminal(client *cryptossh.Client, output io.Writer, terminalType string, rows, columns int) (*Terminal, error) {
	if client == nil || output == nil || terminalType == "" || rows <= 0 || columns <= 0 {
		return nil, errors.New("invalid SSH terminal request")
	}
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		return nil, err
	}
	session.Stdout = output
	session.Stderr = output
	modes := cryptossh.TerminalModes{
		cryptossh.ECHO:          1,
		cryptossh.TTY_OP_ISPEED: 14400,
		cryptossh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(terminalType, rows, columns, modes); err != nil {
		_ = session.Close()
		return nil, err
	}
	if err := session.Shell(); err != nil {
		_ = session.Close()
		return nil, err
	}
	return &Terminal{session: session, stdin: stdin}, nil
}

func (t *Terminal) Input() io.Writer {
	if t == nil {
		return nil
	}
	return t.stdin
}

func (t *Terminal) Resize(rows, columns int) error {
	if t == nil || t.session == nil || rows <= 0 || columns <= 0 {
		return errors.New("SSH terminal is unavailable")
	}
	return t.session.WindowChange(rows, columns)
}

func (t *Terminal) Wait() error {
	if t == nil || t.session == nil {
		return errors.New("SSH terminal is unavailable")
	}
	return t.session.Wait()
}

func (t *Terminal) Close() error {
	if t == nil || t.session == nil {
		return nil
	}
	return t.session.Close()
}
