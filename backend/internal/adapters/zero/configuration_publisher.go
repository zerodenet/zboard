package zero

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const controlSocket = "/run/zerodenet/control.sock"

type ConfigurationPublishRequest struct {
	DeploymentID  uint
	RuntimeConfig []byte
	ConfigSHA256  string
	ConnectorKey  string
}

type ConfigurationPublishResult struct {
	Output      string
	ActivatedAt time.Time
}

type ConfigurationPublisher struct {
	Dialer     KernelRemoteDialer
	NewStageID func() string
	Now        func() time.Time
}

func (p ConfigurationPublisher) Publish(ctx context.Context, request ConfigurationPublishRequest) (ConfigurationPublishResult, error) {
	if p.Dialer == nil || request.DeploymentID == 0 || len(request.RuntimeConfig) == 0 || !kernelSHA256Pattern.MatchString(request.ConfigSHA256) || request.ConnectorKey == "" {
		return ConfigurationPublishResult{}, errors.New("invalid Zero configuration publication request")
	}
	session, err := p.Dialer.Dial(ctx)
	if err != nil {
		return ConfigurationPublishResult{}, err
	}
	defer session.Close()
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()
	if RequiresDirectInboundUDP(request.RuntimeConfig) {
		output, err := session.Run("/usr/local/bin/zero build-info", true)
		if err != nil || !SupportsDirectInboundUDP(output) {
			if err := ctx.Err(); err != nil {
				return ConfigurationPublishResult{Output: strings.TrimSpace(output)}, err
			}
			return ConfigurationPublishResult{Output: strings.TrimSpace(output)}, fmt.Errorf("入口节点的 Zero 未声明 direct 入站 UDP 能力，请升级内核或关闭入口 UDP 转发。: %s", truncateOutput(output))
		}
	}
	stageID := uuid.NewString()
	if p.NewStageID != nil {
		stageID = p.NewStageID()
	}
	if strings.TrimSpace(stageID) == "" {
		return ConfigurationPublishResult{}, errors.New("empty Zero configuration staging identity")
	}
	stage := "/tmp/zboard-zero-config-" + stageID
	if output, err := session.Run("install -d -m 0700 "+shellQuote(stage), false); err != nil {
		return ConfigurationPublishResult{Output: strings.TrimSpace(output)}, operationError(ctx, fmt.Errorf("create Zero config staging directory: %w: %s", err, truncateOutput(output)))
	}
	if err := session.Upload(stage+"/runtime.json", "0600", request.RuntimeConfig); err != nil {
		return ConfigurationPublishResult{}, operationError(ctx, err)
	}
	if err := session.Upload(stage+"/zero.env", "0600", []byte("ZERO_PANEL_API_KEY="+request.ConnectorKey+"\n")); err != nil {
		return ConfigurationPublishResult{}, operationError(ctx, err)
	}
	activatedAt := time.Now().UTC()
	if p.Now != nil {
		activatedAt = p.Now().UTC()
	}
	output, err := session.Run(BuildConfigurationPublishScript(stage, request.ConfigSHA256, request.DeploymentID), true)
	if err != nil {
		return ConfigurationPublishResult{Output: strings.TrimSpace(output), ActivatedAt: activatedAt}, operationError(ctx, fmt.Errorf("activate Zero config (rollback attempted): %w: %s", err, truncateOutput(output)))
	}
	if err := operationError(ctx, nil); err != nil {
		return ConfigurationPublishResult{}, err
	}
	return ConfigurationPublishResult{Output: strings.TrimSpace(output), ActivatedAt: activatedAt}, nil
}

func RequiresDirectInboundUDP(raw []byte) bool {
	type policy struct {
		Enabled *bool `json:"enabled"`
	}
	var config struct {
		Runtime struct {
			UDP policy `json:"udp"`
		} `json:"runtime"`
		Inbounds []struct {
			UDP      policy `json:"udp"`
			Protocol struct {
				Type string `json:"type"`
			} `json:"protocol"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return false
	}
	if config.Runtime.UDP.Enabled != nil && !*config.Runtime.UDP.Enabled {
		return false
	}
	for _, inbound := range config.Inbounds {
		if inbound.Protocol.Type == "direct" && (inbound.UDP.Enabled == nil || *inbound.UDP.Enabled) {
			return true
		}
	}
	return false
}

func SupportsDirectInboundUDP(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		raw, found := strings.CutPrefix(strings.TrimSpace(line), "protocol_capabilities:")
		if !found {
			continue
		}
		var protocols []struct {
			Protocol string `json:"protocol"`
			Compiled bool   `json:"compiled"`
			Inbound  struct {
				UDP struct {
					Supported bool `json:"supported"`
				} `json:"udp"`
			} `json:"inbound"`
		}
		if json.Unmarshal([]byte(raw), &protocols) != nil {
			return false
		}
		for _, protocol := range protocols {
			if protocol.Protocol == "direct" {
				return protocol.Compiled && protocol.Inbound.UDP.Supported
			}
		}
	}
	return false
}

func BuildConfigurationPublishScript(stage, expectedSHA string, deploymentID uint) string {
	generation := fmt.Sprintf("/etc/zerodenet/generations/config-%d.json", deploymentID)
	backup := fmt.Sprintf("/var/lib/zerodenet/backups/config-%d", deploymentID)
	return fmt.Sprintf(`set -eu
stage=%s
generation=%s
backup=%s
expected_sha=%s
test "$(id -u)" = "0"
test -x /usr/local/bin/zero
command -v systemctl >/dev/null
set -a
. "$stage/zero.env"
set +a
/usr/local/bin/zero validate "$stage/runtime.json" >/dev/null
actual_sha="$(sha256sum "$stage/runtime.json" | awk '{print $1}')"
test "$actual_sha" = "$expected_sha"
install -d -m 0755 /etc/zerodenet/generations /var/lib/zerodenet/backups
install -d -m 0700 "$backup"
old_link="$(readlink /etc/zerodenet/current.json 2>/dev/null || true)"
had_env=0
if [ -f /etc/zerodenet/zero.env ]; then cp -a /etc/zerodenet/zero.env "$backup/zero.env"; had_env=1; fi
printf '%%s\n' "$old_link" > "$backup/old_link"
printf '%%s\n' "$had_env" > "$backup/had_env"
rollback() {
  if [ -n "$old_link" ]; then ln -sfn "$old_link" /etc/zerodenet/current.json; else rm -f /etc/zerodenet/current.json; fi
  if [ "$had_env" = "1" ]; then cp -a "$backup/zero.env" /etc/zerodenet/zero.env; else rm -f /etc/zerodenet/zero.env; fi
  systemctl restart zero >/dev/null 2>&1 || true
}
trap 'rc=$?; if [ "$rc" != "0" ]; then rollback; fi; exit "$rc"' EXIT
install -m 0600 "$stage/runtime.json" "$generation"
install -m 0600 "$stage/zero.env" /etc/zerodenet/zero.env
ln -sfn "$generation" /etc/zerodenet/current.json.next
mv -Tf /etc/zerodenet/current.json.next /etc/zerodenet/current.json
systemctl restart zero
healthy=0
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if systemctl is-active --quiet zero && /usr/local/bin/zero status --json --socket %s >/dev/null 2>&1; then healthy=1; break; fi
  sleep 1
done
test "$healthy" = "1"
test "$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')" = "$expected_sha"
trap - EXIT
rm -rf "$stage"
printf 'ZBOARD_CONFIG_APPLIED=%%s\n' "$expected_sha"
`, shellQuote(stage), shellQuote(generation), shellQuote(backup), shellQuote(expectedSHA), shellQuote(controlSocket))
}

func BuildConfigurationRollbackScript(deploymentID uint) string {
	generation := fmt.Sprintf("/etc/zerodenet/generations/config-%d.json", deploymentID)
	backup := fmt.Sprintf("/var/lib/zerodenet/backups/config-%d", deploymentID)
	return fmt.Sprintf(`set -eu
generation=%s
backup=%s
test "$(id -u)" = "0"
test -d "$backup"
old_link="$(cat "$backup/old_link" 2>/dev/null || true)"
had_env="$(cat "$backup/had_env" 2>/dev/null || printf 0)"
if [ -n "$old_link" ]; then ln -sfn "$old_link" /etc/zerodenet/current.json; else rm -f /etc/zerodenet/current.json; fi
if [ "$had_env" = "1" ]; then cp -a "$backup/zero.env" /etc/zerodenet/zero.env; else rm -f /etc/zerodenet/zero.env; fi
rm -f "$generation"
systemctl restart zero
printf 'ZBOARD_CONFIG_ROLLED_BACK=1\n'
`, shellQuote(generation), shellQuote(backup))
}
