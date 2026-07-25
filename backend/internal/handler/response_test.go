package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorResponsesExposeVersionedMachineReadableDetail(t *testing.T) {
	recorder := httptest.NewRecorder()
	BadRequestFields(recorder, "validation failed", map[string]string{"email": "invalid email"})

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	var response APIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Message != "validation failed" || response.Error == nil {
		t.Fatalf("response = %#v", response)
	}
	if response.Error.Version != 1 || response.Error.Code != "validation_failed" || response.Error.Fields["email"] != "invalid email" {
		t.Fatalf("error detail = %#v", response.Error)
	}
}

func TestSetupValidationReturnsStructuredFieldMap(t *testing.T) {
	body := setupRequest{SiteName: "", SiteURL: "javascript:alert(1)", AdminEmail: "invalid", AdminPassword: "short"}
	err := validateSetupRequest(&body)
	var validation *requestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %v, want requestValidationError", err)
	}
	for _, field := range []string{"site_name", "site_url", "admin_email", "admin_password"} {
		if validation.fields[field] == "" {
			t.Errorf("field %s is missing from %#v", field, validation.fields)
		}
	}
}

func TestLegacyErrorWriterKeepsMessageAndAddsGenericDetail(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeJSON(recorder, http.StatusConflict, "already exists", nil)

	var response APIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Message != "already exists" || response.Error == nil || response.Error.Code != "conflict" || response.Error.Version != 1 {
		t.Fatalf("response = %#v", response)
	}
}
