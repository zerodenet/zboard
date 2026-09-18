package zero

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
)

func InspectProxyPoolConfiguration(raw string) (network.ProxyPoolConfigurationFacts, error) {
	if len(raw) == 0 || len(raw) > 128*1024 {
		return network.ProxyPoolConfigurationFacts{}, errors.New("请填写不超过 128 KiB 的代理池配置")
	}
	var path runtimeNetworkPath
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&path); err != nil {
		return network.ProxyPoolConfigurationFacts{}, errors.New("代理池需要 outbounds、outbound_groups 和 target")
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return network.ProxyPoolConfigurationFacts{}, errors.New("代理池配置只能包含一个 JSON 对象")
	}
	facts := network.ProxyPoolConfigurationFacts{SupportsDatagram: true}
	if err := path.validateDatagram(); err != nil {
		facts.SupportsDatagram = false
		facts.DatagramError = strings.TrimSpace(err.Error())
	}
	config := map[string]interface{}{}
	if _, err := path.appendGraph(config, "pool/"); err != nil {
		return network.ProxyPoolConfigurationFacts{}, err
	}
	return facts, nil
}
