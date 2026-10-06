package ssh

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestSCPRequiresReceiverAcknowledgements(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		ack       []byte
		wantError bool
		want      string
	}{
		{"success", []byte{0, 0, 0}, false, "C0700 3 zero\nabc\x00"},
		{"missing receiver", nil, true, ""},
		{"denied", []byte("\x01permission denied\n"), true, ""},
		{"header rejected", []byte("\x00\x02invalid path\n"), true, "C0700 3 zero\n"},
		{"payload not acknowledged", []byte{0, 0}, true, "C0700 3 zero\nabc\x00"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var output bytes.Buffer
			err := sendSCP(&output, bytes.NewReader(scenario.ack), "zero", "0700", []byte("abc"))
			if (err != nil) != scenario.wantError || output.String() != scenario.want {
				t.Fatalf("output=%q err=%v", output.String(), err)
			}
		})
	}
}
func TestSCPBoundsRemoteErrorsAndRejectsUnsafePaths(t *testing.T) {
	err := sendSCP(io.Discard, strings.NewReader("\x02"+strings.Repeat("x", 10000)), "zero", "0700", []byte("x"))
	if err == nil || len(err.Error()) > 2100 {
		t.Fatalf("unbounded SCP error: %v", err)
	}
	session := newRemoteSession(&fakeClient{}, nil)
	for _, name := range []string{"relative", "/tmp/x\nC0700", "/tmp/x\x00"} {
		if err := session.UploadSCP(name, "0700", []byte("x")); err == nil {
			t.Fatal("unsafe path accepted")
		}
	}
}
