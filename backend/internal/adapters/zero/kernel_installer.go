package zero

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zerodenet/zboard/backend/internal/nodecleanup"
)

const kernelSystemdUnit = `[Unit]
Description=Zero network kernel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
EnvironmentFile=/etc/zerodenet/zero.env
RuntimeDirectory=zerodenet
RuntimeDirectoryMode=0750
ExecStart=/usr/local/bin/zero run --control-socket /run/zerodenet/control.sock /etc/zerodenet/current.json
Restart=on-failure
RestartSec=3s
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`

var kernelSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type KernelRemoteSession interface {
	Run(command string, privileged bool) (string, error)
	Upload(path, mode string, payload []byte) error
	Close() error
}

type KernelRemoteDialer interface {
	Dial(context.Context) (KernelRemoteSession, error)
}

type KernelInstallRequest struct {
	OperationID   uint
	Binary        []byte
	BinarySHA256  string
	RuntimeConfig []byte
	ConnectorKey  string
}

type KernelInstaller struct {
	Dialer     KernelRemoteDialer
	NewStageID func() string
}

func (i KernelInstaller) Install(parent context.Context, request KernelInstallRequest) error {
	if i.Dialer == nil || request.OperationID == 0 || len(request.Binary) == 0 || len(request.RuntimeConfig) == 0 || !kernelSHA256Pattern.MatchString(request.BinarySHA256) || request.ConnectorKey == "" {
		return errors.New("invalid staged Zero binary, configuration, or connector credential")
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	session, err := i.Dialer.Dial(ctx)
	if err != nil {
		return err
	}
	defer session.Close()
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()
	stageID := uuid.NewString()
	if i.NewStageID != nil {
		stageID = i.NewStageID()
	}
	if strings.TrimSpace(stageID) == "" {
		return errors.New("empty Zero staging identity")
	}
	stage := "/tmp/zboard-zero-" + stageID
	if output, err := session.Run("install -d -m 0700 "+shellQuote(stage), false); err != nil {
		return operationError(ctx, fmt.Errorf("create Zero staging directory: %w: %s", err, output))
	}
	files := []struct {
		path string
		mode string
		data []byte
	}{
		{stage + "/zero", "0700", request.Binary},
		{stage + "/runtime.json", "0600", request.RuntimeConfig},
		{stage + "/zero.env", "0600", []byte("ZERO_PANEL_API_KEY=" + request.ConnectorKey + "\n")},
		{stage + "/zero.service", "0644", []byte(kernelSystemdUnit)},
		{stage + "/cleanup-zero-node.sh", "0700", nodecleanup.Script},
	}
	for _, file := range files {
		if err := session.Upload(file.path, file.mode, file.data); err != nil {
			return operationError(ctx, fmt.Errorf("stage %s: %w", file.path, err))
		}
	}
	output, err := session.Run(BuildKernelInstallScript(stage, request.BinarySHA256, request.OperationID), true)
	if err != nil {
		return operationError(ctx, fmt.Errorf("activate Zero (automatic rollback attempted): %w: %s", err, truncateOutput(output)))
	}
	return operationError(ctx, nil)
}

func (i KernelInstaller) Rollback(parent context.Context, operationID uint) error {
	if i.Dialer == nil || operationID == 0 {
		return errors.New("invalid Zero rollback request")
	}
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	defer cancel()
	session, err := i.Dialer.Dial(ctx)
	if err != nil {
		return fmt.Errorf("connect for Zero rollback: %w", err)
	}
	defer session.Close()
	stop := context.AfterFunc(ctx, func() { _ = session.Close() })
	defer stop()
	output, err := session.Run(BuildKernelRollbackScript(operationID), true)
	if err != nil {
		return operationError(ctx, fmt.Errorf("rollback Zero generation: %w: %s", err, truncateOutput(output)))
	}
	return operationError(ctx, nil)
}

func operationError(ctx context.Context, operationErr error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return operationErr
}

func truncateOutput(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 2000 {
		return string(runes[:2000]) + "…"
	}
	return value
}

func BuildKernelInstallScript(stage, binarySHA string, operationID uint) string {
	generation := fmt.Sprintf("/etc/zerodenet/generations/%d.json", operationID)
	backup := fmt.Sprintf("/var/lib/zerodenet/backups/%d", operationID)
	return fmt.Sprintf(`set -eu
stage=%s
generation=%s
backup=%s
expected_sha=%s
test "$(id -u)" = "0"
test "$(uname -s)" = "Linux"
test "$(uname -m)" = "x86_64"
command -v systemctl >/dev/null
actual_sha="$(sha256sum "$stage/zero" | awk '{print $1}')"
test "$actual_sha" = "$expected_sha"
set -a
. "$stage/zero.env"
set +a
"$stage/zero" build_info >/dev/null
"$stage/zero" validate "$stage/runtime.json" >/dev/null
install -d -m 0755 /usr/local/bin /etc/zerodenet/generations /var/lib/zerodenet/backups
install -d -m 0700 "$backup"
had_bin=0; had_env=0; had_service=0; old_active=0; old_enabled=0
old_link="$(readlink /etc/zerodenet/current.json 2>/dev/null || true)"
if [ -f /usr/local/bin/zero ]; then cp -a /usr/local/bin/zero "$backup/zero"; had_bin=1; fi
if [ -f /etc/zerodenet/zero.env ]; then cp -a /etc/zerodenet/zero.env "$backup/zero.env"; had_env=1; fi
if [ -f /etc/systemd/system/zero.service ]; then cp -a /etc/systemd/system/zero.service "$backup/zero.service"; had_service=1; fi
if systemctl is-active --quiet zero >/dev/null 2>&1; then old_active=1; fi
if systemctl is-enabled --quiet zero >/dev/null 2>&1; then old_enabled=1; fi
printf '%%s\n' "$had_bin" > "$backup/had_bin"
printf '%%s\n' "$had_env" > "$backup/had_env"
printf '%%s\n' "$had_service" > "$backup/had_service"
printf '%%s\n' "$old_active" > "$backup/old_active"
printf '%%s\n' "$old_enabled" > "$backup/old_enabled"
printf '%%s\n' "$old_link" > "$backup/old_link"
rollback() {
  if [ "$had_bin" = "1" ]; then
    install -m 0755 "$backup/zero" /usr/local/bin/zero.rollback
    mv -f /usr/local/bin/zero.rollback /usr/local/bin/zero
  else
    rm -f /usr/local/bin/zero
  fi
  if [ -n "$old_link" ]; then ln -sfn "$old_link" /etc/zerodenet/current.json; else rm -f /etc/zerodenet/current.json; fi
  if [ "$had_env" = "1" ]; then cp -a "$backup/zero.env" /etc/zerodenet/zero.env; else rm -f /etc/zerodenet/zero.env; fi
  if [ "$had_service" = "1" ]; then cp -a "$backup/zero.service" /etc/systemd/system/zero.service; else rm -f /etc/systemd/system/zero.service; fi
  systemctl daemon-reload >/dev/null 2>&1 || true
  if [ "$old_enabled" = "1" ]; then systemctl enable zero >/dev/null 2>&1 || true; else systemctl disable zero >/dev/null 2>&1 || true; fi
  if [ "$old_active" = "1" ]; then systemctl restart zero >/dev/null 2>&1 || true; else systemctl stop zero >/dev/null 2>&1 || true; fi
}
trap 'rc=$?; if [ "$rc" != "0" ]; then rollback; fi; exit "$rc"' EXIT
install -m 0755 "$stage/zero" /usr/local/bin/zero.next
mv -f /usr/local/bin/zero.next /usr/local/bin/zero
install -m 0600 "$stage/runtime.json" "$generation"
install -m 0600 "$stage/zero.env" /etc/zerodenet/zero.env
install -m 0644 "$stage/zero.service" /etc/systemd/system/zero.service
ln -sfn "$generation" /etc/zerodenet/current.json.next
mv -Tf /etc/zerodenet/current.json.next /etc/zerodenet/current.json
systemctl daemon-reload
systemctl enable zero >/dev/null
systemctl restart zero
install -d -m 0755 /usr/local/sbin
install -m 0755 "$stage/cleanup-zero-node.sh" /usr/local/sbin/zboard-zero-cleanup
trap - EXIT
rm -rf "$stage"
printf 'ZBOARD_KERNEL_ACTIVATED=1\n'
`, shellQuote(stage), shellQuote(generation), shellQuote(backup), shellQuote(binarySHA))
}

func BuildKernelRollbackScript(operationID uint) string {
	generation := fmt.Sprintf("/etc/zerodenet/generations/%d.json", operationID)
	backup := fmt.Sprintf("/var/lib/zerodenet/backups/%d", operationID)
	return fmt.Sprintf(`set -eu
backup=%s
generation=%s
test "$(id -u)" = "0"
test -d "$backup"
for key in had_bin had_env had_service old_active old_enabled old_link; do test -f "$backup/$key"; done
had_bin="$(cat "$backup/had_bin")"
had_env="$(cat "$backup/had_env")"
had_service="$(cat "$backup/had_service")"
old_active="$(cat "$backup/old_active")"
old_enabled="$(cat "$backup/old_enabled")"
old_link="$(cat "$backup/old_link")"
case "$had_bin$had_env$had_service$old_active$old_enabled" in *[!01]*) exit 1;; esac
if [ "$had_bin" = "1" ]; then
  install -m 0755 "$backup/zero" /usr/local/bin/zero.rollback
  mv -f /usr/local/bin/zero.rollback /usr/local/bin/zero
else
  rm -f /usr/local/bin/zero
fi
if [ -n "$old_link" ]; then ln -sfn "$old_link" /etc/zerodenet/current.json; else rm -f /etc/zerodenet/current.json; fi
if [ "$had_env" = "1" ]; then cp -a "$backup/zero.env" /etc/zerodenet/zero.env; else rm -f /etc/zerodenet/zero.env; fi
if [ "$had_service" = "1" ]; then cp -a "$backup/zero.service" /etc/systemd/system/zero.service; else rm -f /etc/systemd/system/zero.service; fi
rm -f "$generation"
systemctl daemon-reload
if [ "$old_enabled" = "1" ]; then systemctl enable zero >/dev/null; else systemctl disable zero >/dev/null 2>&1 || true; fi
if [ "$old_active" = "1" ]; then systemctl restart zero; else systemctl stop zero >/dev/null 2>&1 || true; fi
printf 'ZBOARD_KERNEL_ROLLED_BACK=1\n'
`, shellQuote(backup), shellQuote(generation))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
