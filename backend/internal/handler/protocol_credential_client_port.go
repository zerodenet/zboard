package handler

import (
	"strings"

	"github.com/zerodenet/zboard/backend/internal/model"
)

func protocolCredentialClientPort(endpoint model.ProtocolEndpoint, credential model.ProtocolCredential) int {
	if strings.EqualFold(strings.TrimSpace(endpoint.Protocol), "shadowsocks") {
		return endpoint.PublicPort
	}
	return credential.PublicPort
}
