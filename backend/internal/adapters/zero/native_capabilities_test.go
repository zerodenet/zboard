package zero

import (
	"errors"
	"strings"
	"testing"
)

func TestNativeBuildContractRequiresActualFeaturesAndProtocols(t *testing.T) {
	config := []byte(`{"inbounds":[{"protocol":{"type":"vless"}}]}`)
	if info, err := ParseNativeBuildInfo(nativeTestBuildInfo, config); err != nil || !info.Prerelease || info.Version != NativeBaselineVersion {
		t.Fatalf("expected the complete prerelease baseline, got %+v error=%v", info, err)
	}
	for _, version := range []string{"0.0.15", "0.0.3-dev.202610100641", "0.0.3-dev.202610102232", "0.0.4-rc.1", "custom-build"} {
		if info, err := ParseNativeBuildInfo(strings.Replace(nativeTestBuildInfo, NativeBaselineVersion, version, 1), config); err != nil || info.Version != version {
			t.Fatalf("version %s blocked an available capability: %+v %v", version, info, err)
		}
	}
	if _, err := ParseNativeBuildInfo(strings.Replace(nativeTestBuildInfo, `"protocol":"vless","compiled":true`, `"protocol":"vless","compiled":false`, 1), config); !errors.Is(err, ErrConfigurationCapabilityUnavailable) {
		t.Fatalf("unsupported setting should retain current state: %v", err)
	}
}

func TestConnectorCapabilityIsRequiredOnlyWhenConfigured(t *testing.T) {
	info := strings.Replace(nativeTestBuildInfo, "status-api,connector", "status-api", 1)
	if _, err := ParseNativeBuildInfo(info, []byte(`{"inbounds":[]}`)); err != nil {
		t.Fatalf("unused connector capability blocked configuration: %v", err)
	}
	if _, err := ParseNativeBuildInfo(info, []byte(`{"api":{"event_sinks":[{"kind":"webhook"}]}}`)); !errors.Is(err, ErrConfigurationCapabilityUnavailable) {
		t.Fatalf("missing configured connector capability: %v", err)
	}
}

func TestMissingBuildDeclarationsDeferToNativeValidation(t *testing.T) {
	info, err := ParseNativeBuildInfo("build_id: custom\n", []byte(`{"inbounds":[{"protocol":{"type":"vless"}}]}`))
	if err != nil || info.FeaturesDeclared || info.ProtocolsDeclared {
		t.Fatalf("unknown declarations were treated as absent compiled capabilities: %+v %v", info, err)
	}
}
