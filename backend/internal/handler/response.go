package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type listRequest struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

type APIResponse struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	Error     *APIError   `json:"error,omitempty"`
	Timestamp string      `json:"timestamp"`
}

type APIError struct {
	Version int               `json:"version"`
	Code    string            `json:"code"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type requestValidationError struct {
	message string
	fields  map[string]string
}

func (e *requestValidationError) Error() string { return e.message }

func validationError(message string, fields map[string]string) error {
	return &requestValidationError{message: message, fields: fields}
}

func apiErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusPreconditionRequired:
		return "precondition_required"
	case http.StatusTooManyRequests:
		return "rate_limited"
	case http.StatusServiceUnavailable:
		return "service_unavailable"
	case http.StatusInternalServerError:
		return "internal_error"
	default:
		return "request_failed"
	}
}

func writeJSONResponse(w http.ResponseWriter, code int, message string, data interface{}, detail *APIError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if code >= http.StatusBadRequest && detail == nil {
		detail = &APIError{Version: 1, Code: apiErrorCode(code)}
	}
	_ = json.NewEncoder(w).Encode(APIResponse{
		Code:      code,
		Message:   message,
		Data:      data,
		Error:     detail,
		Timestamp: time.Now().Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, code int, message string, data interface{}) {
	writeJSONResponse(w, code, message, data, nil)
}

func BadRequestFields(w http.ResponseWriter, message string, fields map[string]string) {
	writeJSONResponse(w, http.StatusBadRequest, message, nil, &APIError{Version: 1, Code: "validation_failed", Fields: fields})
}

func BadRequestError(w http.ResponseWriter, err error) {
	var validation *requestValidationError
	if errors.As(err, &validation) {
		BadRequestFields(w, validation.message, validation.fields)
		return
	}
	BadRequest(w, err.Error())
}

func OK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, "ok", data)
}

func NotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, "not found", nil)
}

func NotImplemented(w http.ResponseWriter, feature string) {
	writeJSON(w, http.StatusNotImplemented, feature+" not implemented yet", nil)
}

func BadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, message, nil)
}

func Unauthorized(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusUnauthorized, message, nil)
}

func Forbidden(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusForbidden, message, nil)
}

func ServiceUnavailable(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusServiceUnavailable, message, nil)
}

func ServerError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, err.Error(), nil)
}
