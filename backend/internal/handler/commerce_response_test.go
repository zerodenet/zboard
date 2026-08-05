package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBadRequestErrorKeepsValidationAndPersistenceFailuresDistinct(t *testing.T) {
	validationRecorder := httptest.NewRecorder()
	BadRequestError(validationRecorder, validationError("商品信息校验失败。", map[string]string{"name": "请输入商品名称。"}))
	if validationRecorder.Code != http.StatusBadRequest {
		t.Fatalf("validation status = %d, want %d", validationRecorder.Code, http.StatusBadRequest)
	}

	persistenceRecorder := httptest.NewRecorder()
	BadRequestError(persistenceRecorder, errors.New("unknown column plan_name"))
	if persistenceRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("persistence status = %d, want %d", persistenceRecorder.Code, http.StatusInternalServerError)
	}
}
