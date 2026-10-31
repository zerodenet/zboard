package platform

import (
	"errors"
	"net/url"
	"path"
	"strings"
)

const SubscriptionPrefixKey = "subscription_url_prefix"
const LegacySubscriptionPrefix = "/api/v1/client/subscription/"

// SubscriptionPrefix accepts an administrator-defined path within the site.
// There is deliberately no fallback to the legacy public route.
func SubscriptionPrefix(value string) (*url.URL, error) {
	if value == "" {
		return nil, nil
	}
	u, err := url.Parse(value)
	if err != nil || len(value) > 2048 || strings.ContainsAny(value, "\\\r\n\t %?#") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("订阅前缀必须是站内绝对路径，不能包含查询参数、转义字符或凭据。")
	}
	if u.IsAbs() || u.Host != "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return nil, errors.New("站内订阅前缀必须以单个 / 开头，不能包含域名。")
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	if u.Path == "" || path.Clean(u.Path) != u.Path {
		return nil, errors.New("订阅前缀不能为空或包含重复斜线和相对路径。")
	}
	first := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")[0]
	switch strings.ToLower(first) {
	case "api", "admin", "account", "auth", "setup", "assets", "media", "healthz", "readyz", "login", "register", "favicon.ico", "robots.txt", "extensions", "pricing", "terms", "privacy", "refund", "docs":
		return nil, errors.New("订阅前缀不能占用系统路由，请使用独立的自定义路径。")
	}
	u.Path += "/"
	return u, nil
}

func SubscriptionDeliveryURL(prefix, token string) (string, error) {
	u, err := SubscriptionPrefix(prefix)
	if err != nil || u == nil || token == "" {
		return "", err
	}
	u.Path += token
	return u.String(), nil
}
