from pathlib import Path

SOURCE = Path("backend/internal/handler/certificate_management.go")
TEST = Path("backend/internal/handler/certificate_management_test.go")


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise RuntimeError(f"{label}: expected one match, found {count}")
    return text.replace(old, new, 1)


source = SOURCE.read_text(encoding="utf-8")

source = replace_once(
    source,
    r'''\tquotedArgs := make([]string, 0, len(args))
\tfor _, argument := range args {
\t\tquotedArgs = append(quotedArgs, shellQuote(argument))
\t}
\treturn fmt.Sprintf(`set -eu
'''.replace("\\t", "\t"),
    r'''\tquotedArgs := make([]string, 0, len(args))
\tfor _, argument := range args {
\t\tquotedArgs = append(quotedArgs, shellQuote(argument))
\t}
\tquotedDomains := make([]string, 0, len(domains))
\tfor _, domain := range domains {
\t\tquotedDomains = append(quotedDomains, shellQuote(domain))
\t}
\thttp01PreflightToken := "zboard-http01-preflight-" + stagingID
\treturn fmt.Sprintf(`set -eu
'''.replace("\\t", "\t"),
    "prepare HTTP-01 preflight arguments",
)

source = replace_once(
    source,
    r'''stage_dir=%s
install -d -m 0700 "$stage_dir"
cleanup_challenge() { rm -f "$stage_dir/cloudflare.ini"; }
trap cleanup_challenge EXIT HUP INT TERM
if [ %s = dns-01 ]; then
''',
    r'''stage_dir=%s
install -d -m 0700 "$stage_dir"
http01_challenge_file=""
cleanup_challenge() {
  rm -f "$stage_dir/cloudflare.ini"
  if [ -n "$http01_challenge_file" ]; then rm -f "$http01_challenge_file"; fi
}
trap cleanup_challenge EXIT HUP INT TERM
if [ %s = http-01-webroot ]; then
  webroot=%s
  challenge_token=%s
  challenge_dir="$webroot/.well-known/acme-challenge"
  http01_challenge_file="$challenge_dir/$challenge_token"
  install -d -m 0755 "$challenge_dir"
  printf '%%s' "$challenge_token" > "$http01_challenge_file"
  chmod 0644 "$http01_challenge_file"
  fetch_http01_challenge() {
    if command -v curl >/dev/null 2>&1; then
      curl --fail --silent --show-error --insecure --location --proto '=http,https' --proto-redir '=http,https' --connect-timeout 5 --max-time 20 "$1"
    elif command -v wget >/dev/null 2>&1; then
      wget -qO- --no-check-certificate --timeout=20 "$1"
    else
      printf 'ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight requires curl or wget on the node\n' >&2
      return 1
    fi
  }
  for domain in %s; do
    challenge_url="http://$domain/.well-known/acme-challenge/$challenge_token"
    if ! challenge_body="$(fetch_http01_challenge "$challenge_url")"; then
      printf 'ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight could not fetch %%s; verify that %%s serves %%s\n' "$challenge_url" "$domain" "$webroot" >&2
      exit 1
    fi
    if [ "$challenge_body" != "$challenge_token" ]; then
      printf 'ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight returned unexpected content for %%s; verify that %%s maps to %%s\n' "$domain" "$challenge_url" "$webroot" >&2
      exit 1
    fi
  done
  rm -f "$http01_challenge_file"
  http01_challenge_file=""
fi
if [ %s = dns-01 ]; then
''',
    "add HTTP-01 Webroot reachability preflight",
)

source = replace_once(
    source,
    r'''  plugin_installed=0
  if command -v apt-get >/dev/null 2>&1; then
    if DEBIAN_FRONTEND=noninteractive apt-get install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
  elif command -v dnf >/dev/null 2>&1; then
    if dnf install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
  elif command -v yum >/dev/null 2>&1; then
    if yum install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
  elif command -v apk >/dev/null 2>&1; then
    if apk add --no-cache certbot-dns-cloudflare; then plugin_installed=1; fi
  fi
  if [ "$plugin_installed" = 0 ]; then
    if command -v apt-get >/dev/null 2>&1; then
      DEBIAN_FRONTEND=noninteractive apt-get install -y python3 python3-venv
    elif command -v dnf >/dev/null 2>&1; then
      dnf install -y python3
    elif command -v yum >/dev/null 2>&1; then
      yum install -y python3
    elif command -v apk >/dev/null 2>&1; then
      apk add --no-cache python3 py3-pip
    fi
    command -v python3 >/dev/null 2>&1
    certbot_venv=/opt/zboard-certbot
    python3 -m venv "$certbot_venv"
    "$certbot_venv/bin/pip" install --disable-pip-version-check --upgrade certbot certbot-dns-cloudflare
    certbot_bin="$certbot_venv/bin/certbot"
  fi
''',
    r'''  plugin_installed=0
  python_ready=0
  if "$certbot_bin" plugins 2>/dev/null | grep -q 'dns-cloudflare'; then
    plugin_installed=1
  fi
  if [ "$plugin_installed" = 0 ]; then
    if command -v apt-get >/dev/null 2>&1; then
      DEBIAN_FRONTEND=noninteractive apt-get update >/dev/null 2>&1 || true
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
    elif command -v dnf >/dev/null 2>&1; then
      if dnf install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
    elif command -v yum >/dev/null 2>&1; then
      if yum install -y python3-certbot-dns-cloudflare; then plugin_installed=1; fi
    elif command -v apk >/dev/null 2>&1; then
      if apk add --no-cache certbot-dns-cloudflare; then plugin_installed=1; fi
    fi
  fi
  if [ "$plugin_installed" = 1 ] && ! "$certbot_bin" plugins 2>/dev/null | grep -q 'dns-cloudflare'; then
    plugin_installed=0
  fi
  if [ "$plugin_installed" = 0 ]; then
    if command -v python3 >/dev/null 2>&1; then
      python_ready=1
    elif command -v apt-get >/dev/null 2>&1; then
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3; then python_ready=1; fi
    elif command -v dnf >/dev/null 2>&1; then
      if dnf install -y python3; then python_ready=1; fi
    elif command -v yum >/dev/null 2>&1; then
      if yum install -y python3; then python_ready=1; fi
    elif command -v apk >/dev/null 2>&1; then
      if apk add --no-cache python3; then python_ready=1; fi
    fi
  fi
  if [ "$plugin_installed" = 0 ] && [ "$python_ready" = 1 ]; then
    certbot_venv=/opt/zboard-certbot
    venv_ready=0
    if python3 -m venv "$certbot_venv" >/dev/null 2>&1; then
      venv_ready=1
    elif command -v apt-get >/dev/null 2>&1; then
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3-venv && python3 -m venv "$certbot_venv"; then
        venv_ready=1
      elif python_version="$(python3 -c 'import sys; print(f"{sys.version_info.major}.{sys.version_info.minor}")')" &&
        DEBIAN_FRONTEND=noninteractive apt-get install -y "python${python_version}-venv" &&
        python3 -m venv "$certbot_venv"; then
        venv_ready=1
      fi
    fi
    if [ "$venv_ready" = 1 ]; then
      if "$certbot_venv/bin/pip" install --disable-pip-version-check --upgrade certbot certbot-dns-cloudflare; then
        certbot_bin="$certbot_venv/bin/certbot"
        plugin_installed=1
      fi
    fi
  fi
  if [ "$plugin_installed" = 0 ] && [ "$python_ready" = 1 ]; then
    pip_ready=0
    if python3 -m pip --version >/dev/null 2>&1; then
      pip_ready=1
    elif python3 -m ensurepip --upgrade >/dev/null 2>&1 && python3 -m pip --version >/dev/null 2>&1; then
      pip_ready=1
    elif command -v apt-get >/dev/null 2>&1; then
      if DEBIAN_FRONTEND=noninteractive apt-get install -y python3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    elif command -v dnf >/dev/null 2>&1; then
      if dnf install -y python3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    elif command -v yum >/dev/null 2>&1; then
      if yum install -y python3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    elif command -v apk >/dev/null 2>&1; then
      if apk add --no-cache py3-pip && python3 -m pip --version >/dev/null 2>&1; then pip_ready=1; fi
    fi
    if [ "$pip_ready" = 1 ]; then
      certbot_target=/opt/zboard-certbot-packages
      certbot_wrapper=/opt/zboard-certbot-run
      install -d -m 0755 "$certbot_target"
      if python3 -m pip install --disable-pip-version-check --upgrade --target "$certbot_target" certbot certbot-dns-cloudflare; then
        cat > "$certbot_wrapper" <<'ZBOARD_CERTBOT_WRAPPER'
#!/bin/sh
PYTHONPATH=/opt/zboard-certbot-packages exec python3 -m certbot "$@"
ZBOARD_CERTBOT_WRAPPER
        chmod 0755 "$certbot_wrapper"
        certbot_bin="$certbot_wrapper"
        plugin_installed=1
      fi
    fi
  fi
  if [ "$plugin_installed" = 0 ] || ! "$certbot_bin" plugins 2>/dev/null | grep -q 'dns-cloudflare'; then
    printf 'ZBOARD_CERT_ERROR=unable to install Certbot Cloudflare DNS plugin; system package, Python venv, and pip target fallbacks failed\n' >&2
    exit 1
  fi
''',
    "make DNS-01 plugin installation resilient",
)

source = replace_once(
    source,
    r'''`, shellQuote(stageDir), shellQuote(certificate.ChallengeType), strings.Join(quotedArgs, " "), shellQuote("/etc/letsencrypt/live/"+certName), shellQuote(baseDir))
''',
    r'''`, shellQuote(stageDir), shellQuote(certificate.ChallengeType), shellQuote(certificate.WebrootPath), shellQuote(http01PreflightToken),
\t\tstrings.Join(quotedDomains, " "), shellQuote(certificate.ChallengeType), strings.Join(quotedArgs, " "),
\t\tshellQuote("/etc/letsencrypt/live/"+certName), shellQuote(baseDir))
'''.replace("\\t", "\t"),
    "wire generated-script format arguments",
)

SOURCE.write_text(source, encoding="utf-8")

test = TEST.read_text(encoding="utf-8")

test = replace_once(
    test,
    r'''import (
\t"context"
\t"errors"
\t"net"
\t"strings"
\t"testing"
\t"time"
'''.replace("\\t", "\t"),
    r'''import (
\t"context"
\t"errors"
\t"net"
\t"os/exec"
\t"strings"
\t"testing"
\t"time"
'''.replace("\\t", "\t"),
    "add shell syntax test import",
)

test = replace_once(
    test,
    r'''\tfor _, expected := range []string{"python3-certbot-dns-cloudflare", "python3 -m venv", "certbot-dns-cloudflare", `"$certbot_bin"`} {
'''.replace("\\t", "\t"),
    r'''\tfor _, expected := range []string{
\t\t"python3-certbot-dns-cloudflare", "plugins 2>/dev/null | grep -q 'dns-cloudflare'", "python3 -m venv",
\t\t"python3 -m ensurepip", "python3-pip", "--target", "certbot-dns-cloudflare", `"$certbot_bin"`,
\t\t"ZBOARD_CERT_ERROR=unable to install Certbot Cloudflare DNS plugin",
\t} {
'''.replace("\\t", "\t"),
    "expand DNS fallback assertions",
)

test = replace_once(
    test,
    r'''\tfor _, expected := range []string{"--webroot", "--webroot-path", "/var/www/acme"} {
'''.replace("\\t", "\t"),
    r'''\tfor _, expected := range []string{
\t\t"--webroot", "--webroot-path", "/var/www/acme", "zboard-http01-preflight-stage-id",
\t\t".well-known/acme-challenge", "curl --fail", "wget -qO-", "edge.example.com",
\t\t"ZBOARD_CERT_ERROR=HTTP-01 Webroot preflight",
\t} {
'''.replace("\\t", "\t"),
    "expand Webroot preflight assertions",
)

test += r'''

func TestBuildCertbotCertificateScriptsHaveValidShellSyntax(t *testing.T) {
\tsh, err := exec.LookPath("sh")
\tif err != nil {
\t\tt.Skip("POSIX shell is not available")
\t}
\tcertificates := []model.ManagedCertificate{
\t\t{ID: 9, ContactEmail: "admin@example.com", Environment: certificateEnvironmentProduction, ChallengeType: certificateChallengeDNS01},
\t\t{ID: 10, ContactEmail: "admin@example.com", Environment: certificateEnvironmentProduction, ChallengeType: certificateChallengeHTTP01Webroot, WebrootPath: "/var/www/acme"},
\t}
\tfor _, certificate := range certificates {
\t\tscript := buildCertbotCertificateScript(certificate, []string{"edge.example.com"}, false, "stage-id")
\t\tcommand := exec.Command(sh, "-n")
\t\tcommand.Stdin = strings.NewReader(script)
\t\tif output, err := command.CombinedOutput(); err != nil {
\t\t\tt.Fatalf("challenge %s generated invalid shell: %v: %s", certificate.ChallengeType, err, output)
\t\t}
\t}
}
'''.replace("\\t", "\t")

TEST.write_text(test, encoding="utf-8")
