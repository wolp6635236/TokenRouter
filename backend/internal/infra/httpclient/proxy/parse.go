package proxy

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// allowedSchemes 列出支持的代理协议。
var allowedSchemes = map[string]bool{
	"http":    true,
	"https":   true,
	"socks5":  true,
	"socks5h": true,
}

// Parse 解析代理 URL，支持 HTTP、HTTPS、SOCKS5 和 SOCKS5H。
// 去除首尾空白后为空时返回直连结果，解析失败、缺少主机或协议不受支持时返回错误。
// SOCKS5 地址转换为 SOCKS5H，有效地址返回去除空白的字符串和解析结果。
func Parse(raw string) (trimmed string, parsed *url.URL, err error) {
	trimmed = strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil, nil
	}

	parsed, err = url.Parse(trimmed)
	if err != nil {
		// 将 URL 解析错误写入返回的错误消息。
		return "", nil, fmt.Errorf("invalid proxy URL: %v", err)
	}

	if parsed.Host == "" || parsed.Hostname() == "" {
		return "", nil, fmt.Errorf("proxy URL missing host: %s", parsed.Redacted())
	}

	scheme := strings.ToLower(parsed.Scheme)
	if !allowedSchemes[scheme] {
		return "", nil, fmt.Errorf("unsupported proxy scheme %q (allowed: http, https, socks5, socks5h)", scheme)
	}

	// 将 SOCKS5 地址规范为 SOCKS5H。
	if scheme == "socks5" {
		parsed.Scheme = "socks5h"
		trimmed = parsed.String()
	}

	return trimmed, parsed, nil
}

// NormalizePoolKey 返回代理连接池使用的规范 URL 键和解析结果。
func NormalizePoolKey(raw string) (string, *url.URL, error) {
	_, parsed, err := Parse(raw)
	if err != nil {
		return "", nil, err
	}
	if parsed == nil {
		return "direct", nil, nil
	}
	// 规范化：小写 scheme/host，去除路径和查询参数
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.ForceQuery = false
	if hostname := parsed.Hostname(); hostname != "" {
		port := parsed.Port()
		if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
			port = ""
		}
		hostname = strings.ToLower(hostname)
		if port != "" {
			parsed.Host = net.JoinHostPort(hostname, port)
		} else {
			parsed.Host = hostname
		}
	}
	return parsed.String(), parsed, nil
}
