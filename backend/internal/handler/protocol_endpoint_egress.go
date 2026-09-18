package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	networkcap "github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

type protocolEndpointEgressParseRequest struct {
	Input string `json:"input"`
}

type protocolEndpointEgressParseResult struct {
	Protocol string                 `json:"protocol"`
	Config   map[string]interface{} `json:"config"`
}

func (h *handlers) ProtocolEndpointEgressParseHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var request protocolEndpointEgressParseRequest
	if err := decodeBody(r, &request); err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := parseProtocolEndpointEgress(request.Input)
	if err != nil {
		BadRequestFields(w, "协议出口解析失败。", map[string]string{"egress_import": err.Error()})
		return
	}
	OK(w, result)
}

func parseProtocolEndpointEgress(input string) (protocolEndpointEgressParseResult, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return protocolEndpointEgressParseResult{}, errors.New("请输入分享链接或 Zero 出口 JSON。")
	}
	if len(input) > 16*1024 {
		return protocolEndpointEgressParseResult{}, errors.New("出口配置不能超过 16 KiB。")
	}
	var protocol map[string]interface{}
	if strings.HasPrefix(input, "{") {
		var document map[string]interface{}
		if err := json.Unmarshal([]byte(input), &document); err != nil {
			return protocolEndpointEgressParseResult{}, errors.New("出口 JSON 格式不正确。")
		}
		if nested, ok := document["protocol"].(map[string]interface{}); ok {
			protocol = nested
		} else {
			protocol = document
		}
	} else if parsed, err := url.Parse(input); err == nil && (strings.EqualFold(parsed.Scheme, "socks") || strings.EqualFold(parsed.Scheme, "socks5")) {
		if parsed.Hostname() == "" {
			return protocolEndpointEgressParseResult{}, errors.New("SOCKS5 链接缺少服务器地址。")
		}
		port := 1080
		if parsed.Port() != "" {
			port, err = strconv.Atoi(parsed.Port())
			if err != nil || port < 1 || port > 65535 {
				return protocolEndpointEgressParseResult{}, errors.New("SOCKS5 链接端口无效。")
			}
		}
		protocol = map[string]interface{}{"type": "socks5", "server": parsed.Hostname(), "port": port}
		if parsed.User != nil {
			if username := parsed.User.Username(); username != "" {
				protocol["username"] = username
			}
			if password, ok := parsed.User.Password(); ok {
				protocol["password"] = password
			}
		}
	} else {
		path, count, err := proxyPoolPathFromLinks([]byte(input))
		if err != nil {
			return protocolEndpointEgressParseResult{}, err
		}
		if count != 1 || len(path.Outbounds) != 1 {
			return protocolEndpointEgressParseResult{}, errors.New("每个协议入口只能配置一个出口，请只导入一个节点。")
		}
		protocol, _ = path.Outbounds[0]["protocol"].(map[string]interface{})
	}
	encoded, err := json.Marshal(protocol)
	if err != nil {
		return protocolEndpointEgressParseResult{}, errors.New("无法读取出口配置。")
	}
	kind, canonical, err := networkcap.NormalizeProtocolEndpointEgressConfig(string(encoded))
	if err != nil {
		var validation *networkcap.ProtocolEndpointMutationValidation
		if errors.As(err, &validation) {
			return protocolEndpointEgressParseResult{}, errors.New(validation.Fields["egress_config"])
		}
		return protocolEndpointEgressParseResult{}, err
	}
	if canonical == "" {
		return protocolEndpointEgressParseResult{}, errors.New("导入内容不是代理出口。")
	}
	if err := json.Unmarshal([]byte(canonical), &protocol); err != nil {
		return protocolEndpointEgressParseResult{}, err
	}
	return protocolEndpointEgressParseResult{Protocol: kind, Config: protocol}, nil
}
