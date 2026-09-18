package smtpadapter

import (
	"errors"
	"io"
	"net/textproto"
	"testing"

	"github.com/zerodenet/zboard/backend/internal/capabilities/messaging"
)

type fakeSubmission struct {
	writer    *fakeData
	quitError error
	quitCalls int
}

func (s *fakeSubmission) Mail(string) error             { return nil }
func (s *fakeSubmission) Rcpt(string) error             { return nil }
func (s *fakeSubmission) Data() (io.WriteCloser, error) { return s.writer, nil }
func (s *fakeSubmission) Quit() error                   { s.quitCalls++; return s.quitError }

type fakeData struct {
	writeError, closeError error
	closes                 int
}

func (s *fakeData) Write(p []byte) (int, error) {
	if s.writeError != nil {
		return 0, s.writeError
	}
	return len(p), nil
}
func (s *fakeData) Close() error { s.closes++; return s.closeError }
func TestSMTPAcceptanceBoundary(t *testing.T) {
	for _, test := range []struct {
		name               string
		write, close, quit error
		want               messaging.Acceptance
		closes, quits      int
	}{
		{"accepted despite QUIT failure", nil, nil, io.EOF, messaging.Accepted, 1, 1},
		{"lost final response", nil, io.EOF, nil, messaging.AcceptanceUnknown, 1, 0},
		{"negative final response", nil, &textproto.Error{Code: 550, Msg: "rejected"}, nil, messaging.NotAccepted, 1, 0},
		{"partial body must not terminate DATA", io.ErrUnexpectedEOF, nil, nil, messaging.AcceptanceUnknown, 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeSubmission{writer: &fakeData{writeError: test.write, closeError: test.close}, quitError: test.quit}
			err := submit(client, "from@example.test", "to@example.test", "message")
			if messaging.AcceptanceFor(err) != test.want || client.writer.closes != test.closes || client.quitCalls != test.quits {
				t.Fatal(err, client.writer.closes, client.quitCalls)
			}
			if test.want == messaging.AcceptanceUnknown && !errors.Is(err, messaging.ErrAcceptanceUnknown) {
				t.Fatal("uncertainty contract lost")
			}
		})
	}
}
