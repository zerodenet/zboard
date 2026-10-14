package pluginv1

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const ActionErrorDomain = "zboard.plugin.action.v1"
const MaxActionErrorMessageBytes = 1024
const MaxActionErrorBytes = 2048

var actionErrorCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ActionError is an explicitly public business rejection, never a host
// permission or transport error. Retryable permits manual retry, not replay.
type ActionError struct {
	Code      string
	Message   string
	Retryable bool
}

func NewActionError(code, publicMessage string, retryable bool) error {
	return &ActionError{Code: code, Message: publicMessage, Retryable: retryable}
}

func (e *ActionError) Error() string { return "plugin action rejected" }

func validActionError(e *ActionError) bool {
	if e == nil || !actionErrorCode.MatchString(e.Code) || len(e.Message) == 0 || len(e.Message) > MaxActionErrorMessageBytes || !utf8.ValidString(e.Message) || strings.TrimSpace(e.Message) != e.Message {
		return false
	}
	for _, r := range e.Message {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

func (e *ActionError) GRPCStatus() *status.Status {
	if !validActionError(e) {
		return status.New(codes.Internal, "invalid plugin action error")
	}
	retryable := "false"
	if e.Retryable {
		retryable = "true"
	}
	s, err := status.New(codes.FailedPrecondition, "plugin action rejected").WithDetails(&errdetails.ErrorInfo{
		Domain: ActionErrorDomain, Reason: e.Code,
		Metadata: map[string]string{"public_message": e.Message, "retryable": retryable},
	})
	if err != nil {
		return status.New(codes.Internal, "invalid plugin action error")
	}
	return s
}

// ParseActionError accepts only the versioned public contract. Raw RPC text,
// unknown details, extra metadata and malformed values remain host failures.
func ParseActionError(err error) (*ActionError, bool) {
	s, ok := status.FromError(err)
	if !ok || s.Code() != codes.FailedPrecondition || proto.Size(s.Proto()) > MaxActionErrorBytes {
		return nil, false
	}
	details := s.Details()
	if len(details) != 1 {
		return nil, false
	}
	info, ok := details[0].(*errdetails.ErrorInfo)
	if !ok || info.Domain != ActionErrorDomain || len(info.Metadata) != 2 {
		return nil, false
	}
	retryable, ok := info.Metadata["retryable"]
	if !ok || (retryable != "true" && retryable != "false") {
		return nil, false
	}
	value := &ActionError{Code: info.Reason, Message: info.Metadata["public_message"], Retryable: retryable == "true"}
	if !validActionError(value) {
		return nil, false
	}
	return value, true
}
