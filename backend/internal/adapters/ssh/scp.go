package ssh

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

type scpSession interface {
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.Reader, error)
	Start(string) error
	Wait() error
}

// UploadSCP uses the SCP wire protocol on the existing authenticated SSH client.
// The receiver must acknowledge the header and complete payload before success.
func (s *RemoteSession) UploadSCP(destination, mode string, payload []byte) error {
	if s == nil || s.client == nil || !strings.HasPrefix(destination, "/") || strings.ContainsAny(destination, "\r\n\x00") || len(payload) == 0 || !uploadModePattern.MatchString(mode) {
		return errors.New("invalid SCP upload")
	}
	session, err := s.client.newSession()
	if err != nil {
		return err
	}
	defer session.close()
	transfer, ok := session.(scpSession)
	if !ok {
		return errors.New("SSH session does not support SCP")
	}
	input, err := transfer.StdinPipe()
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := transfer.StdoutPipe()
	if err != nil {
		return err
	}
	if err = transfer.Start("umask 077; scp -t " + shellQuote(destination)); err != nil {
		return fmt.Errorf("start SCP receiver: %w", err)
	}
	if err = sendSCP(input, output, path.Base(destination), mode, payload); err != nil {
		return err
	}
	if err = input.Close(); err != nil {
		return err
	}
	if err = transfer.Wait(); err != nil {
		return fmt.Errorf("SCP receiver failed: %w", err)
	}
	return nil
}

func sendSCP(w io.Writer, r io.Reader, name, mode string, payload []byte) error {
	reader := bufio.NewReader(r)
	if err := readSCPAck(reader); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "C%04s %d %s\n", mode, len(payload), name); err != nil {
		return err
	}
	if err := readSCPAck(reader); err != nil {
		return err
	}
	if _, err := io.Copy(w, bytes.NewReader(payload)); err != nil {
		return err
	}
	if _, err := w.Write([]byte{0}); err != nil {
		return err
	}
	return readSCPAck(reader)
}

func readSCPAck(r *bufio.Reader) error {
	code, err := r.ReadByte()
	if err != nil {
		return fmt.Errorf("SCP acknowledgement: %w", err)
	}
	if code == 0 {
		return nil
	}
	if code != 1 && code != 2 {
		return fmt.Errorf("invalid SCP acknowledgement %d", code)
	}
	var message strings.Builder
	for n := 0; n < 2048; n++ {
		b, err := r.ReadByte()
		if err != nil || b == '\n' {
			break
		}
		message.WriteByte(b)
	}
	return fmt.Errorf("SCP receiver: %s", message.String())
}
