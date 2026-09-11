package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func validateProxyPoolSubscriptionURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 2048 {
		return nil, errors.New("订阅地址不能超过 2048 字节")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("订阅地址必须是完整的 HTTP 或 HTTPS URL")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("订阅地址不能包含用户信息或片段")
	}
	if address, err := netip.ParseAddr(parsed.Hostname()); err == nil && !proxyPoolSubscriptionAddressAllowed(address) {
		return nil, errors.New("订阅地址不能使用内网、回环或链路本地地址")
	}
	return parsed, nil
}

func proxyPoolSubscriptionAddressAllowed(address netip.Addr) bool {
	return address.IsValid() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast() && !address.IsMulticast() && !address.IsUnspecified()
}

func fetchProxyPoolSubscription(ctx context.Context, rawURL, userAgent string) ([]byte, error) {
	parsed, err := validateProxyPoolSubscriptionURL(rawURL)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(addresses) == 0 {
				return nil, errors.New("订阅地址无法解析")
			}
			for _, resolved := range addresses {
				if !proxyPoolSubscriptionAddressAllowed(resolved) {
					return nil, errors.New("订阅地址解析到了内网或本地地址")
				}
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("订阅地址重定向次数过多")
			}
			_, err := validateProxyPoolSubscriptionURL(request.URL.String())
			return err
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, errors.New("订阅请求无法创建")
	}
	if strings.TrimSpace(userAgent) == "" {
		userAgent = proxyPoolSubscriptionDefaultAgent
	}
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept", "application/json, application/yaml, text/yaml, text/plain;q=0.9, */*;q=0.1")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("订阅请求失败，请检查地址、网络和 TLS")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("订阅服务返回 HTTP %d", response.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, proxyPoolSubscriptionMaxBytes+1))
	if err != nil {
		return nil, errors.New("订阅响应读取失败")
	}
	if len(content) > proxyPoolSubscriptionMaxBytes {
		return nil, fmt.Errorf("订阅响应不能超过 %d KiB", proxyPoolSubscriptionMaxBytes/1024)
	}
	return content, nil
}
