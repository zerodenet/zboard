package handler

import (
	"errors"
	"net/http"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func writeNodeGroupMutationError(w http.ResponseWriter, err error) {
	var validation *network.NodeGroupMutationValidation
	var precondition *network.NodeGroupMutationPrecondition
	var conflict *network.NodeGroupMutationConflict
	switch {
	case errors.As(err, &validation):
		if len(validation.Fields) > 0 {
			BadRequestFields(w, validation.Message, validation.Fields)
		} else {
			BadRequest(w, validation.Message)
		}
	case errors.As(err, &precondition):
		writeJSON(w, http.StatusPreconditionRequired, "保存节点组前需要提供当前版本号。", map[string]interface{}{"current_revision": precondition.CurrentRevision})
	case errors.As(err, &conflict):
		writeJSON(w, http.StatusConflict, "节点组已被其他管理员更新，请重新加载最新版本。", map[string]interface{}{"current_revision": conflict.CurrentRevision})
	case errors.Is(err, network.ErrNodeGroupNotFound):
		NotFound(w)
	case errors.Is(err, network.ErrNodeGroupMutationPermission):
		Forbidden(w, "管理员权限已失效。")
	default:
		ServerError(w, err)
	}
}
