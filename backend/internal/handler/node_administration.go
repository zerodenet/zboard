package handler

import (
	"errors"
	"net/http"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func writeNodeAdministrationError(w http.ResponseWriter, err error) bool {
	var validation *networkcap.NodeAdministrationValidation
	if errors.As(err, &validation) {
		if len(validation.Fields) == 0 {
			BadRequest(w, validation.Message)
		} else {
			BadRequestFields(w, validation.Message, validation.Fields)
		}
		return true
	}
	switch {
	case errors.Is(err, networkcap.ErrNodeAdministrationNotFound), errors.Is(err, networkcap.ErrNodeCredentialNotFound):
		NotFound(w)
		return true
	case errors.Is(err, networkcap.ErrNodeAdministrationPermission):
		Forbidden(w, "管理员权限已失效。")
		return true
	case errors.Is(err, networkcap.ErrNodeAdministrationDeleting):
		writeJSON(w, http.StatusConflict, "节点已进入删除流程，请等待删除完成。", nil)
		return true
	}
	return false
}
