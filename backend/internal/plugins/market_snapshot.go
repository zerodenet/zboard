package plugins

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// publicMarketplaceSnapshot reads the same derived, bounded snapshot as the
// public web directory. The URL fixes the host and channel; ZBoard filters its
// host version and native platform locally before a package enters installation.
func (m *Manager) publicMarketplaceSnapshot(ctx context.Context) (Market, error) {
	hostVersion := strings.TrimPrefix(strings.TrimSpace(m.host), "v")
	market := Market{Kind: "registry", Configured: true, Entries: []MarketEntry{}}
	byID := map[string]int{}
	byProduct := map[string]string{}
	var snapshot string
	var generatedAt time.Time
	for _, channel := range []string{"stable", "rc", "dev"} {
		channelMarket, channelSnapshot, channelGeneratedAt, err := m.fetchMarketplaceChannel(ctx, channel, hostVersion, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return Market{}, err
		}
		if snapshot == "" {
			snapshot, generatedAt = channelSnapshot, channelGeneratedAt
		} else if snapshot != channelSnapshot || !generatedAt.Equal(channelGeneratedAt) {
			return Market{}, errors.New("marketplace snapshot changed between channels")
		}
		for _, entry := range channelMarket.Entries {
			if packageID, ok := byProduct[entry.ProductID]; ok && packageID != entry.ID {
				return Market{}, errors.New("marketplace package identity differs between channels")
			}
			byProduct[entry.ProductID] = entry.ID
			if index, ok := byID[entry.ID]; ok {
				left, right := market.Entries[index], entry
				left.releases, right.releases = nil, nil
				if !reflect.DeepEqual(left, right) {
					return Market{}, errors.New("marketplace product differs between channels")
				}
				market.Entries[index].releases = append(market.Entries[index].releases, entry.releases...)
				continue
			}
			byID[entry.ID] = len(market.Entries)
			market.Entries = append(market.Entries, entry)
		}
	}
	for index := range market.Entries {
		sort.Slice(market.Entries[index].releases, func(i, j int) bool {
			left, _ := semver.StrictNewVersion(market.Entries[index].releases[i].Version)
			right, _ := semver.StrictNewVersion(market.Entries[index].releases[j].Version)
			return left.GreaterThan(right)
		})
	}
	market.SourceURL = DefaultMarketplaceAPIURL
	market.GeneratedAt = generatedAt
	market.SnapshotVersion = snapshot
	return market, nil
}

func (m *Manager) fetchMarketplaceChannel(ctx context.Context, channel, hostVersion, os, arch string) (Market, string, time.Time, error) {
	raw, err := m.fetch(ctx, DefaultMarketplaceAPIURL+"/zboard/"+channel+".json", 2<<20)
	if err != nil {
		return Market{}, "", time.Time{}, err
	}
	market, snapshot, generatedAt, err := parseMarketplacePage(raw, channel, hostVersion, os, arch)
	if err != nil {
		return Market{}, "", time.Time{}, err
	}
	return market, snapshot, generatedAt, nil
}
