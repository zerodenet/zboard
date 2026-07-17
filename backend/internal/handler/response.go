package handler

import (
	"encoding/json"
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
	Timestamp string      `json:"timestamp"`
}

func writeJSON(w http.ResponseWriter, code int, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(APIResponse{
		Code:      code,
		Message:   message,
		Data:      data,
		Timestamp: time.Now().Format(time.RFC3339),
	})
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
