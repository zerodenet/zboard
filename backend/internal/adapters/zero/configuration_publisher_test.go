package zero

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestConfigurationPublisherStagesAndActivatesThroughRemotePort(t *testing.T) {
	session := &kernelRemoteSessionStub{}
	now := time.Date(2026, time.September, 16, 4, 0, 0, 0, time.UTC)
	publisher := ConfigurationPublisher{
		Dialer: &kernelRemoteDialerStub{session: session}, NewStageID: func() string { return "config-stage" }, Now: func() time.Time { return now },
	}
	result, err := publisher.Publish(context.Background(), ConfigurationPublishRequest{
		DeploymentID: 8, RuntimeConfig: []byte(`{"inbounds":[]}`), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.ActivatedAt.Equal(now) || len(session.runs) != 2 || len(session.uploads) != 2 || !session.runs[1].privileged || !strings.Contains(session.runs[1].command, "ZBOARD_CONFIG_APPLIED") {
		t.Fatalf("result=%+v runs=%+v uploads=%+v", result, session.runs, session.uploads)
	}
}

func TestConfigurationPublisherRequiresDeclaredDirectInboundUDP(t *testing.T) {
	session := &kernelRemoteSessionStub{}
	publisher := ConfigurationPublisher{Dialer: &kernelRemoteDialerStub{session: session}}
	_, err := publisher.Publish(context.Background(), ConfigurationPublishRequest{
		DeploymentID: 8, RuntimeConfig: []byte(`{"inbounds":[{"protocol":{"type":"direct"}}]}`), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret",
	})
	if err == nil || !strings.Contains(err.Error(), "direct 入站 UDP") || len(session.uploads) != 0 {
		t.Fatalf("runs=%+v uploads=%+v err=%v", session.runs, session.uploads, err)
	}
}

func TestConfigurationPublisherPropagatesCanceledDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (ConfigurationPublisher{Dialer: &kernelRemoteDialerStub{session: &kernelRemoteSessionStub{}}}).Publish(ctx, ConfigurationPublishRequest{
		DeploymentID: 8, RuntimeConfig: []byte("{}"), ConfigSHA256: strings.Repeat("a", 64), ConnectorKey: "secret",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
}

func TestSupportsDirectInboundUDP(t *testing.T) {
	supported := `protocol_capabilities:[{"protocol":"direct","compiled":true,"inbound":{"udp":{"supported":true}}}]`
	if !SupportsDirectInboundUDP(supported) || SupportsDirectInboundUDP(`protocol_capabilities:[{"protocol":"direct","compiled":false,"inbound":{"udp":{"supported":true}}}]`) {
		t.Fatal("direct UDP capability projection is incorrect")
	}
}
