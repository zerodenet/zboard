package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	zeroadapter "github.com/zerodenet/zboard/backend/internal/adapters/zero"
	"gorm.io/gorm"

	"github.com/zerodenet/zboard/backend/internal/capabilities/network"
	"github.com/zerodenet/zboard/backend/internal/model"
)

const (
	zeroReleaseAPI       = "https://api.github.com/repos/zerodenet/zero/releases/latest"
	zeroReleasesAPI      = "https://api.github.com/repos/zerodenet/zero/releases?per_page=30"
	zeroReleaseByTagAPI  = "https://api.github.com/repos/zerodenet/zero/releases/tags/"
	zeroLinuxGNUAsset    = "zero-linux-x86_64.tar.gz"
	zeroLinuxMuslAsset   = "zero-linux-x86_64-musl.tar.gz"
	zeroArtifactMaxBytes = 128 << 20
	zeroControlSocket    = "/run/zerodenet/control.sock"
)

var (
	errKernelOperationRunning    = network.ErrKernelOperationRunning
	errKernelPlatformUnsupported = network.ErrKernelPlatformUnsupported
	stableZeroTagPattern         = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	publishedZeroTagPattern      = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	managedZeroArtifactPattern   = regexp.MustCompile(`^zero-v[0-9]+\.[0-9]+\.[0-9]+-linux-x86_64-musl\.tar\.gz$`)
	localZeroVersionPattern      = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)
	localZeroArtifactPattern     = regexp.MustCompile(`^zero-v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?-linux-x86_64-musl\.tar\.gz$`)
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
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

type zeroReleaseOption struct {
	Version       string    `json:"version"`
	Tag           string    `json:"tag"`
	PublishedAt   time.Time `json:"published_at"`
	Prerelease    bool      `json:"prerelease"`
	GNUAvailable  bool      `json:"gnu_available"`
	MuslAvailable bool      `json:"musl_available"`
}

type kernelReconcileRequest struct {
	Version        string `json:"version"`
	AllowDowngrade bool   `json:"allow_downgrade"`
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

type preparedHandlerKernelReconciliation struct {
	h    *handlers
	node model.Node
}

func (h *handlers) PrepareKernelReconciliation(ctx context.Context, nodeID uint) (network.PreparedKernelReconciliation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	node, err := h.loadNode(nodeID)
	if err != nil {
		return nil, err
	}
	if err := h.validateNodeSSH(node); err != nil {
		return nil, err
	}
	return preparedHandlerKernelReconciliation{h: h, node: node}, nil
}

func (p preparedHandlerKernelReconciliation) Probe(ctx context.Context) (network.KernelProbe, error) {
	probe, err := p.h.probeNodeKernelContext(ctx, p.node)
	if err != nil {
		return network.KernelProbe{}, err
	}
	return kernelProbeCapability(probe), nil
}

func (p preparedHandlerKernelReconciliation) ResolveRelease(ctx context.Context, probe network.KernelProbe, version string) (network.PreparedKernelRelease, error) {
	release, err := p.h.resolveZeroRelease(ctx, kernelProbeFromCapability(probe), version)
	if err != nil {
		return nil, err
	}
	return preparedHandlerKernelRelease{h: p.h, node: p.node, release: release}, nil
}

func kernelProbeCapability(probe kernelProbe) network.KernelProbe {
	return network.KernelProbe{
		OperatingSystem: probe.OperatingSystem,
		Architecture:    probe.Architecture,
		Libc:            probe.Libc,
		Systemd:         probe.Systemd,
		Installed:       probe.Installed,
		Version:         probe.Version,
		BinarySHA256:    probe.BinarySHA256,
		ConfigSHA256:    probe.ConfigSHA256,
		ServiceStatus:   probe.ServiceStatus,
		ControlStatus:   probe.ControlStatus,
	}
}

func kernelProbeFromCapability(probe network.KernelProbe) kernelProbe {
	return kernelProbe{
		OperatingSystem: probe.OperatingSystem,
		Architecture:    probe.Architecture,
		Libc:            probe.Libc,
		Systemd:         probe.Systemd,
		Installed:       probe.Installed,
		Version:         probe.Version,
		BinarySHA256:    probe.BinarySHA256,
		ConfigSHA256:    probe.ConfigSHA256,
		ServiceStatus:   probe.ServiceStatus,
		ControlStatus:   probe.ControlStatus,
	}
}

func kernelReleaseCapability(release zeroRelease) network.KernelRelease {
	return network.KernelRelease{
		Version:        release.Version,
		ArtifactURL:    release.ArtifactURL,
		ArtifactSHA256: release.ArtifactSHA256,
		ArtifactSize:   release.ArtifactSize,
	}
}

type pendingNodeCredential struct {
	Raw       string
	Encrypted string
	Prefix    string
	IsNew     bool
}

type preparedHandlerKernelRelease struct {
	h       *handlers
	node    model.Node
	release zeroRelease
}

func (p preparedHandlerKernelRelease) Descriptor() network.KernelRelease {
	return kernelReleaseCapability(p.release)
}

func (p preparedHandlerKernelRelease) PrepareTrafficCredential(ctx context.Context) (*network.KernelEncryptedCredential, error) {
	return p.h.prepareNodeTrafficReportCredential(ctx, p.node)
}

func (p preparedHandlerKernelRelease) PrepareActivation(ctx context.Context) (network.PreparedKernelActivation, error) {
	credential, err := p.h.nodeConnectorCredential(p.node)
	if err != nil {
		return nil, err
	}
	if err := p.h.services.NodeCredentialReconciliation(p.h.credentialCipher, true).ReconcileNode(ctx, p.node.ID); err != nil {
		return nil, fmt.Errorf("reconcile node subscription credentials: %w", err)
	}
	runtimeConfig, configSHA, err := p.h.compileNodeRuntimeConfigContext(ctx, p.node, credential.Raw, p.release.Version)
	if err != nil {
		return nil, err
	}
	return &preparedHandlerKernelActivation{
		h: p.h, node: p.node, release: p.release, runtimeConfig: runtimeConfig,
		configSHA: configSHA, credential: credential,
	}, nil
}

type preparedHandlerKernelActivation struct {
	h             *handlers
	node          model.Node
	release       zeroRelease
	runtimeConfig []byte
	configSHA     string
	credential    pendingNodeCredential
}

func (p *preparedHandlerKernelActivation) ConfigSHA256() string { return p.configSHA }

func (p *preparedHandlerKernelActivation) ConnectorCredential() (network.KernelEncryptedCredential, bool) {
	if !p.credential.IsNew {
		return network.KernelEncryptedCredential{}, false
	}
	return network.KernelEncryptedCredential{Ciphertext: p.credential.Encrypted, Prefix: p.credential.Prefix}, true
}

func (p *preparedHandlerKernelActivation) ConnectorSnapshot() network.KernelConnectorSnapshot {
	return kernelConnectorSnapshot(p.node)
}

func (p *preparedHandlerKernelActivation) Materialize(ctx context.Context) (network.PreparedKernelMaterialization, error) {
	binary, binarySHA, err := downloadZeroBinary(ctx, p.release)
	if err != nil {
		return nil, err
	}
	return &preparedHandlerKernelMaterialization{
		h: p.h, node: p.node, binary: binary, binarySHA: binarySHA,
		runtimeConfig: p.runtimeConfig, connectorKey: p.credential.Raw,
	}, nil
}

func (p *preparedHandlerKernelActivation) Verify(ctx context.Context, expectedBinarySHA string) (network.KernelProbe, error) {
	probe, err := p.h.verifyNodeKernelStable(ctx, p.node, expectedBinarySHA)
	if err != nil {
		return network.KernelProbe{}, err
	}
	return kernelProbeCapability(probe), nil
}

func (p *preparedHandlerKernelActivation) WaitConnector(ctx context.Context, activationStartedAt time.Time) (time.Time, error) {
	return p.h.services.ConnectorActivityObserver().Wait(ctx, p.node.ID, activationStartedAt)
}

func (p *preparedHandlerKernelActivation) InvalidateConnectorCredential() {
	p.h.invalidateZeroEventCredential(p.node.ID)
}

type preparedHandlerKernelMaterialization struct {
	h             *handlers
	node          model.Node
	binary        []byte
	binarySHA     string
	runtimeConfig []byte
	connectorKey  string
}

func (p *preparedHandlerKernelMaterialization) BinarySHA256() string { return p.binarySHA }

func (p *preparedHandlerKernelMaterialization) Install(ctx context.Context, operationID uint) error {
	return p.h.installNodeKernel(ctx, p.node, operationID, p.binary, p.binarySHA, p.runtimeConfig, p.connectorKey)
}

func (p *preparedHandlerKernelMaterialization) Rollback(ctx context.Context, operationID uint) error {
	return p.h.rollbackNodeKernel(ctx, p.node, operationID)
}

func (h *handlers) LatestKernelReleaseHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	var release zeroRelease
	var err error
	if h.zeroNativeAccess {
		release, err = resolveLocalNativeZeroRelease(h.zeroArtifactDir, h.zeroLocalVersion)
	} else {
		release, err = resolveLatestZeroRelease(r.Context())
	}
	if err != nil {
		ServerError(w, fmt.Errorf("resolve latest Zero release: %w", err))
		return
	}
	OK(w, release)
}

func (h *handlers) KernelReleasesHandler(w http.ResponseWriter, r *http.Request) {
	if _, err := h.requireAdmin(w, r); err != nil {
		return
	}
	if h.zeroNativeAccess {
		release, err := resolveLocalNativeZeroRelease(h.zeroArtifactDir, h.zeroLocalVersion)
		if err != nil {
			ServerError(w, fmt.Errorf("resolve configured Zero release: %w", err))
			return
		}
		OK(w, []zeroReleaseOption{{
			Version: release.Version, Tag: release.Tag, GNUAvailable: false, MuslAvailable: true,
		}})
		return
	}
	releases, err := listInstallableZeroReleases(r.Context())
	if err != nil {
		ServerError(w, fmt.Errorf("list installable Zero releases: %w", err))
		return
	}
	for index := range releases {
		if releases[index].MuslAvailable {
			continue
		}
		if _, err := resolveManagedZeroRelease(h.zeroArtifactDir, zeroRelease{
			Version: releases[index].Version,
			Tag:     releases[index].Tag,
		}); err == nil {
			releases[index].MuslAvailable = true
		}
	}
	OK(w, releases)
}

func (h *handlers) NodeKernelStateHandler(w http.ResponseWriter, r *http.Request) {
	claims, err := h.requireAdmin(w, r)
	if err != nil {
		return
	}
	nodeID, err := parsePathID(r.URL.Path, "/api/v1/nodes/")
	if err != nil {
		BadRequest(w, err.Error())
		return
	}
	result, err := h.services.KernelHistory().Get(r.Context(), network.KernelDetectionRequest{NodeID: nodeID, ActorID: claims.UserID})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			NotFound(w)
			return
		}
		if errors.Is(err, network.ErrKernelPermission) {
			Forbidden(w, err.Error())
			return
		}
		ServerError(w, err)
		return
	}
	OK(w, result)
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
	result, err := h.services.KernelDetection(h, h.hasConfiguredNativeZeroArtifact()).Detect(r.Context(), network.KernelDetectionRequest{NodeID: nodeID, ActorID: claims.UserID})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			NotFound(w)
		case errors.Is(err, network.ErrKernelPermission):
			Forbidden(w, err.Error())
		case errors.Is(err, network.ErrKernelDetectionCommit):
			ServerError(w, err)
		default:
			BadRequest(w, err.Error())
		}
		return
	}
	OK(w, result)
}

func decodeKernelReconcileRequest(r *http.Request) (kernelReconcileRequest, error) {
	var request kernelReconcileRequest
	if r.Body != nil && r.ContentLength != 0 {
		decoder := json.NewDecoder(io.LimitReader(r.Body, 4096))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return kernelReconcileRequest{}, validationError("invalid kernel reconcile request", map[string]string{"form": "请求内容不是有效 JSON。"})
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return kernelReconcileRequest{}, validationError("invalid kernel reconcile request", map[string]string{"form": "请求只能包含一个 JSON 对象。"})
		}
	}
	request.Version = strings.TrimSpace(strings.TrimPrefix(request.Version, "v"))
	if request.Version != "" && !localZeroVersionPattern.MatchString(request.Version) {
		return kernelReconcileRequest{}, validationError("invalid kernel reconcile request", map[string]string{"version": "请选择有效的 Zero 版本。"})
	}
	if request.AllowDowngrade && request.Version == "" {
		return kernelReconcileRequest{}, validationError("invalid kernel reconcile request", map[string]string{"allow_downgrade": "允许降级时必须明确指定目标版本。"})
	}
	return request, nil
}

func resolveLatestZeroRelease(parent context.Context) (zeroRelease, error) {
	payload, ctx, client, cancel, err := fetchZeroRelease(parent, "")
	if cancel != nil {
		defer cancel()
	}
	if err != nil {
		return zeroRelease{}, err
	}
	return resolveGitHubZeroReleaseAsset(ctx, client, payload, zeroLinuxGNUAsset)
}

func fetchZeroRelease(parent context.Context, version string) (githubRelease, context.Context, *http.Client, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	client := zeroHTTPClient()
	endpoint := zeroReleaseAPI
	if version != "" {
		if !localZeroVersionPattern.MatchString(version) {
			cancel()
			return githubRelease{}, nil, nil, nil, errors.New("selected Zero version is not a supported semantic version")
		}
		endpoint = zeroReleaseByTagAPI + url.PathEscape("v"+version)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		cancel()
		return githubRelease{}, nil, nil, nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "zboard-kernel-automation")
	response, err := client.Do(request)
	if err != nil {
		cancel()
		return githubRelease{}, nil, nil, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		cancel()
		return githubRelease{}, nil, nil, nil, fmt.Errorf("GitHub release API returned %s", response.Status)
	}
	var payload githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&payload); err != nil {
		cancel()
		return githubRelease{}, nil, nil, nil, err
	}
	if payload.Draft || !publishedZeroTagPattern.MatchString(payload.TagName) {
		cancel()
		return githubRelease{}, nil, nil, nil, errors.New("selected Zero release is not a published semantic version")
	}
	if version == "" && (payload.Prerelease || !stableZeroTagPattern.MatchString(payload.TagName)) {
		cancel()
		return githubRelease{}, nil, nil, nil, errors.New("latest Zero release is not a stable version")
	}
	if version != "" && payload.TagName != "v"+version {
		cancel()
		return githubRelease{}, nil, nil, nil, errors.New("GitHub returned a different Zero release than requested")
	}
	return payload, ctx, client, cancel, nil
}

func resolveGitHubZeroReleaseAsset(ctx context.Context, client *http.Client, payload githubRelease, assetName string) (zeroRelease, error) {
	var archiveURL, checksumURL string
	var archiveSize int64
	for _, asset := range payload.Assets {
		switch asset.Name {
		case assetName:
			archiveURL, archiveSize = asset.BrowserDownloadURL, asset.Size
		case assetName + ".sha256":
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if archiveURL == "" || checksumURL == "" || archiveSize <= 0 || archiveSize > zeroArtifactMaxBytes {
		return zeroRelease{}, fmt.Errorf("release is missing %s or its checksum", assetName)
	}
	checksum, err := fetchSmallText(ctx, client, checksumURL, 4096)
	if err != nil {
		return zeroRelease{}, fmt.Errorf("download release checksum: %w", err)
	}
	fields := strings.Fields(checksum)
	if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != assetName || !sha256Pattern.MatchString(strings.ToLower(fields[0])) {
		return zeroRelease{}, fmt.Errorf("%s checksum file is invalid", assetName)
	}
	return zeroRelease{
		Version: strings.TrimPrefix(payload.TagName, "v"), Tag: payload.TagName,
		ArtifactURL: archiveURL, ArtifactSHA256: strings.ToLower(fields[0]), ArtifactSize: archiveSize,
	}, nil
}

func githubZeroMuslAssetNames(payload githubRelease) []string {
	names := []string{zeroLinuxMuslAsset}
	if publishedZeroTagPattern.MatchString(payload.TagName) {
		names = append(names, fmt.Sprintf("zero-%s-linux-x86_64-musl.tar.gz", payload.TagName))
	}
	return names
}

func resolveGitHubZeroMuslRelease(ctx context.Context, client *http.Client, payload githubRelease) (zeroRelease, error) {
	var lastErr error
	for _, name := range githubZeroMuslAssetNames(payload) {
		release, err := resolveGitHubZeroReleaseAsset(ctx, client, payload, name)
		if err == nil {
			return release, nil
		}
		lastErr = err
	}
	return zeroRelease{}, lastErr
}

func listInstallableZeroReleases(parent context.Context) ([]zeroReleaseOption, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, zeroReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "zboard-kernel-automation")
	response, err := zeroHTTPClient().Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub releases API returned %s", response.Status)
	}
	var payload []githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	releases := installableZeroReleaseOptions(payload)
	if len(releases) == 0 {
		return nil, errors.New("no installable published Zero releases were found")
	}
	return releases, nil
}

func installableZeroReleaseOptions(payload []githubRelease) []zeroReleaseOption {
	releases := make([]zeroReleaseOption, 0, len(payload))
	for _, release := range payload {
		if release.Draft || !publishedZeroTagPattern.MatchString(release.TagName) {
			continue
		}
		option := zeroReleaseOption{
			Version: strings.TrimPrefix(release.TagName, "v"),
			Tag:     release.TagName, PublishedAt: release.PublishedAt, Prerelease: release.Prerelease,
		}
		legacyMuslAsset := fmt.Sprintf("zero-%s-linux-x86_64-musl.tar.gz", release.TagName)
		var gnuArchive, gnuChecksum bool
		muslArchives := map[string]bool{}
		muslChecksums := map[string]bool{}
		for _, asset := range release.Assets {
			switch asset.Name {
			case zeroLinuxGNUAsset:
				gnuArchive = asset.Size > 0 && asset.Size <= zeroArtifactMaxBytes
			case zeroLinuxGNUAsset + ".sha256":
				gnuChecksum = asset.Size > 0 && asset.Size <= 4096
			case zeroLinuxMuslAsset, legacyMuslAsset:
				muslArchives[asset.Name] = asset.Size > 0 && asset.Size <= zeroArtifactMaxBytes
			case zeroLinuxMuslAsset + ".sha256", legacyMuslAsset + ".sha256":
				muslChecksums[strings.TrimSuffix(asset.Name, ".sha256")] = asset.Size > 0 && asset.Size <= 4096
			}
		}
		option.GNUAvailable = gnuArchive && gnuChecksum
		option.MuslAvailable = (muslArchives[zeroLinuxMuslAsset] && muslChecksums[zeroLinuxMuslAsset]) ||
			(muslArchives[legacyMuslAsset] && muslChecksums[legacyMuslAsset])
		if option.GNUAvailable || option.MuslAvailable {
			releases = append(releases, option)
		}
	}
	return releases
}

func (h *handlers) resolveZeroRelease(ctx context.Context, probe kernelProbe, version string) (zeroRelease, error) {
	if h.zeroNativeAccess {
		release, err := resolveLocalNativeZeroRelease(h.zeroArtifactDir, h.zeroLocalVersion)
		if err != nil {
			return zeroRelease{}, err
		}
		if version != "" && version != release.Version {
			return zeroRelease{}, fmt.Errorf("native-local exposes only configured Zero %s", release.Version)
		}
		return release, nil
	}
	payload, releaseCtx, client, cancel, err := fetchZeroRelease(ctx, version)
	if cancel != nil {
		defer cancel()
	}
	if err != nil {
		return zeroRelease{}, err
	}
	if supportsOfficialZeroArtifact(probe) {
		return resolveGitHubZeroReleaseAsset(releaseCtx, client, payload, zeroLinuxGNUAsset)
	}
	musl, muslErr := resolveGitHubZeroMuslRelease(releaseCtx, client, payload)
	if muslErr == nil {
		return musl, nil
	}
	official := zeroRelease{Version: strings.TrimPrefix(payload.TagName, "v"), Tag: payload.TagName}
	managed, err := resolveManagedZeroRelease(h.zeroArtifactDir, official)
	if err != nil {
		return zeroRelease{}, fmt.Errorf(
			"%w: the GNU artifact requires glibc >= 2.34, the node reports %s, the release musl artifact is unavailable (%v), and the legacy managed fallback is unavailable: %v",
			errKernelPlatformUnsupported,
			probe.Libc,
			muslErr,
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
	return resolveManagedZeroArtifact(artifactDir, name, official.Version, official.Tag)
}

func resolveLocalNativeZeroRelease(artifactDir, version string) (zeroRelease, error) {
	version = strings.TrimSpace(version)
	if !localZeroVersionPattern.MatchString(version) {
		return zeroRelease{}, errors.New("ZBOARD_ZERO_LOCAL_VERSION is not a supported semantic version")
	}
	name := fmt.Sprintf("zero-v%s-linux-x86_64-musl.tar.gz", version)
	if !localZeroArtifactPattern.MatchString(name) {
		return zeroRelease{}, errors.New("the local native artifact name is invalid")
	}
	return resolveManagedZeroArtifact(artifactDir, name, version, "v"+version)
}

func resolveManagedZeroArtifact(artifactDir, name, version, tag string) (zeroRelease, error) {
	if strings.TrimSpace(artifactDir) == "" {
		return zeroRelease{}, errors.New("ZBOARD_ZERO_ARTIFACT_DIR is not configured")
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
		Version:        version,
		Tag:            tag,
		ArtifactURL:    "managed://" + name,
		ArtifactSHA256: strings.ToLower(fields[0]),
		ArtifactSize:   info.Size(),
		LocalPath:      archivePath,
	}, nil
}

func downloadZeroBinary(parent context.Context, release zeroRelease) ([]byte, string, error) {
	return (zeroadapter.ArtifactLoader{Client: zeroHTTPClient()}).LoadBinary(parent, zeroadapter.ArtifactRelease{
		URL: release.ArtifactURL, SHA256: release.ArtifactSHA256, Size: release.ArtifactSize, LocalPath: release.LocalPath,
	})
}

var managedZeroSubscriptionValidator = validateSubscriptionWithManagedZero

func (h *handlers) validateZeroSubscriptionPreview(ctx context.Context, renderer, rendered string) error {
	if renderer != subscriptionRendererZnetSink || !h.zeroMieruAccess {
		return nil
	}
	return managedZeroSubscriptionValidator(ctx, h.zeroArtifactDir, h.zeroLocalVersion, []byte(rendered))
}

func validateSubscriptionWithManagedZero(ctx context.Context, artifactDir, version string, config []byte) error {
	release, err := resolveLocalNativeZeroRelease(artifactDir, version)
	if err != nil {
		return fmt.Errorf("resolve Zero preview validator: %w", err)
	}
	binary, _, err := downloadZeroBinary(ctx, release)
	if err != nil {
		return fmt.Errorf("load Zero preview validator: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "zboard-zero-preview-*")
	if err != nil {
		return fmt.Errorf("create Zero preview validator directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	binaryPath := filepath.Join(tempDir, "zero")
	configPath := filepath.Join(tempDir, "subscription.json")
	if err := os.WriteFile(binaryPath, binary, 0o700); err != nil {
		return fmt.Errorf("write Zero preview validator: %w", err)
	}
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		return fmt.Errorf("write Zero subscription preview: %w", err)
	}
	validateCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if zeroadapter.RequiresDirectInboundUDP(config) {
		info, infoErr := exec.CommandContext(validateCtx, binaryPath, "build-info").CombinedOutput()
		if infoErr != nil || !zeroadapter.SupportsDirectInboundUDP(string(info)) {
			return fmt.Errorf("Zero validator does not declare direct inbound UDP support; upgrade the validator or disable inbound UDP")
		}
	}
	output, err := exec.CommandContext(validateCtx, binaryPath, "validate", configPath).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if len(message) > 2048 {
			message = message[:2048]
		}
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("Zero rejected subscription preview: %s", message)
	}
	return nil
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
	return zeroadapter.ValidateReleaseURL(value)
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

func (h *handlers) compileNodeRuntimeConfig(node model.Node, apiKey, zeroVersion string) ([]byte, string, error) {
	return h.compileNodeRuntimeConfigContext(context.Background(), node, apiKey, zeroVersion)
}

func (h *handlers) compileNodeRuntimeConfigWithOptions(node model.Node, apiKey, zeroVersion string, suppressMieruFallback bool) ([]byte, string, error) {
	return h.compileNodeRuntimeConfigWithOptionsContext(context.Background(), node, apiKey, zeroVersion, suppressMieruFallback)
}

func (h *handlers) compileNodeRuntimeConfigContext(ctx context.Context, node model.Node, apiKey, zeroVersion string) ([]byte, string, error) {
	return h.compileNodeRuntimeConfigWithOptionsContext(ctx, node, apiKey, zeroVersion, false)
}

func (h *handlers) compileNodeRuntimeConfigWithOptionsContext(ctx context.Context, node model.Node, apiKey, zeroVersion string, suppressMieruFallback bool) ([]byte, string, error) {
	now := time.Now().UTC()
	snapshot, err := h.services.RuntimeConfigurationSource().Load(ctx, node.ID, now, h.runtimeCredentialProtocols())
	if err != nil {
		return nil, "", fmt.Errorf("load node runtime configuration source: %w", err)
	}
	return (zeroadapter.RuntimeConfigurationRenderer{Cipher: h.credentialCipher}).Render(zeroadapter.RuntimeConfigurationRenderRequest{
		NodeID: node.ID, APIKey: apiKey, ZeroVersion: zeroVersion, NativeConnector: h.zeroNativeAccess,
		SuppressMieruFallback: suppressMieruFallback, Now: now, Snapshot: snapshot,
	})
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

func (h *handlers) installNodeKernel(ctx context.Context, node model.Node, operationID uint, binary []byte, binarySHA string, runtimeConfig []byte, apiKey string) error {
	installer := zeroadapter.KernelInstaller{Dialer: zeroKernelRemoteDialer{h: h, node: node}}
	return installer.Install(ctx, zeroadapter.KernelInstallRequest{
		OperationID: operationID, Binary: binary, BinarySHA256: binarySHA, RuntimeConfig: runtimeConfig, ConnectorKey: apiKey,
	})
}

func kernelConnectorSnapshot(node model.Node) network.KernelConnectorSnapshot {
	return network.KernelConnectorSnapshot{
		Credential:          network.KernelEncryptedCredential{Ciphertext: node.NodeCredential, Prefix: node.NodeCredentialPrefix},
		RevokedAt:           node.NodeCredentialRevokedAt,
		ConnectorLastSeenAt: node.ConnectorLastSeenAt,
		LastSeenAt:          node.LastSeenAt,
		IsOnline:            node.IsOnline,
		Status:              node.Status,
		Version:             node.Version,
		UptimeSeconds:       node.UptimeSeconds,
		ActiveFlows:         node.ActiveFlows,
		BytesUp:             node.BytesUp,
		BytesDown:           node.BytesDown,
	}
}

func (h *handlers) rollbackNodeKernel(ctx context.Context, node model.Node, operationID uint) error {
	return (zeroadapter.KernelInstaller{Dialer: zeroKernelRemoteDialer{h: h, node: node}}).Rollback(ctx, operationID)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (h *handlers) probeNodeKernel(node model.Node) (kernelProbe, error) {
	return h.probeNodeKernelContext(context.Background(), node)
}

func (h *handlers) probeNodeKernelContext(ctx context.Context, node model.Node) (kernelProbe, error) {
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
	output, _, err := h.execSSHCommandWithPrivilegeContext(ctx, node, command, true)
	if err != nil {
		return kernelProbe{}, fmt.Errorf("probe Zero over SSH: %w: %s", err, truncateKernelError(output))
	}
	probe, err := parseKernelProbe(output)
	if err != nil {
		return kernelProbe{}, err
	}
	return probe, nil
}

func (h *handlers) ValidateKernelProbeTarget(ctx context.Context, nodeID uint) error {
	node, err := h.loadNodeContext(ctx, nodeID)
	if err != nil {
		return err
	}
	node.SSHPrivilegeConfigured = node.SSHPrivilegePassword != ""
	return h.validateNodeSSH(node)
}

func (h *handlers) ProbeKernel(ctx context.Context, nodeID uint) (network.KernelProbe, error) {
	node, err := h.loadNodeContext(ctx, nodeID)
	if err != nil {
		return network.KernelProbe{}, err
	}
	node.SSHPrivilegeConfigured = node.SSHPrivilegePassword != ""
	probe, err := h.probeNodeKernelContext(ctx, node)
	if err != nil {
		return network.KernelProbe{}, err
	}
	return kernelProbeCapability(probe), nil
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
	return network.ClassifyKernelAction(kernelProbeCapability(probe), desiredVersion, desiredBinarySHA, desiredConfigSHA)
}

func compareZeroVersions(left, right string) int {
	return network.CompareKernelVersions(left, right)
}

func (h *handlers) kernelStatus(probe kernelProbe) string {
	if !probe.Installed {
		if probe.Architecture != "x86_64" || !probe.Systemd || !h.hasConfiguredNativeZeroArtifact() {
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

func (h *handlers) hasConfiguredNativeZeroArtifact() bool {
	if !h.zeroNativeAccess {
		return true
	}
	_, err := resolveLocalNativeZeroRelease(h.zeroArtifactDir, h.zeroLocalVersion)
	return err == nil
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
