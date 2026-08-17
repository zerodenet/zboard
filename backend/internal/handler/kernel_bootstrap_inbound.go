package handler

const zeroBootstrapInboundTag = "zboard-control-bootstrap"

// zeroBootstrapControlInbound keeps Zero's runtime and Connector alive before
// a VPS has any real protocol endpoint. Direct without a target accepts a TCP
// connection and immediately returns without creating a proxy session.
// Port 0 asks the OS for an ephemeral loopback port, avoiding collisions and
// ensuring the placeholder is never a stable service endpoint.
func zeroBootstrapControlInbound() map[string]interface{} {
	return map[string]interface{}{
		"tag": zeroBootstrapInboundTag,
		"listen": map[string]interface{}{
			"address": "127.0.0.1",
			"port":    0,
		},
		"protocol": map[string]interface{}{
			"type": "direct",
		},
	}
}
