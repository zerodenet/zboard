package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zerodenet/zboard/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *handlers) ManagedCertificateDeleteHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	id, err := parsePathID(r.URL.Path, "/api/v1/admin/certificates/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	h.deletionMu.Lock()
	defer h.deletionMu.Unlock()
	var certificate model.ManagedCertificate
	err = h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&certificate, id).Error; err != nil {
			return err
		}
		var references, running int64
		if err := tx.Model(&model.CertificateProtocolEndpoint{}).Where("managed_certificate_id = ?", id).Count(&references).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.CertificateOperation{}).Where("managed_certificate_id = ? AND status = ?", id, "running").Count(&running).Error; err != nil {
			return err
		}
		if references > 0 || running > 0 || certificate.Status == certificateStatusIssuing || certificate.Status == certificateStatusRenewing {
			return errors.New("请先解除协议服务引用，并等待签发或续期任务结束")
		}
		return tx.Model(&certificate).Updates(map[string]interface{}{"status": resourceStatusDeleting, "auto_renew": false, "last_error": ""}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		NotFound(w)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, err.Error(), nil)
		return
	}
	var node model.Node
	err = h.db.First(&node, certificate.NodeID).Error
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), certificateOperationTimeout)
		defer cancel()
		err = h.removeManagedCertificateRemote(ctx, node, certificate)
	}
	if err == nil {
		err = h.db.Transaction(func(tx *gorm.DB) error {
			if err := createAuditLog(tx, claims, "certificate.delete", fmt.Sprintf("certificate:%d", id), fmt.Sprintf("node=%d external_cleanup=completed", certificate.NodeID)); err != nil {
				return err
			}
			return tx.Delete(&certificate).Error
		})
	}
	if err != nil {
		_ = h.db.Model(&certificate).Updates(map[string]interface{}{"status": resourceStatusDeleting, "last_error": truncateCertificateError(err.Error())}).Error
		writeJSON(w, http.StatusBadGateway, "证书外部清理未完成，面板记录已保留；请修复后重试删除。", nil)
		return
	}
	OK(w, map[string]interface{}{"id": id, "deleted": true, "remote_files_retained": false, "external_cleanup_completed": true})
}

func (h *handlers) removeManagedCertificateRemote(ctx context.Context, node model.Node, certificate model.ManagedCertificate) error {
	// Failed issuance may have reached Certbot even before paths were persisted.
	var attempts int64
	if err := h.db.Model(&model.CertificateOperation{}).Where("managed_certificate_id = ?", certificate.ID).Count(&attempts).Error; err != nil {
		return err
	}
	if attempts == 0 && certificate.CertPath == "" && certificate.KeyPath == "" && certificate.LastIssuedAt == nil {
		return nil
	}
	if err := h.validateNodeSSH(node); err != nil {
		return err
	}
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	timeout := time.AfterFunc(certificateOperationTimeout, func() { _ = conn.Close() })
	defer timeout.Stop()
	script, err := buildCertificateDeleteScript(certificate)
	if err != nil {
		return err
	}
	output, err := h.runNodeSSHSession(conn, node, script, true)
	if err != nil {
		return fmt.Errorf("certificate cleanup failed: %w", err)
	}
	if !strings.Contains(output, "ZBOARD_CERTIFICATE_REMOVED=1") {
		return errors.New("certificate cleanup acknowledgement missing")
	}
	return nil
}

// Destructive paths derive only from the numeric ID, never editable DB paths.
func buildCertificateDeleteScript(certificate model.ManagedCertificate) (string, error) {
	if certificate.ID == 0 {
		return "", errors.New("certificate ID is required")
	}
	certPath, keyPath := managedCertificatePaths(certificate.ID)
	if (certificate.CertPath != "" && certificate.CertPath != certPath) || (certificate.KeyPath != "" && certificate.KeyPath != keyPath) {
		return "", errors.New("certificate paths are outside the managed resource directory")
	}
	server := "https://acme-v02.api.letsencrypt.org/directory"
	if certificate.Environment == certificateEnvironmentStaging {
		server = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}
	return fmt.Sprintf(certificateDeleteScript, certificate.ID, certificate.ID, shellQuote(server)), nil
}

const certificateDeleteScript = `set -eu
umask 077
test "$(id -u)" = 0
base=/etc/zboard/certificates/%d
name=zboard-%d
server=%s
for parent in /etc/zboard /etc/zboard/certificates /etc/letsencrypt /etc/letsencrypt/live /etc/letsencrypt/archive /etc/letsencrypt/renewal; do
  test ! -L "$parent" || { echo 'managed parent is a symlink' >&2; exit 1; }
done
for target in "$base" "$base/generations" "/etc/letsencrypt/live/$name" "/etc/letsencrypt/archive/$name" "/etc/letsencrypt/renewal/$name.conf"; do
  test ! -L "$target" || { echo 'managed resource root is a symlink' >&2; exit 1; }
done
command -v flock >/dev/null
exec 9>"/run/lock/zboard-certificate-$name.lock"
flock -w 10 9
certbot_bin=""
if command -v certbot >/dev/null 2>&1; then certbot_bin="$(command -v certbot)"
elif [ -x /opt/zboard-certbot/bin/certbot ]; then certbot_bin=/opt/zboard-certbot/bin/certbot
elif [ -x /opt/zboard-certbot-run ]; then certbot_bin=/opt/zboard-certbot-run
fi
revoke_pair() {
  cert="$1"
  key="$2"
  test -f "$cert" || return 0
  test ! -L "$cert"
  test -f "$key" && test ! -L "$key"
  openssl x509 -in "$cert" -noout >/dev/null
  if ! openssl x509 -in "$cert" -checkend 0 -noout >/dev/null; then return 0; fi
  test -n "$certbot_bin"
  if ! result="$("$certbot_bin" revoke --non-interactive --cert-path "$cert" --key-path "$key" --server "$server" --reason cessationofoperation --no-delete-after-revoke 2>&1)"; then
    case "$result" in
      *urn:ietf:params:acme:error:alreadyRevoked*|*"Certificate already revoked"*) ;;
      *) echo 'CA revocation failed; certificate material retained' >&2; return 1 ;;
    esac
  fi
}
archive="/etc/letsencrypt/archive/$name"
for cert in "$archive"/cert[0-9]*.pem; do
  test -f "$cert" || continue
  suffix="$(basename "$cert" | cut -c5-)"
  revoke_pair "$cert" "$archive/privkey$suffix"
done
for generation in "$base"/generations/*; do
  test -d "$generation" || continue
  test ! -L "$generation"
  revoke_pair "$generation/fullchain.pem" "$generation/privkey.pem"
done
if [ -e "/etc/letsencrypt/renewal/$name.conf" ] || [ -d "/etc/letsencrypt/live/$name" ] || [ -d "$archive" ]; then
  test -n "$certbot_bin"
  if [ -f "/etc/letsencrypt/renewal/$name.conf" ]; then
    # Certbot trusts paths in renewal files. Refuse a lineage pointing outside
    # the exact ID-owned live/archive roots before invoking its deletion API.
    awk -F ' *= *' -v live="/etc/letsencrypt/live/$name/" -v archive="$archive" '
      $1 == "archive_dir" && $2 != archive { exit 1 }
      $1 == "cert" && $2 != live "cert.pem" { exit 1 }
      $1 == "privkey" && $2 != live "privkey.pem" { exit 1 }
      $1 == "chain" && $2 != live "chain.pem" { exit 1 }
      $1 == "fullchain" && $2 != live "fullchain.pem" { exit 1 }
    ' "/etc/letsencrypt/renewal/$name.conf"
  fi
  "$certbot_bin" delete --non-interactive --cert-name "$name" >/dev/null
fi
test ! -e "/etc/letsencrypt/renewal/$name.conf"
test ! -e "/etc/letsencrypt/live/$name"
test ! -e "$archive"
rm -rf -- "$base"
test ! -e "$base"
printf 'ZBOARD_CERTIFICATE_REMOVED=1\n'
`
