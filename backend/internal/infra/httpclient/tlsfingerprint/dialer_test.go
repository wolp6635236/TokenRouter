package tlsfingerprint

import (
	"context"
	stdtls "crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// FingerprintResponse 保存 tls.peet.ws/api/all 返回的指纹数据。
type FingerprintResponse struct {
	IP    string  `json:"ip"`
	TLS   TLSInfo `json:"tls"`
	HTTP2 any     `json:"http2"`
}

// TestProfileExpectation 描述一个模板的预期 TLS 指纹。
type TestProfileExpectation struct {
	Profile       *Profile
	ExpectedJA3   string // 预期 JA3 哈希，空字符串表示跳过检查。
	ExpectedJA4   string // 预期完整 JA4，空字符串表示跳过检查。
	JA4CipherHash string // JA4 中部的加密套件哈希，空字符串表示跳过检查。
}

// TLSInfo 保存 JA3、JA4 和 TLS 会话参数。
type TLSInfo struct {
	JA3           string `json:"ja3"`
	JA3Hash       string `json:"ja3_hash"`
	JA4           string `json:"ja4"`
	PeetPrint     string `json:"peetprint"`
	PeetPrintHash string `json:"peetprint_hash"`
	ClientRandom  string `json:"client_random"`
	SessionID     string `json:"session_id"`
}

// TestDialerBasicConnection 检查拨号器建立 TLS 连接。
func TestDialerBasicConnection(t *testing.T) {
	skipNetworkTest(t)

	// 使用默认模板创建拨号器。
	profile := &Profile{
		Name:         "Test Profile",
		EnableGREASE: false,
	}
	dialer := NewDialer(profile, nil)

	// HTTP 客户端使用自定义 TLS 拨号器。
	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: dialer.DialTLSContext,
		},
		Timeout: 30 * time.Second,
	}

	// 请求 Google HTTPS 端点。
	resp, err := client.Get("https://www.google.com")
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

// TestJA3Fingerprint 通过 tls.peet.ws 检查 JA3 和 JA4 指纹。
// 预期 JA3 哈希： 44f88fca027f27bab4bb08d4af15f23e (Node.js 24.x)
// 预期 JA4： t13d1714h1_5b57614c22b0_7baf387fc6ff。
func TestJA3Fingerprint(t *testing.T) {
	skipNetworkTest(t)

	profile := &Profile{
		Name:         "Default Profile Test",
		EnableGREASE: false,
	}
	dialer := NewDialer(profile, nil)

	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: dialer.DialTLSContext,
		},
		Timeout: 30 * time.Second,
	}

	// 请求 tls.peet.ws 指纹检测 API。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://tls.peet.ws/api/all", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("User-Agent", "Claude Code/2.0.0 Node.js/24.3.0")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to get fingerprint: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	var fpResp FingerprintResponse
	if err := json.Unmarshal(body, &fpResp); err != nil {
		t.Logf("Response body: %s", string(body))
		t.Fatalf("failed to parse fingerprint response: %v", err)
	}

	// 记录指纹数据。
	t.Logf("JA3: %s", fpResp.TLS.JA3)
	t.Logf("JA3 Hash: %s", fpResp.TLS.JA3Hash)
	t.Logf("JA4: %s", fpResp.TLS.JA4)
	t.Logf("PeetPrint: %s", fpResp.TLS.PeetPrint)
	t.Logf("PeetPrint Hash: %s", fpResp.TLS.PeetPrintHash)

	// 比较 Node.js 24.x 默认 JA3 哈希。
	expectedJA3Hash := "44f88fca027f27bab4bb08d4af15f23e"
	if fpResp.TLS.JA3Hash == expectedJA3Hash {
		t.Logf("✓ JA3 hash matches expected value: %s", expectedJA3Hash)
	} else {
		t.Errorf("✗ JA3 hash mismatch: got %s, expected %s", fpResp.TLS.JA3Hash, expectedJA3Hash)
	}

	// 比较 JA4 中部的加密套件哈希。
	expectedJA4CipherHash := "_5b57614c22b0_"
	if strings.Contains(fpResp.TLS.JA4, expectedJA4CipherHash) {
		t.Logf("✓ JA4 cipher hash matches: %s", expectedJA4CipherHash)
	} else {
		t.Errorf("✗ JA4 cipher hash mismatch: got %s, expected containing %s", fpResp.TLS.JA4, expectedJA4CipherHash)
	}

	// 检查 JA4 前缀为 t13d1714h1 或 t13i1714h1。
	expectedJA4Prefix := "t13d1714h1"
	if strings.HasPrefix(fpResp.TLS.JA4, expectedJA4Prefix) {
		t.Logf("✓ JA4 prefix matches: %s (t13=TLS1.3, d=domain, 17=ciphers, 14=extensions, h1=HTTP/1.1)", expectedJA4Prefix)
	} else {
		altPrefix := "t13i1714h1"
		if strings.HasPrefix(fpResp.TLS.JA4, altPrefix) {
			t.Logf("✓ JA4 prefix matches (IP variant): %s", altPrefix)
		} else {
			t.Errorf("✗ JA4 prefix mismatch: got %s, expected %s or %s", fpResp.TLS.JA4, expectedJA4Prefix, altPrefix)
		}
	}

	// 检查 JA3 中的 TLS 1.3 加密套件。
	if strings.Contains(fpResp.TLS.JA3, "4865-4866-4867") {
		t.Logf("✓ JA3 contains expected TLS 1.3 cipher suites")
	} else {
		t.Logf("Warning: JA3 does not contain expected TLS 1.3 cipher suites")
	}

	// 比较按 Node.js 24.x 顺序排列的 14 个扩展。
	expectedExtensions := "0-65037-23-65281-10-11-35-16-5-13-18-51-45-43"
	if strings.Contains(fpResp.TLS.JA3, expectedExtensions) {
		t.Logf("✓ JA3 contains expected extension list: %s", expectedExtensions)
	} else {
		t.Logf("Warning: JA3 extension list may differ")
	}
}

func skipNetworkTest(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过网络测试（short 模式）")
	}
	if os.Getenv("TLSFINGERPRINT_NETWORK_TESTS") != "1" {
		t.Skip("跳过网络测试（需要设置 TLSFINGERPRINT_NETWORK_TESTS=1）")
	}
}

// TestDialerWithProfile 比较不同模板生成的 ClientHello。
func TestDialerWithProfile(t *testing.T) {
	// 用两个模板创建拨号器。
	profile1 := &Profile{
		Name:         "Profile 1 - No GREASE",
		EnableGREASE: false,
	}
	profile2 := &Profile{
		Name:         "Profile 2 - With GREASE",
		EnableGREASE: true,
	}

	dialer1 := NewDialer(profile1, nil)
	dialer2 := NewDialer(profile2, nil)

	// 通过构造 ClientHello 比较模板差异。
	spec1 := buildClientHelloSpecFromProfile(dialer1.profile)
	spec2 := buildClientHelloSpecFromProfile(dialer2.profile)

	// 开启 GREASE 后应增加扩展项。
	if len(spec2.Extensions) <= len(spec1.Extensions) {
		t.Error("expected GREASE profile to have more extensions")
	}
}

// TestHTTPProxyDialerBasic 检查 HTTP 代理拨号器的构造结果。
func TestHTTPProxyDialerBasic(t *testing.T) {
	profile := &Profile{
		Name:         "Test Profile",
		EnableGREASE: false,
	}

	// 构造代理拨号器。
	proxyURL := mustParseURL("http://proxy.example.com:8080")
	dialer := NewHTTPProxyDialer(profile, proxyURL)

	if dialer == nil {
		t.Fatal("expected dialer to be created")
	}
	if dialer.profile != profile {
		t.Error("expected profile to be set")
	}
	if dialer.proxyURL != proxyURL {
		t.Error("expected proxyURL to be set")
	}
}

func TestHTTPProxyDialerSupportsHTTPSProxyCONNECT(t *testing.T) {
	var connectSeen atomic.Bool
	proxyServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect && r.Host == "upstream.example:443" {
			connectSeen.Store(true)
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "unexpected request", http.StatusBadRequest)
	}))
	defer proxyServer.Close()

	proxyURL := mustParseURL(proxyServer.URL)
	dialer := NewHTTPProxyDialer(&Profile{Name: "Test Profile"}, proxyURL)
	// 使用本地代理夹具的证书验证 HTTPS 代理身份。
	roots := x509.NewCertPool()
	roots.AddCert(proxyServer.Certificate())
	dialer.proxyTLSConfig = &stdtls.Config{RootCAs: roots}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := dialer.DialTLSContext(ctx, "tcp", net.JoinHostPort("upstream.example", "443"))
	if conn != nil {
		_ = conn.Close()
	}

	if !connectSeen.Load() {
		t.Fatal("expected HTTPS proxy to receive CONNECT request")
	}
	if err == nil || !strings.Contains(err.Error(), "TLS handshake failed") {
		t.Fatalf("expected target TLS handshake failure after CONNECT, got %v", err)
	}
}

// TestSOCKS5ProxyDialerBasic 检查 SOCKS5 代理拨号器的构造结果。
func TestSOCKS5ProxyDialerBasic(t *testing.T) {
	profile := &Profile{
		Name:         "Test Profile",
		EnableGREASE: false,
	}

	// 构造代理拨号器。
	proxyURL := mustParseURL("socks5://proxy.example.com:1080")
	dialer := NewSOCKS5ProxyDialer(profile, proxyURL)

	if dialer == nil {
		t.Fatal("expected dialer to be created")
	}
	if dialer.profile != profile {
		t.Error("expected profile to be set")
	}
	if dialer.proxyURL != proxyURL {
		t.Error("expected proxyURL to be set")
	}
}

// TestBuildClientHelloSpec 检查 ClientHello 的默认值和自定义模板字段。
func TestBuildClientHelloSpec(t *testing.T) {
	// nil 模板使用默认值。
	spec := buildClientHelloSpecFromProfile(nil)

	if len(spec.CipherSuites) == 0 {
		t.Error("expected cipher suites to be set")
	}
	if len(spec.Extensions) == 0 {
		t.Error("expected extensions to be set")
	}

	// 比较默认加密套件。
	if len(spec.CipherSuites) != len(defaultCipherSuites) {
		t.Errorf("expected %d cipher suites, got %d", len(defaultCipherSuites), len(spec.CipherSuites))
	}

	// 设置自定义模板。
	customProfile := &Profile{
		Name:         "Custom",
		EnableGREASE: false,
		CipherSuites: []uint16{0x1301, 0x1302},
	}
	spec = buildClientHelloSpecFromProfile(customProfile)

	if len(spec.CipherSuites) != 2 {
		t.Errorf("expected 2 cipher suites, got %d", len(spec.CipherSuites))
	}
}

// TestToUTLSCurves 检查曲线 ID 的转换。
func TestToUTLSCurves(t *testing.T) {
	input := []uint16{0x001d, 0x0017, 0x0018}
	result := toUTLSCurves(input)

	if len(result) != len(input) {
		t.Errorf("expected %d curves, got %d", len(input), len(result))
	}

	for i, curve := range result {
		if uint16(curve) != input[i] {
			t.Errorf("curve %d: expected 0x%04x, got 0x%04x", i, input[i], uint16(curve))
		}
	}
}

// mustParseURL 解析测试 URL，失败时触发 panic。
func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	return u
}

// TestAllProfiles 通过 tls.peet.ws 检查多个 TLS 指纹模板。
// 运行：TLSFINGERPRINT_NETWORK_TESTS=1 go test -v -run TestAllProfiles ./internal/infra/httpclient/tlsfingerprint/...
func TestAllProfiles(t *testing.T) {
	skipNetworkTest(t)

	profiles := []TestProfileExpectation{
		{
			// 默认模板对应 Node.js 24.x。
			// JA3 哈希： 44f88fca027f27bab4bb08d4af15f23e
			// JA4： t13d1714h1_5b57614c22b0_7baf387fc6ff
			Profile: &Profile{
				Name:         "default_node_v24",
				EnableGREASE: false,
			},
			JA4CipherHash: "5b57614c22b0",
		},
		{
			// Linux x64 Node.js v22.17.1 模板。
			Profile: &Profile{
				Name:         "linux_x64_node_v22171",
				EnableGREASE: false,
				CipherSuites: []uint16{
					4866,
					4867,
					4865,
					49199,
					49195,
					49200,
					49196,
					158,
					49191,
					103,
					49192,
					107,
					163,
					159,
					52393,
					52392,
					52394,
					49327,
					49325,
					49315,
					49311,
					49245,
					49249,
					49239,
					49235,
					162,
					49326,
					49324,
					49314,
					49310,
					49244,
					49248,
					49238,
					49234,
					49188,
					106,
					49187,
					64,
					49162,
					49172,
					57,
					56,
					49161,
					49171,
					51,
					50,
					157,
					49313,
					49309,
					49233,
					156,
					49312,
					49308,
					49232,
					61,
					60,
					53,
					47,
					255,
				},
				Curves:       []uint16{29, 23, 30, 25, 24, 256, 257, 258, 259, 260},
				PointFormats: []uint16{0, 1, 2},
				Extensions:   []uint16{0, 11, 10, 35, 16, 22, 23, 13, 43, 45, 51},
			},
			JA4CipherHash: "a33745022dd6",
		},
	}

	for _, tc := range profiles {
		// 保存当前模板。
		t.Run(tc.Profile.Name, func(t *testing.T) {
			fp := fetchFingerprint(t, tc.Profile)
			if fp == nil {
				return // fetchFingerprint 已调用 t.Fatal。
			}

			t.Logf("Profile: %s", tc.Profile.Name)
			t.Logf("  JA3:           %s", fp.JA3)
			t.Logf("  JA3 Hash:      %s", fp.JA3Hash)
			t.Logf("  JA4:           %s", fp.JA4)
			t.Logf("  PeetPrint:     %s", fp.PeetPrint)
			t.Logf("  PeetPrintHash: %s", fp.PeetPrintHash)

			// 比较预期指纹。
			if tc.ExpectedJA3 != "" {
				if fp.JA3Hash == tc.ExpectedJA3 {
					t.Logf("  ✓ JA3 hash matches: %s", tc.ExpectedJA3)
				} else {
					t.Errorf("  ✗ JA3 hash mismatch: got %s, expected %s", fp.JA3Hash, tc.ExpectedJA3)
				}
			}

			if tc.ExpectedJA4 != "" {
				if fp.JA4 == tc.ExpectedJA4 {
					t.Logf("  ✓ JA4 matches: %s", tc.ExpectedJA4)
				} else {
					t.Errorf("  ✗ JA4 mismatch: got %s, expected %s", fp.JA4, tc.ExpectedJA4)
				}
			}

			// 比较 JA4 中部的加密套件哈希。
			// JA4 格式：prefix_cipherHash_extHash。
			if tc.JA4CipherHash != "" {
				if strings.Contains(fp.JA4, "_"+tc.JA4CipherHash+"_") {
					t.Logf("  ✓ JA4 cipher hash matches: %s", tc.JA4CipherHash)
				} else {
					t.Errorf("  ✗ JA4 cipher hash mismatch: got %s, expected cipher hash %s", fp.JA4, tc.JA4CipherHash)
				}
			}
		})
	}
}

// fetchFingerprint 请求 tls.peet.ws 并返回 TLS 指纹。
func fetchFingerprint(t *testing.T, profile *Profile) *TLSInfo {
	t.Helper()

	dialer := NewDialer(profile, nil)
	client := &http.Client{
		Transport: &http.Transport{
			DialTLSContext: dialer.DialTLSContext,
		},
		Timeout: 30 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://tls.peet.ws/api/all", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
		return nil
	}
	req.Header.Set("User-Agent", "Claude Code/2.0.0 Node.js/20.0.0")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to get fingerprint: %v", err)
		return nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
		return nil
	}

	var fpResp FingerprintResponse
	if err := json.Unmarshal(body, &fpResp); err != nil {
		t.Logf("Response body: %s", string(body))
		t.Fatalf("failed to parse fingerprint response: %v", err)
		return nil
	}

	return &fpResp.TLS
}

// TestTLSFingerprintNetworkDialerHasBoundedTimeout 检查 TLS 指纹拨号器自身的建连和握手超时。
// 该拨号器绕过 http.Transport.DialContext。
func TestTLSFingerprintNetworkDialerHasBoundedTimeout(t *testing.T) {
	dialer := newTLSFingerprintNetworkDialer()

	require.Equal(t, 10*time.Second, defaultTLSFingerprintDialTimeout)
	require.Equal(t, defaultTLSFingerprintDialTimeout, dialer.Timeout)
	require.Equal(t, defaultTLSFingerprintDialKeepAlive, dialer.KeepAlive)
	require.Equal(t, 10*time.Second, defaultTLSFingerprintHandshakeTimeout)
}
