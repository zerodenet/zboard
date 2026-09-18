package smtpadapter

import (
	"errors"
	"fmt"
	"io"
	"net/textproto"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
)

type submissionClient interface {
	Mail(string) error
	Rcpt(string) error
	Data() (io.WriteCloser, error)
	Quit() error
}

func submit(client submissionClient, from, recipient, message string) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message: %w", err)
	}
	written, err := io.WriteString(w, message)
	if err == nil && written != len(message) {
		err = io.ErrShortWrite
	}
	if err != nil {
		// Do not close the DATA writer here: that would send the final terminator
		// and could submit a truncated body. The owner closes the connection.
		return fmt.Errorf("%w: write SMTP message: %v", messaging.ErrAcceptanceUnknown, err)
	}
	if err := w.Close(); err != nil {
		var response *textproto.Error
		if errors.As(err, &response) && response.Code >= 400 && response.Code < 600 {
			return fmt.Errorf("SMTP rejected message: %w", err)
		}
		return fmt.Errorf("%w: finish SMTP message: %v", messaging.ErrAcceptanceUnknown, err)
	}
	// DATA's positive final response is authoritative acceptance. QUIT only
	// closes the session; a broken QUIT must not turn acceptance into a retry.
	_ = client.Quit()
	return nil
}
