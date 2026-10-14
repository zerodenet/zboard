package pluginv1

import (
	"strings"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestPublicActionErrorUsesVersionedDetailsWithoutRawRPCText(t *testing.T) {
	for _, retryable := range []bool{false, true} {
		err := NewActionError("resource_busy", "Resource is busy. <b>Try later</b>", retryable)
		wire := status.ErrorProto(status.Convert(err).Proto())
		value, ok := ParseActionError(wire)
		if !ok || value.Code != "resource_busy" || value.Message != "Resource is busy. <b>Try later</b>" || value.Retryable != retryable {
			t.Fatalf("roundtrip: %+v %v", value, ok)
		}
		if strings.Contains(status.Convert(err).Message(), "Resource is busy") {
			t.Fatal("public message was mixed into raw RPC diagnostic")
		}
	}
}

func TestPublicActionErrorRejectsInvalidUnknownAndOversizedDetails(t *testing.T) {
	for _, test := range []struct{ code, message string }{
		{"UPPER", "public"}, {"a.b", "public"}, {strings.Repeat("a", 65), "public"},
		{"valid", ""}, {"valid", " trailing "}, {"valid", "internal\x00value"},
		{"valid", string([]byte{0xff})}, {"valid", strings.Repeat("中", 342)},
	} {
		if _, ok := ParseActionError(NewActionError(test.code, test.message, true)); ok {
			t.Fatalf("invalid contract accepted: %q", test.code)
		}
	}
	for _, test := range []struct {
		domain, retry string
		extra         bool
	}{
		{"other.domain", "true", false}, {ActionErrorDomain, "yes", false}, {ActionErrorDomain, "true", true},
	} {
		metadata := map[string]string{"public_message": "public", "retryable": test.retry}
		if test.extra {
			metadata["internal_stack"] = "do not expose"
		}
		s, _ := status.New(codes.FailedPrecondition, "private secret").WithDetails(&errdetails.ErrorInfo{Domain: test.domain, Reason: "busy", Metadata: metadata})
		if _, ok := ParseActionError(s.Err()); ok {
			t.Fatal("unknown or malformed metadata accepted")
		}
	}
	if _, ok := ParseActionError(status.Error(codes.FailedPrecondition, "private secret")); ok {
		t.Fatal("legacy RPC text was interpreted as public error")
	}
	s, _ := status.New(codes.PermissionDenied, "host-like error").WithDetails(&errdetails.ErrorInfo{Domain: ActionErrorDomain, Reason: "busy", Metadata: map[string]string{"public_message": "public", "retryable": "true"}})
	if _, ok := ParseActionError(s.Err()); ok {
		t.Fatal("non-business RPC status accepted")
	}
	info := &errdetails.ErrorInfo{Domain: ActionErrorDomain, Reason: "busy", Metadata: map[string]string{"public_message": "public", "retryable": "true"}}
	duplicate, _ := status.New(codes.FailedPrecondition, "generic").WithDetails(info, info)
	if _, ok := ParseActionError(duplicate.Err()); ok {
		t.Fatal("ambiguous duplicate public details accepted")
	}
	oversized, _ := status.New(codes.FailedPrecondition, strings.Repeat("internal", 400)).WithDetails(info)
	if _, ok := ParseActionError(oversized.Err()); ok {
		t.Fatal("oversized RPC status accepted")
	}
}
