package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubscriptionCamouflageRedirectUsesConfiguredTargetAndNoStore(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/client/subscription/invalid", nil)
	recorder := httptest.NewRecorder()
	writeSubscriptionCamouflageRedirect(recorder, request, subscriptionCamouflageTarget("https://www.example.com/cover", "https://panel.example.com"))

	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != "https://www.example.com/cover" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("camouflage response = code %d headers %#v", recorder.Code, recorder.Header())
	}
	if target := subscriptionCamouflageTarget("", "https://panel.example.com"); target != "https://panel.example.com" {
		t.Fatalf("site fallback target = %q", target)
	}
}

func TestNativeSubscriptionManifestDeliveryIsBase64Text(t *testing.T) {
	recorder := httptest.NewRecorder()
	manifest := subscriptionManifest{Version: "zboard.subscription/v1"}
	if err := writeBase64SubscriptionManifest(recorder, manifest, subscriptionDeliveryNative); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(recorder.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	var got subscriptionManifest
	if err := json.Unmarshal(decoded, &got); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/plain; charset=utf-8" || recorder.Header().Get("X-Zboard-Subscription-Format") != "native-base64" || got.Version != manifest.Version {
		t.Fatalf("native delivery = code %d headers %#v payload %#v", recorder.Code, recorder.Header(), got)
	}
}

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
