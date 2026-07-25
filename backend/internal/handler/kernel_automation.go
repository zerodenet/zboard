package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	zeroReleaseAPI       = "https://api.github.com/repos/zerodenet/zero/releases/latest"
	zeroLinuxAsset       = "zero-linux-x86_64.tar.gz"
	zeroBinaryMaxBytes   = 64 << 20
	zeroArtifactMaxBytes = 128 << 20
	zeroControlSocket    = "/run/zerodenet/control.sock"
	zeroHeartbeatTimeout = 55 * time.Second
)

var (
	errKernelOperationRunning    = errors.New("another kernel operation is already running for this node")
	errKernelPlatformUnsupported = errors.New("the official Zero artifact is incompatible with this platform")
	stableZeroTagPattern         = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	managedZeroArtifactPattern   = regexp.MustCompile(`^zero-v[0-9]+\.[0-9]+\.[0-9]+-linux-x86_64-musl\.tar\.gz$`)
	sha256Pattern                = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type zeroRelease struct {
	Version        string `json:"version"`
	Tag            string `json:"tag"`
	ArtifactURL    string `json:"artifact_url"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	ArtifactSize   int64  `json:"artifact_size"`
	LocalPath      string `json:"-"`
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
	Assets     []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

type kernelProbe struct {
	OperatingSystem string
	Architecture    string
	Libc            string
	Systemd         bool
	Installed       bool
	Version         string
	BinarySHA256    string
	ConfigSHA256    string
	ServiceStatus   string
	ControlStatus   string
}

type pendingNodeCredential struct {
	Raw       string
	Encrypted string
	Prefix    string
	IsNew     bool
}

func (h *handlers) LatestKernelReleaseHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	release, err := resolveLatestZeroRelease(r.Context())
	if err != nil {
		ServerError(w, fmt.Errorf("resolve latest Zero release: %w", err))
		return
	}
	OK(w, release)
}

func (h *handlers) NodeKernelStateHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	if _, err := h.loadNode(nodeID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	state, err := h.ensureKernelState(nodeID)
	if err != nil {
		ServerError(w, err)
		return
	}
	operations := make([]model.NodeOperation, 0)
	if err := h.db.Where("node_id = ?", nodeID).Order("id desc").Limit(20).Find(&operations).Error; err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"state": state, "operations": operations})
}

func (h *handlers) NodeKernelDetectHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}
	operation, err := h.beginKernelOperation(node.ID, claims, "detect")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	probe, probeErr := h.probeNodeKernel(node)
	if probeErr != nil {
		_ = h.failKernelOperation(operation.ID, node.ID, "detecting", probeErr)
		BadRequest(w, probeErr.Error())
		return
	}
	state, err := h.completeKernelDetection(operation, probe)
	if err != nil {
		ServerError(w, err)
		return
	}
	OK(w, map[string]interface{}{"state": state, "operation": operation})
}

func (h *handlers) NodeKernelReconcileHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		ServerError(w, err)
		return
	}
	if err := h.validateNodeSSH(node); err != nil {
		BadRequest(w, err.Error())
		return
	}
	operation, err := h.beginKernelOperation(node.ID, claims, "reconcile")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}

	result, reconcileErr := h.reconcileNodeKernel(r.Context(), node, &operation)
	if reconcileErr != nil {
		_ = h.failKernelOperation(operation.ID, node.ID, operation.Phase, reconcileErr)
		BadRequest(w, reconcileErr.Error())
		return
	}
	OK(w, result)
}

func (h *handlers) reconcileNodeKernel(ctx context.Context, node model.Node, operation *model.NodeOperation) (map[string]interface{}, error) {
	if err := h.setKernelOperationPhase(operation, "detecting"); err != nil {
		return nil, err
	}
	probe, err := h.probeNodeKernel(node)
	if err != nil {
		return nil, err
	}
	if err := h.updateKernelState(node.ID, map[string]interface{}{
		"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
		"installed_version": probe.Version, "installed_sha256": probe.BinarySHA256,
		"applied_config_sha256": probe.ConfigSHA256, "service_status": probe.ServiceStatus,
		"control_status": probe.ControlStatus, "last_detected_at": time.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	if probe.Architecture != "x86_64" || !probe.Systemd {
		return nil, fmt.Errorf("%w: automatic installation requires Linux x86_64 with systemd (os=%s arch=%s systemd=%t)", errKernelPlatformUnsupported, probe.OperatingSystem, probe.Architecture, probe.Systemd)
	}
	if err := h.setKernelOperationPhase(operation, "resolving_release"); err != nil {
		return nil, err
	}
	release, err := h.resolveZeroRelease(ctx, probe)
	if err != nil {
		return nil, fmt.Errorf("resolve Zero release: %w", err)
	}
	operation.DesiredVersion = release.Version
	operation.DesiredSHA256 = release.ArtifactSHA256
	operation.ArtifactURL = release.ArtifactURL
	if err := h.db.Model(operation).Updates(map[string]interface{}{
		"desired_version": release.Version,
		"desired_sha256":  release.ArtifactSHA256,
		"artifact_url":    release.ArtifactURL,
	}).Error; err != nil {
		return nil, err
	}

	credential, err := h.nodeConnectorCredential(node)
	if err != nil {
		return nil, err
	}
	runtimeConfig, configSHA, err := h.compileNodeRuntimeConfig(node, credential.Raw)
	if err != nil {
		return nil, err
	}

	if compareZeroVersions(probe.Version, release.Version) > 0 {
		_ = h.updateKernelState(node.ID, map[string]interface{}{
			"status":                h.kernelStatus(probe),
			"phase":                 "idle",
			"recommended_action":    "manual_review",
			"desired_version":       release.Version,
			"desired_config_sha256": configSHA,
		})
		return nil, fmt.Errorf("installed Zero %s is newer than the latest stable release %s; automatic downgrade is refused", probe.Version, release.Version)
	}

	if err := h.setKernelOperationPhase(operation, "downloading"); err != nil {
		return nil, err
	}
	binary, binarySHA, err := downloadZeroBinary(ctx, release)
	if err != nil {
		return nil, err
	}
	action := classifyKernelAction(probe, release.Version, binarySHA, configSHA)
	operation.OperationType = action
	if err := h.db.Model(operation).Update("operation_type", action).Error; err != nil {
		return nil, err
	}
	if action == "none" {
		state, err := h.finishKernelOperation(operation, probe, release, binarySHA, configSHA, "Zero is already at the desired binary and configuration")
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"state": state, "operation": operation, "changed": false}, nil
	}

	if err := h.setKernelOperationPhase(operation, "staging"); err != nil {
		return nil, err
	}
	activationStartedAt := time.Now().UTC()
	if err := h.installNodeKernel(node, operation.ID, binary, binarySHA, runtimeConfig, credential.Raw); err != nil {
		return nil, err
	}
	rollbackAfterActivation := func(cause error) error {
		rollbackErr := h.rollbackNodeKernel(node, operation.ID)
		credentialErr := h.restoreGeneratedNodeCredential(node, credential)
		if rollbackErr != nil || credentialErr != nil {
			return fmt.Errorf("%w; automatic rollback incomplete (kernel=%v credential=%v)", cause, rollbackErr, credentialErr)
		}
		return fmt.Errorf("%w; the activated generation was rolled back", cause)
	}
	if err := h.setKernelOperationPhase(operation, "verifying"); err != nil {
		return nil, rollbackAfterActivation(err)
	}
	verified, err := h.probeNodeKernel(node)
	if err != nil {
		return nil, rollbackAfterActivation(fmt.Errorf("post-install probe failed: %w", err))
	}
	if !verified.Installed || verified.BinarySHA256 != binarySHA || verified.ServiceStatus != "active" || verified.ControlStatus != "healthy" {
		return nil, rollbackAfterActivation(fmt.Errorf("post-install verification failed (installed=%t sha_match=%t service=%s control=%s)", verified.Installed, verified.BinarySHA256 == binarySHA, verified.ServiceStatus, verified.ControlStatus))
	}
	if credential.IsNew {
		if err := h.db.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]interface{}{
			"node_credential":            credential.Encrypted,
			"node_credential_prefix":     credential.Prefix,
			"node_credential_revoked_at": nil,
		}).Error; err != nil {
			return nil, rollbackAfterActivation(fmt.Errorf("activate generated connector credential: %w", err))
		}
	}
	if err := h.setKernelOperationPhase(operation, "waiting_heartbeat"); err != nil {
		return nil, rollbackAfterActivation(err)
	}
	heartbeatAt, err := h.waitForNodeConnectorHeartbeat(ctx, node.ID, activationStartedAt)
	if err != nil {
		return nil, rollbackAfterActivation(fmt.Errorf("panel heartbeat verification failed: %w", err))
	}
	state, err := h.finishKernelOperation(operation, verified, release, binarySHA, configSHA, fmt.Sprintf("Zero %s %s and passed systemd, control-socket, and panel-heartbeat health checks at %s", release.Version, action, heartbeatAt.Format(time.RFC3339)))
	if err != nil {
		return nil, rollbackAfterActivation(fmt.Errorf("persist successful Zero operation: %w", err))
	}
	_ = h.db.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]interface{}{
		"version":         verified.Version,
		"ssh_verified_at": time.Now().UTC(),
	}).Error
	return map[string]interface{}{"state": state, "operation": operation, "changed": true, "action": action}, nil
}

func resolveLatestZeroRelease(parent context.Context) (zeroRelease, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	client := zeroHTTPClient()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, zeroReleaseAPI, nil)
	if err != nil {
		return zeroRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "zboard-kernel-automation")
	response, err := client.Do(request)
	if err != nil {
		return zeroRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return zeroRelease{}, fmt.Errorf("GitHub release API returned %s", response.Status)
	}
	var payload githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&payload); err != nil {
		return zeroRelease{}, err
	}
	if payload.Draft || payload.Prerelease || !stableZeroTagPattern.MatchString(payload.TagName) {
		return zeroRelease{}, errors.New("latest Zero release is not a stable version")
	}
	var archiveURL, checksumURL string
	var archiveSize int64
	for _, asset := range payload.Assets {
		switch asset.Name {
		case zeroLinuxAsset:
			archiveURL, archiveSize = asset.BrowserDownloadURL, asset.Size
		case zeroLinuxAsset + ".sha256":
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if archiveURL == "" || checksumURL == "" || archiveSize <= 0 || archiveSize > zeroArtifactMaxBytes {
		return zeroRelease{}, errors.New("stable release is missing a valid Linux x86_64 artifact or checksum")
	}
	checksum, err := fetchSmallText(ctx, client, checksumURL, 4096)
	if err != nil {
		return zeroRelease{}, fmt.Errorf("download release checksum: %w", err)
	}
	fields := strings.Fields(checksum)
	if len(fields) < 2 || fields[1] != zeroLinuxAsset || !sha256Pattern.MatchString(strings.ToLower(fields[0])) {
		return zeroRelease{}, errors.New("release checksum file is invalid")
	}
	return zeroRelease{
		Version: strings.TrimPrefix(payload.TagName, "v"), Tag: payload.TagName,
		ArtifactURL: archiveURL, ArtifactSHA256: strings.ToLower(fields[0]), ArtifactSize: archiveSize,
	}, nil
}

func (h *handlers) resolveZeroRelease(ctx context.Context, probe kernelProbe) (zeroRelease, error) {
	official, err := resolveLatestZeroRelease(ctx)
	if err != nil {
		return zeroRelease{}, err
	}
	if supportsOfficialZeroArtifact(probe) {
		return official, nil
	}
	managed, err := resolveManagedZeroRelease(h.zeroArtifactDir, official)
	if err != nil {
		return zeroRelease{}, fmt.Errorf(
			"%w: the official GNU artifact requires glibc >= 2.34, the node reports %s, and the matching managed musl artifact is unavailable: %v",
			errKernelPlatformUnsupported,
			probe.Libc,
			err,
		)
	}
	return managed, nil
}

func resolveManagedZeroRelease(artifactDir string, official zeroRelease) (zeroRelease, error) {
	if strings.TrimSpace(artifactDir) == "" {
		return zeroRelease{}, errors.New("ZBOARD_ZERO_ARTIFACT_DIR is not configured")
	}
	if !stableZeroTagPattern.MatchString(official.Tag) {
		return zeroRelease{}, errors.New("the desired Zero tag is not stable")
	}
	name := fmt.Sprintf("zero-%s-linux-x86_64-musl.tar.gz", official.Tag)
	if !managedZeroArtifactPattern.MatchString(name) {
		return zeroRelease{}, errors.New("the managed artifact name is invalid")
	}
	root, err := filepath.Abs(artifactDir)
	if err != nil {
		return zeroRelease{}, fmt.Errorf("resolve managed artifact directory: %w", err)
	}
	archivePath := filepath.Join(root, name)
	relative, err := filepath.Rel(root, archivePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return zeroRelease{}, errors.New("managed artifact escapes the configured directory")
	}
	info, err := os.Lstat(archivePath)
	if err != nil {
		return zeroRelease{}, fmt.Errorf("open %s: %w", name, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > zeroArtifactMaxBytes {
		return zeroRelease{}, fmt.Errorf("%s has an invalid size or file type", name)
	}
	checksumPath := archivePath + ".sha256"
	checksumInfo, err := os.Lstat(checksumPath)
	if err != nil {
		return zeroRelease{}, fmt.Errorf("open %s.sha256: %w", name, err)
	}
	if !checksumInfo.Mode().IsRegular() || checksumInfo.Size() <= 0 || checksumInfo.Size() > 4096 {
		return zeroRelease{}, fmt.Errorf("%s.sha256 has an invalid size or file type", name)
	}
	checksum, err := os.ReadFile(checksumPath)
	if err != nil {
		return zeroRelease{}, fmt.Errorf("read %s.sha256: %w", name, err)
	}
	fields := strings.Fields(string(checksum))
	if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name || !sha256Pattern.MatchString(strings.ToLower(fields[0])) {
		return zeroRelease{}, fmt.Errorf("%s.sha256 is invalid", name)
	}
	return zeroRelease{
		Version:        official.Version,
		Tag:            official.Tag,
		ArtifactURL:    "managed://" + name,
		ArtifactSHA256: strings.ToLower(fields[0]),
		ArtifactSize:   info.Size(),
		LocalPath:      archivePath,
	}, nil
}

func downloadZeroBinary(parent context.Context, release zeroRelease) ([]byte, string, error) {
	var archive []byte
	var err error
	if release.LocalPath != "" {
		artifact, openErr := os.Open(release.LocalPath)
		if openErr != nil {
			return nil, "", fmt.Errorf("open managed Zero artifact: %w", openErr)
		}
		archive, err = io.ReadAll(io.LimitReader(artifact, zeroArtifactMaxBytes+1))
		closeErr := artifact.Close()
		if err != nil {
			return nil, "", fmt.Errorf("read managed Zero artifact: %w", err)
		}
		if closeErr != nil {
			return nil, "", fmt.Errorf("close managed Zero artifact: %w", closeErr)
		}
	} else {
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		defer cancel()
		client := zeroHTTPClient()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, release.ArtifactURL, nil)
		if requestErr != nil {
			return nil, "", requestErr
		}
		request.Header.Set("User-Agent", "zboard-kernel-automation")
		response, requestErr := client.Do(request)
		if requestErr != nil {
			return nil, "", fmt.Errorf("download Zero artifact: %w", requestErr)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("download Zero artifact returned %s", response.Status)
		}
		archive, err = io.ReadAll(io.LimitReader(response.Body, zeroArtifactMaxBytes+1))
		if err != nil {
			return nil, "", err
		}
	}
	if len(archive) > zeroArtifactMaxBytes {
		return nil, "", errors.New("Zero artifact exceeds the size limit")
	}
	if release.ArtifactSize <= 0 || int64(len(archive)) != release.ArtifactSize {
		return nil, "", errors.New("Zero artifact size does not match release metadata")
	}
	archiveDigest := sha256.Sum256(archive)
	if hex.EncodeToString(archiveDigest[:]) != release.ArtifactSHA256 {
		return nil, "", errors.New("Zero artifact SHA-256 does not match the signed release metadata")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, "", fmt.Errorf("open Zero artifact: %w", err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("read Zero artifact: %w", err)
		}
		if header.Name != "zero" || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size <= 0 || header.Size > zeroBinaryMaxBytes {
			return nil, "", errors.New("Zero binary in the release has an invalid size")
		}
		binary, err := io.ReadAll(io.LimitReader(tarReader, zeroBinaryMaxBytes+1))
		if err != nil {
			return nil, "", err
		}
		digest := sha256.Sum256(binary)
		return binary, hex.EncodeToString(digest[:]), nil
	}
	return nil, "", errors.New("Zero release archive does not contain the zero binary")
}

func zeroHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 6 {
				return errors.New("too many release download redirects")
			}
			return validateZeroReleaseURL(request.URL)
		},
	}
}

func validateZeroReleaseURL(value *url.URL) error {
	if value == nil || value.Scheme != "https" {
		return errors.New("Zero release URL must use HTTPS")
	}
	switch strings.ToLower(value.Hostname()) {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return nil
	default:
		return fmt.Errorf("Zero release host %q is not allowed", value.Hostname())
	}
}

func fetchSmallText(ctx context.Context, client *http.Client, rawURL string, limit int64) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if err := validateZeroReleaseURL(parsed); err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "zboard-kernel-automation")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request returned %s", response.Status)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(payload)) > limit {
		return "", errors.New("response exceeds size limit")
	}
	return string(payload), nil
}

func (h *handlers) compileNodeRuntimeConfig(node model.Node, apiKey string) ([]byte, string, error) {
	var installation model.Installation
	if err := h.db.First(&installation, 1).Error; err != nil {
		return nil, "", fmt.Errorf("load installation URL: %w", err)
	}
	panelURL := strings.TrimRight(strings.TrimSpace(installation.SiteURL), "/")
	parsedURL, err := url.Parse(panelURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, "", errors.New("site_url must be an absolute HTTP(S) URL reachable by the VPS before installing Zero")
	}
	if apiKey == "" {
		return nil, "", errors.New("Zero connector credential is unavailable")
	}
	now := time.Now().UTC()
	var subscriptions []model.Subscription
	if err := h.db.Model(&model.Subscription{}).
		Select("DISTINCT subscriptions.*").
		Joins("JOIN node_group_endpoints ON node_group_endpoints.node_group_id = subscriptions.node_group_id").
		Joins("JOIN protocol_endpoints ON protocol_endpoints.id = node_group_endpoints.protocol_endpoint_id").
		Where("protocol_endpoints.node_id = ? AND subscriptions.status = ? AND subscriptions.end_at > ? AND subscriptions.flow_used < subscriptions.flow_total", node.ID, subStatusActive, now).
		Find(&subscriptions).Error; err != nil {
		return nil, "", err
	}
	if err := h.ensureCredentialsForSubscriptions(subscriptions); err != nil {
		return nil, "", fmt.Errorf("reconcile subscription credentials: %w", err)
	}
	var endpoints []model.ProtocolEndpoint
	if err := h.db.Where("node_id = ? AND is_active = ?", node.ID, true).Order("sort_order asc, id asc").Find(&endpoints).Error; err != nil {
		return nil, "", err
	}
	inbounds := make([]map[string]interface{}, 0, len(endpoints))
	for _, endpoint := range endpoints {
		rawConfig, err := h.credentialCipher.Decrypt(endpoint.ServerConfig)
		if err != nil {
			return nil, "", fmt.Errorf("decrypt protocol endpoint %d config: %w", endpoint.ID, err)
		}
		var protocol map[string]interface{}
		if err := json.Unmarshal([]byte(rawConfig), &protocol); err != nil {
			return nil, "", fmt.Errorf("protocol endpoint %d config is invalid JSON: %w", endpoint.ID, err)
		}
		if kind, _ := protocol["type"].(string); !strings.EqualFold(strings.TrimSpace(kind), endpoint.Protocol) {
			return nil, "", fmt.Errorf("protocol endpoint %d config type must be %s", endpoint.ID, endpoint.Protocol)
		}
		if endpoint.Port <= 0 || endpoint.Port > 65535 {
			return nil, "", fmt.Errorf("protocol endpoint %d listen port is invalid", endpoint.ID)
		}
		endpointInbounds, err := h.runtimeInboundsForEndpoint(endpoint, protocol, now)
		if err != nil {
			return nil, "", fmt.Errorf("compile protocol endpoint %d: %w", endpoint.ID, err)
		}
		inbounds = append(inbounds, endpointInbounds...)
	}
	eventSink := map[string]interface{}{
		"tag":         "zboard",
		"type":        "webhook",
		"url":         panelURL + "/api/zero/events",
		"events":      []string{"flow.updated", "flow.completed"},
		"source_id":   fmt.Sprintf("node-%d", node.ID),
		"api_key_env": "ZERO_PANEL_API_KEY",
	}
	if parsedURL.Scheme == "http" {
		eventSink["allow_insecure"] = true
	}
	config := map[string]interface{}{
		"inbounds": inbounds,
		"mode":     map[string]interface{}{"type": "rule"},
		"route":    map[string]interface{}{"rules": []interface{}{}, "final": map[string]interface{}{"type": "direct"}},
		"api": map[string]interface{}{
			"event_sinks": []interface{}{eventSink},
		},
		"push": map[string]interface{}{
			"url": panelURL, "node_id": strconv.FormatUint(uint64(node.ID), 10),
			"api_key_env": "ZERO_PANEL_API_KEY", "heartbeat_interval_seconds": 30,
			"pull_commands": true, "command_poll_interval_seconds": 10,
		},
	}
	payload, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, "", err
	}
	payload = append(payload, '\n')
	digest := sha256.Sum256(payload)
	return payload, hex.EncodeToString(digest[:]), nil
}

func (h *handlers) nodeConnectorCredential(node model.Node) (pendingNodeCredential, error) {
	if node.NodeCredential != "" && node.NodeCredentialRevokedAt == nil {
		raw, err := h.credentialCipher.Decrypt(node.NodeCredential)
		if err != nil {
			return pendingNodeCredential{}, fmt.Errorf("decrypt existing Zero connector credential: %w", err)
		}
		return pendingNodeCredential{Raw: raw, Prefix: node.NodeCredentialPrefix}, nil
	}
	raw, prefix, err := newNodeReportSecret()
	if err != nil {
		return pendingNodeCredential{}, err
	}
	encrypted, err := h.credentialCipher.Encrypt(raw)
	if err != nil {
		return pendingNodeCredential{}, err
	}
	return pendingNodeCredential{Raw: raw, Encrypted: encrypted, Prefix: prefix, IsNew: true}, nil
}

func (h *handlers) installNodeKernel(node model.Node, operationID uint, binary []byte, binarySHA string, runtimeConfig []byte, apiKey string) error {
	if !sha256Pattern.MatchString(binarySHA) || apiKey == "" {
		return errors.New("invalid staged Zero binary or connector credential")
	}
	stage := "/tmp/zboard-zero-" + uuid.NewString()
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return err
	}
	defer conn.Close()
	timeout := time.AfterFunc(4*time.Minute, func() { _ = conn.Close() })
	defer timeout.Stop()
	if output, err := h.runNodeSSHSession(conn, node, "install -d -m 0700 "+shellQuote(stage), false); err != nil {
		return fmt.Errorf("create Zero staging directory: %w: %s", err, output)
	}
	files := []struct {
		path string
		mode string
		data []byte
	}{
		{stage + "/zero", "0700", binary},
		{stage + "/runtime.json", "0600", runtimeConfig},
		{stage + "/zero.env", "0600", []byte("ZERO_PANEL_API_KEY=" + apiKey + "\n")},
		{stage + "/zero.service", "0644", []byte(zeroSystemdUnit)},
	}
	for _, file := range files {
		if err := uploadSSHFile(conn, file.path, file.mode, file.data); err != nil {
			return fmt.Errorf("stage %s: %w", file.path, err)
		}
	}
	script := buildZeroInstallScript(stage, binarySHA, operationID)
	output, err := h.runNodeSSHSession(conn, node, script, true)
	if err != nil {
		return fmt.Errorf("activate Zero (automatic rollback attempted): %w: %s", err, truncateKernelError(output))
	}
	return nil
}

func (h *handlers) waitForNodeConnectorHeartbeat(parent context.Context, nodeID uint, activatedAt time.Time) (time.Time, error) {
	ctx, cancel := context.WithTimeout(parent, zeroHeartbeatTimeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		var heartbeat struct {
			ConnectorLastSeenAt *time.Time
		}
		if err := h.db.Model(&model.Node{}).Select("connector_last_seen_at").Where("id = ?", nodeID).Take(&heartbeat).Error; err != nil {
			return time.Time{}, err
		}
		if heartbeat.ConnectorLastSeenAt != nil && !heartbeat.ConnectorLastSeenAt.Before(activatedAt) {
			return heartbeat.ConnectorLastSeenAt.UTC(), nil
		}
		select {
		case <-ctx.Done():
			if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return time.Time{}, fmt.Errorf("connector heartbeat verification canceled: %w", ctx.Err())
			}
			return time.Time{}, fmt.Errorf("no fresh connector heartbeat arrived within %s: %w", zeroHeartbeatTimeout, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (h *handlers) restoreGeneratedNodeCredential(node model.Node, credential pendingNodeCredential) error {
	if !credential.IsNew {
		return nil
	}
	return h.db.Model(&model.Node{}).Where("id = ?", node.ID).Updates(map[string]interface{}{
		"node_credential":            node.NodeCredential,
		"node_credential_prefix":     node.NodeCredentialPrefix,
		"node_credential_revoked_at": node.NodeCredentialRevokedAt,
		"connector_last_seen_at":     node.ConnectorLastSeenAt,
		"last_seen_at":               node.LastSeenAt,
		"is_online":                  node.IsOnline,
		"status":                     node.Status,
		"version":                    node.Version,
		"uptime_seconds":             node.UptimeSeconds,
		"active_flows":               node.ActiveFlows,
		"bytes_up":                   node.BytesUp,
		"bytes_down":                 node.BytesDown,
	}).Error
}

func (h *handlers) rollbackNodeKernel(node model.Node, operationID uint) error {
	conn, _, err := h.dialNodeSSH(node)
	if err != nil {
		return fmt.Errorf("connect for Zero rollback: %w", err)
	}
	defer conn.Close()
	timeout := time.AfterFunc(time.Minute, func() { _ = conn.Close() })
	defer timeout.Stop()
	output, err := h.runNodeSSHSession(conn, node, buildZeroRollbackScript(operationID), true)
	if err != nil {
		return fmt.Errorf("rollback Zero generation: %w: %s", err, truncateKernelError(output))
	}
	return nil
}

const zeroSystemdUnit = `[Unit]
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

func buildZeroInstallScript(stage, binarySHA string, operationID uint) string {
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
  if [ "$had_bin" = "1" ]; then cp -a "$backup/zero" /usr/local/bin/zero; else rm -f /usr/local/bin/zero; fi
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
healthy=0
for attempt in 1 2 3 4 5 6 7 8 9 10; do
  if systemctl is-active --quiet zero && /usr/local/bin/zero status --json --socket %s >/dev/null 2>&1; then healthy=1; break; fi
  sleep 1
done
test "$healthy" = "1"
trap - EXIT
rm -rf "$stage"
printf 'ZBOARD_KERNEL_ACTIVATED=1\n'
`, shellQuote(stage), shellQuote(generation), shellQuote(backup), shellQuote(binarySHA), shellQuote(zeroControlSocket))
}

func buildZeroRollbackScript(operationID uint) string {
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
if [ "$had_bin" = "1" ]; then cp -a "$backup/zero" /usr/local/bin/zero; else rm -f /usr/local/bin/zero; fi
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

func uploadSSHFile(conn *ssh.Client, path, mode string, payload []byte) error {
	if conn == nil || len(payload) == 0 {
		return errors.New("empty SSH upload")
	}
	session, err := conn.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	session.Stdin = bytes.NewReader(payload)
	command := "umask 077; cat > " + shellQuote(path) + " && chmod " + mode + " " + shellQuote(path)
	output, err := session.CombinedOutput(command)
	if err != nil {
		return fmt.Errorf("%w: %s", err, truncateKernelError(string(output)))
	}
	return nil
}

func (h *handlers) runNodeSSHSession(conn *ssh.Client, node model.Node, command string, privileged bool) (string, error) {
	session, err := conn.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	command, stdin, requestPTY, err := h.prepareSSHCommand(node, command, privileged)
	if err != nil {
		return "", err
	}
	if requestPTY {
		modes := ssh.TerminalModes{ssh.ECHO: 0, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
		if err := session.RequestPty("xterm", 24, 80, modes); err != nil {
			return "", fmt.Errorf("request privilege terminal: %w", err)
		}
	}
	if stdin != "" {
		session.Stdin = strings.NewReader(stdin)
	}
	output, err := session.CombinedOutput(command)
	return strings.TrimSpace(string(output)), err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (h *handlers) probeNodeKernel(node model.Node) (kernelProbe, error) {
	const command = `set -u
if [ -r /etc/os-release ]; then . /etc/os-release; printf 'ZBOARD_OS=%s %s\n' "${ID:-linux}" "${VERSION_ID:-unknown}"; else printf 'ZBOARD_OS=linux unknown\n'; fi
printf 'ZBOARD_ARCH=%s\n' "$(uname -m 2>/dev/null || printf unknown)"
printf 'ZBOARD_LIBC=%s\n' "$(getconf GNU_LIBC_VERSION 2>/dev/null || ldd --version 2>&1 | head -n 1 || printf unknown)"
if command -v systemctl >/dev/null 2>&1; then printf 'ZBOARD_SYSTEMD=1\n'; else printf 'ZBOARD_SYSTEMD=0\n'; fi
zero_path=""
if [ -x /usr/local/bin/zero ]; then zero_path=/usr/local/bin/zero; elif command -v zero >/dev/null 2>&1; then zero_path="$(command -v zero)"; fi
if [ -z "$zero_path" ]; then
  printf 'ZBOARD_INSTALLED=0\nZBOARD_SERVICE=not_found\nZBOARD_CONTROL=unavailable\n'
  exit 0
fi
printf 'ZBOARD_INSTALLED=1\n'
printf 'ZBOARD_VERSION=%s\n' "$("$zero_path" build_info 2>/dev/null | awk -F': ' '$1 == "build_id" {print $2; exit}')"
printf 'ZBOARD_BINARY_SHA=%s\n' "$(sha256sum "$zero_path" | awk '{print $1}')"
if [ -f /etc/zerodenet/current.json ]; then printf 'ZBOARD_CONFIG_SHA=%s\n' "$(sha256sum /etc/zerodenet/current.json | awk '{print $1}')"; else printf 'ZBOARD_CONFIG_SHA=\n'; fi
service_status="$(systemctl is-active zero 2>/dev/null || true)"
if [ -z "$service_status" ]; then service_status=unknown; fi
printf 'ZBOARD_SERVICE=%s\n' "$service_status"
if [ "$service_status" = "active" ] && "$zero_path" status --json --socket /run/zerodenet/control.sock >/dev/null 2>&1; then printf 'ZBOARD_CONTROL=healthy\n'; else printf 'ZBOARD_CONTROL=unavailable\n'; fi`
	output, _, err := h.execSSHCommandWithPrivilege(node, command, true)
	if err != nil {
		return kernelProbe{}, fmt.Errorf("probe Zero over SSH: %w: %s", err, truncateKernelError(output))
	}
	probe, err := parseKernelProbe(output)
	if err != nil {
		return kernelProbe{}, err
	}
	return probe, nil
}

func parseKernelProbe(output string) (kernelProbe, error) {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "ZBOARD_") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	if values["ZBOARD_OS"] == "" || values["ZBOARD_ARCH"] == "" || values["ZBOARD_LIBC"] == "" || values["ZBOARD_SYSTEMD"] == "" || values["ZBOARD_INSTALLED"] == "" {
		return kernelProbe{}, errors.New("Zero probe returned an incomplete response")
	}
	probe := kernelProbe{
		OperatingSystem: values["ZBOARD_OS"],
		Architecture:    values["ZBOARD_ARCH"], Systemd: values["ZBOARD_SYSTEMD"] == "1",
		Libc:      values["ZBOARD_LIBC"],
		Installed: values["ZBOARD_INSTALLED"] == "1", Version: strings.TrimPrefix(values["ZBOARD_VERSION"], "v"),
		BinarySHA256: strings.ToLower(values["ZBOARD_BINARY_SHA"]), ConfigSHA256: strings.ToLower(values["ZBOARD_CONFIG_SHA"]),
		ServiceStatus: values["ZBOARD_SERVICE"], ControlStatus: values["ZBOARD_CONTROL"],
	}
	if probe.Installed && (!sha256Pattern.MatchString(probe.BinarySHA256) || probe.Version == "") {
		return kernelProbe{}, errors.New("installed Zero did not return a valid version or binary SHA-256")
	}
	return probe, nil
}

func classifyKernelAction(probe kernelProbe, desiredVersion, desiredBinarySHA, desiredConfigSHA string) string {
	if !probe.Installed {
		return "install"
	}
	switch compareZeroVersions(probe.Version, desiredVersion) {
	case -1:
		return "upgrade"
	case 1:
		return "manual_review"
	}
	if probe.BinarySHA256 != desiredBinarySHA {
		return "repair"
	}
	if probe.ConfigSHA256 != desiredConfigSHA {
		return "configure"
	}
	if probe.ServiceStatus != "active" || probe.ControlStatus != "healthy" {
		return "repair"
	}
	return "none"
}

func compareZeroVersions(left, right string) int {
	parse := func(raw string) ([3]int, bool) {
		var result [3]int
		parts := strings.Split(strings.SplitN(strings.TrimPrefix(strings.TrimSpace(raw), "v"), "-", 2)[0], ".")
		if len(parts) != 3 {
			return result, false
		}
		for index, part := range parts {
			value, err := strconv.Atoi(part)
			if err != nil || value < 0 {
				return result, false
			}
			result[index] = value
		}
		return result, true
	}
	l, lok := parse(left)
	r, rok := parse(right)
	if !lok || !rok {
		return 0
	}
	for index := range l {
		if l[index] < r[index] {
			return -1
		}
		if l[index] > r[index] {
			return 1
		}
	}
	return 0
}

func (h *handlers) kernelStatus(probe kernelProbe) string {
	if !probe.Installed {
		if probe.Architecture != "x86_64" || !probe.Systemd || (!supportsOfficialZeroArtifact(probe) && !h.hasManagedZeroArtifact()) {
			return "unsupported"
		}
		return "not_installed"
	}
	if probe.ServiceStatus == "active" && probe.ControlStatus == "healthy" {
		return "healthy"
	}
	return "degraded"
}

func (h *handlers) kernelRecommendedAction(probe kernelProbe) string {
	if !probe.Installed {
		if h.kernelStatus(probe) == "unsupported" {
			return "manual_review"
		}
		return "install"
	}
	if probe.ServiceStatus != "active" || probe.ControlStatus != "healthy" {
		return "repair"
	}
	return "check_release"
}

func (h *handlers) hasManagedZeroArtifact() bool {
	if strings.TrimSpace(h.zeroArtifactDir) == "" {
		return false
	}
	entries, err := os.ReadDir(h.zeroArtifactDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type().IsRegular() && managedZeroArtifactPattern.MatchString(entry.Name()) {
			if checksum, err := os.Stat(filepath.Join(h.zeroArtifactDir, entry.Name()+".sha256")); err == nil && checksum.Mode().IsRegular() {
				return true
			}
		}
	}
	return false
}

func (h *handlers) ensureKernelState(nodeID uint) (model.NodeKernelState, error) {
	state := model.NodeKernelState{NodeID: nodeID, Status: "unknown", Phase: "idle", RecommendedAction: "detect"}
	err := h.db.Where("node_id = ?", nodeID).FirstOrCreate(&state).Error
	return state, err
}

func (h *handlers) beginKernelOperation(nodeID uint, claims authClaims, operationType string) (model.NodeOperation, error) {
	var operation model.NodeOperation
	err := h.db.Transaction(func(tx *gorm.DB) error {
		var state model.NodeKernelState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("node_id = ?", nodeID).First(&state).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			state = model.NodeKernelState{NodeID: nodeID, Status: "unknown", Phase: "idle", RecommendedAction: "detect"}
			if err := tx.Create(&state).Error; err != nil {
				return err
			}
		}
		if state.ActiveOperationID != nil {
			var active model.NodeOperation
			if err := tx.First(&active, *state.ActiveOperationID).Error; err == nil && active.Status == "running" {
				return errKernelOperationRunning
			}
		}
		now := time.Now().UTC()
		operation = model.NodeOperation{
			NodeID: nodeID, OperationType: operationType, Status: "running", Phase: "queued",
			RequestedBy: claims.UserID, StartedAt: &now,
		}
		if err := tx.Create(&operation).Error; err != nil {
			return err
		}
		if err := tx.Model(&state).Updates(map[string]interface{}{
			"phase": "queued", "active_operation_id": operation.ID, "last_error": "",
		}).Error; err != nil {
			return err
		}
		return createAuditLog(tx, claims, "node.kernel."+operationType, fmt.Sprintf("node:%d", nodeID), fmt.Sprintf("operation=%d", operation.ID))
	})
	return operation, err
}

func (h *handlers) setKernelOperationPhase(operation *model.NodeOperation, phase string) error {
	operation.Phase = phase
	return h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(operation).Update("phase", phase).Error; err != nil {
			return err
		}
		return tx.Model(&model.NodeKernelState{}).Where("node_id = ? AND active_operation_id = ?", operation.NodeID, operation.ID).Update("phase", phase).Error
	})
}

func (h *handlers) completeKernelDetection(operation model.NodeOperation, probe kernelProbe) (model.NodeKernelState, error) {
	now := time.Now().UTC()
	summary := fmt.Sprintf("os=%s arch=%s libc=%s installed=%t version=%s service=%s control=%s", probe.OperatingSystem, probe.Architecture, probe.Libc, probe.Installed, probe.Version, probe.ServiceStatus, probe.ControlStatus)
	updates := map[string]interface{}{
		"status": h.kernelStatus(probe), "phase": "idle", "recommended_action": h.kernelRecommendedAction(probe),
		"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
		"installed_version": probe.Version, "installed_sha256": probe.BinarySHA256,
		"applied_config_sha256": probe.ConfigSHA256, "service_status": probe.ServiceStatus,
		"control_status": probe.ControlStatus, "last_detected_at": now, "last_error": "", "active_operation_id": nil,
	}
	if h.kernelStatus(probe) == "healthy" {
		updates["last_healthy_at"] = now
	}
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.NodeKernelState{}).Where("node_id = ?", operation.NodeID).Updates(updates).Error; err != nil {
			return err
		}
		operation.Status, operation.Phase, operation.ResultSummary, operation.FinishedAt = "succeeded", "completed", summary, &now
		return tx.Save(&operation).Error
	})
	if err != nil {
		return model.NodeKernelState{}, err
	}
	return h.ensureKernelState(operation.NodeID)
}

func (h *handlers) finishKernelOperation(operation *model.NodeOperation, probe kernelProbe, release zeroRelease, binarySHA, configSHA, summary string) (model.NodeKernelState, error) {
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status": h.kernelStatus(probe), "phase": "idle", "recommended_action": "none",
		"platform_os": probe.OperatingSystem, "architecture": probe.Architecture, "libc": probe.Libc,
		"desired_version": release.Version, "installed_version": probe.Version,
		"desired_sha256": binarySHA, "installed_sha256": probe.BinarySHA256,
		"desired_config_sha256": configSHA, "applied_config_sha256": probe.ConfigSHA256,
		"service_status": probe.ServiceStatus, "control_status": probe.ControlStatus,
		"last_detected_at": now, "last_error": "", "active_operation_id": nil,
	}
	if h.kernelStatus(probe) == "healthy" {
		updates["last_healthy_at"] = now
	}
	err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.NodeKernelState{}).Where("node_id = ?", operation.NodeID).Updates(updates).Error; err != nil {
			return err
		}
		operation.Status, operation.Phase, operation.ResultSummary, operation.FinishedAt = "succeeded", "completed", summary, &now
		return tx.Save(operation).Error
	})
	if err != nil {
		return model.NodeKernelState{}, err
	}
	return h.ensureKernelState(operation.NodeID)
}

func (h *handlers) failKernelOperation(operationID, nodeID uint, phase string, operationErr error) error {
	now := time.Now().UTC()
	errorText := truncateKernelError(operationErr.Error())
	stateStatus, recommendedAction := "failed", "retry"
	if errors.Is(operationErr, errKernelPlatformUnsupported) {
		stateStatus, recommendedAction = "unsupported", "manual_review"
	}
	return h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.NodeOperation{}).Where("id = ?", operationID).Updates(map[string]interface{}{
			"status": "failed", "phase": phase, "error": errorText, "finished_at": now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.NodeKernelState{}).Where("node_id = ? AND active_operation_id = ?", nodeID, operationID).Updates(map[string]interface{}{
			"status": stateStatus, "phase": "idle", "recommended_action": recommendedAction, "last_error": errorText, "active_operation_id": nil,
		}).Error
	})
}

func (h *handlers) updateKernelState(nodeID uint, updates map[string]interface{}) error {
	return h.db.Model(&model.NodeKernelState{}).Where("node_id = ?", nodeID).Updates(updates).Error
}

func truncateKernelError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 2000 {
		return value[:2000] + "…"
	}
	return value
}

func supportsOfficialZeroArtifact(probe kernelProbe) bool {
	fields := strings.Fields(strings.ToLower(probe.Libc))
	if len(fields) < 2 || fields[0] != "glibc" {
		return false
	}
	parts := strings.Split(fields[1], ".")
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	return majorErr == nil && minorErr == nil && (major > 2 || (major == 2 && minor >= 34))
}
