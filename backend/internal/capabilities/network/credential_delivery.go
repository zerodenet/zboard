package network

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// CredentialDeliveryTarget identifies one opaque credential binding included in
// an immutable compiled target. It contains neither plaintext nor ciphertext.
// It records configuration delivery, never business authorization or runtime
// observation. PolicyRevision describes this target, not an expiry heartbeat.
type CredentialDeliveryTarget struct {
	EndpointSHA256     string `json:"endpoint_sha256"`
	CredentialID       uint   `json:"credential_id"`
	ProtocolEndpointID uint   `json:"protocol_endpoint_id"`
	MaterialSHA256     string `json:"material_sha256"`
	PolicyRevision     uint64 `json:"policy_revision"`
}

type NetworkEntryDeliveryTarget struct {
	NetworkEntryID uint   `json:"network_entry_id"`
	MaterialSHA256 string `json:"material_sha256"`
}
type ConfigurationDeliveryTargets struct {
	Credentials    []CredentialDeliveryTarget
	NetworkEntries []NetworkEntryDeliveryTarget
}

type CredentialDeliveryManifest struct {
	NetworkEntries *[]NetworkEntryDeliveryTarget `json:"network_entries,omitempty"`

	Version int                        `json:"version"`
	Targets []CredentialDeliveryTarget `json:"targets"`
}

var ErrCredentialDeliveryManifest = errors.New("credential delivery manifest is invalid")

// CredentialMaterialFingerprint binds the canonical opaque identity, encrypted
// material and resource location. Ciphertext is hashed in memory; it is never
// duplicated into deployment manifests or SQL lookup parameters.
func CredentialMaterialFingerprint(nodeID, endpointID, credentialID uint, principal, ciphertext string, listenPort, publicPort int) string {
	if nodeID == 0 || endpointID == 0 || credentialID == 0 || principal == "" || ciphertext == "" {
		return ""
	}
	h := sha256.New()
	h.Write([]byte("zboard-credential-delivery-v1\x00"))
	var value [8]byte
	for _, id := range []uint{nodeID, endpointID, credentialID, uint(listenPort), uint(publicPort)} {
		binary.BigEndian.PutUint64(value[:], uint64(id))
		h.Write(value[:])
	}
	for _, text := range []string{principal, ciphertext} {
		binary.BigEndian.PutUint64(value[:], uint64(len(text)))
		h.Write(value[:])
		h.Write([]byte(text))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func EncodeCredentialDeliveryManifest(targets []CredentialDeliveryTarget, entries ...[]NetworkEntryDeliveryTarget) (string, error) {
	ordered := append([]CredentialDeliveryTarget{}, targets...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CredentialID < ordered[j].CredentialID })
	for i, target := range ordered {
		digest, err := hex.DecodeString(target.MaterialSHA256)
		endpointDigest, endpointErr := hex.DecodeString(target.EndpointSHA256)
		if endpointErr != nil || len(endpointDigest) != sha256.Size || target.EndpointSHA256 != hex.EncodeToString(endpointDigest) || target.CredentialID == 0 || target.ProtocolEndpointID == 0 || target.PolicyRevision == 0 || err != nil || len(digest) != sha256.Size || target.MaterialSHA256 != hex.EncodeToString(digest) || (i > 0 && ordered[i-1].CredentialID == target.CredentialID) {
			return "", ErrCredentialDeliveryManifest
		}
	}
	manifest := CredentialDeliveryManifest{Version: 1, Targets: ordered}
	if len(entries) > 1 {
		return "", ErrCredentialDeliveryManifest
	}
	if len(entries) == 1 {
		routes := append([]NetworkEntryDeliveryTarget{}, entries[0]...)
		sort.Slice(routes, func(i, j int) bool { return routes[i].NetworkEntryID < routes[j].NetworkEntryID })
		for i, route := range routes {
			digest, err := hex.DecodeString(route.MaterialSHA256)
			if route.NetworkEntryID == 0 || err != nil || len(digest) != sha256.Size || route.MaterialSHA256 != hex.EncodeToString(digest) || (i > 0 && routes[i-1].NetworkEntryID == route.NetworkEntryID) {
				return "", ErrCredentialDeliveryManifest
			}
		}
		manifest.NetworkEntries = &routes
	}
	body, err := json.Marshal(manifest)
	return string(body), err
}

func DecodeCredentialDeliveryManifest(body string) ([]CredentialDeliveryTarget, error) {
	manifest, err := DecodeConfigurationDeliveryManifest(body)
	return manifest.Targets, err
}
func DecodeConfigurationDeliveryManifest(body string) (CredentialDeliveryManifest, error) {
	var manifest CredentialDeliveryManifest
	if err := json.Unmarshal([]byte(body), &manifest); err != nil || manifest.Version != 1 || manifest.Targets == nil {
		return manifest, ErrCredentialDeliveryManifest
	}
	var entries [][]NetworkEntryDeliveryTarget
	if manifest.NetworkEntries != nil {
		entries = append(entries, *manifest.NetworkEntries)
	}
	canonical, err := EncodeCredentialDeliveryManifest(manifest.Targets, entries...)
	if err != nil || canonical != body {
		return manifest, ErrCredentialDeliveryManifest
	}
	return manifest, nil
}

// CredentialEndpointFingerprint includes the saved server and client binding
// inputs as opaque values. It does not parse a kernel-private configuration.
func CredentialEndpointFingerprint(endpoint RuntimeConfigurationEndpoint) string {
	body, _ := json.Marshal(struct {
		NodeID, ID                               uint
		Protocol, Address, ListenAddress         string
		Port, PublicPort                         int
		ServerConfig, EgressConfig, ClientConfig string
	}{endpoint.NodeID, endpoint.ID, endpoint.Protocol, endpoint.Address, endpoint.ListenAddress, endpoint.Port, endpoint.PublicPort, endpoint.ServerConfig, endpoint.EgressConfig, endpoint.ClientConfig})
	digest := sha256.Sum256(append([]byte("zboard-credential-endpoint-v1\x00"), body...))
	return hex.EncodeToString(digest[:])
}

func NetworkEntryDeliveryFingerprint(entry RuntimeConfigurationNetworkEntry) string {
	poolConfig := ""
	if entry.ProxyPool != nil {
		poolConfig = entry.ProxyPool.Config
	}
	body, _ := json.Marshal(struct {
		ID, NodeID, EndpointID          uint
		Network, Address, ListenAddress string
		Port, PublicPort                int
		PathConfig                      string
		ProxyPoolID                     *uint
		ProxyPoolConfig                 string
		LandingProtocol, LandingAddress string
		LandingPort, LandingPublicPort  int
	}{entry.ID, entry.NodeID, entry.EndpointID, entry.Network, entry.Address, entry.ListenAddress, entry.Port, entry.PublicPort, entry.PathConfig, entry.ProxyPoolID, poolConfig, entry.Landing.Protocol, entry.Landing.Address, entry.Landing.Port, entry.Landing.PublicPort})
	digest := sha256.Sum256(append([]byte("zboard-network-entry-delivery-v1\x00"), body...))
	return hex.EncodeToString(digest[:])
}
