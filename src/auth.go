package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var globalLogger *Logger

var httpClient = newHTTPClient()

func newHTTPClient() *http.Client {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return nil
		},
	}
	jar, err := cookiejar.New(nil)
	if err == nil {
		client.Jar = jar
	}
	return client
}

// getOutboundIP keeps the original portal-first routing behavior.
func getOutboundIP() (net.IP, string, error) {
	conn, err := net.Dial("udp", "10.50.255.11:80")
	if err != nil {
		conn, err = net.Dial("udp", "114.114.114.114:53")
		if err != nil {
			return nil, "", fmt.Errorf("无法获取出口IP: %v", err)
		}
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP, "", nil
}

// newRequest preserves the v1 wire headers; context only adds cancellation.
func newRequest(ctx context.Context, method, requestURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Cache-Control", "max-age=0")
	return req, nil
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		globalLogger.Printf("检测到gzip压缩数据，尝试解压")
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("创建gzip reader失败: %v", err)
		}
		defer reader.Close()
		decompressed, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("gzip解压失败: %v", err)
		}
		return decompressed, nil
	}
	return body, nil
}

func visitHomepage(ctx context.Context) error {
	homepageURL := "http://10.50.255.11/"
	req, err := newRequest(ctx, http.MethodGet, homepageURL, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("访问首页失败: %v", err)
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return fmt.Errorf("读取首页响应失败: %v", err)
	}
	globalLogger.Printf("首页访问完成，状态码: %d，内容长度: %d", resp.StatusCode, len(body))
	return nil
}

// login is protocol-frozen. Only context cancellation and sensitive-log removal
// differ from v1; request URLs, parameters, headers and response decisions match.
func login(ctx context.Context, username, password, provider string) error {
	if err := visitHomepage(ctx); err != nil {
		globalLogger.Printf("访问首页失败（不影响登录尝试）: %v", err)
	}

	ip, iface, err := getOutboundIP()
	if err != nil {
		return fmt.Errorf("获取本机IP失败: %v", err)
	}
	ipStr := ip.String()
	if iface != "" {
		globalLogger.Printf("使用IP: %s (接口: %s)", ipStr, iface)
	} else {
		globalLogger.Printf("使用IP: %s", ipStr)
	}

	encodedIP := url.QueryEscape(base64.StdEncoding.EncodeToString([]byte(ipStr)))
	loadConfigURL := fmt.Sprintf("http://10.50.255.11:801/eportal/portal/page/loadConfig?callback=dr1001&program_index=&wlan_vlan_id=0&wlan_user_ip=%s&wlan_user_ipv6=&wlan_user_ssid=&wlan_user_areaid=&wlan_ac_ip=&wlan_ap_mac=000000000000&gw_id=000000000000&jsVersion=4.X&v=%d&lang=zh",
		encodedIP, time.Now().UnixNano()/1e6)

	req, err := newRequest(ctx, http.MethodGet, loadConfigURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "http://10.50.255.11/")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("loadConfig请求失败: %v", err)
	}
	body, err := readResponseBody(resp)
	if err != nil {
		return fmt.Errorf("读取loadConfig响应失败: %v", err)
	}
	if !bytes.Contains(body, []byte(`"code":1`)) {
		globalLogger.Printf("loadConfig返回异常（响应已省略）")
	}

	userAccount := fmt.Sprintf(",0,%s@%s", username, provider)
	loginURL := fmt.Sprintf("http://10.50.255.11:801/eportal/portal/login?callback=dr1003&login_method=1&user_account=%s&user_password=%s&wlan_user_ip=%s&wlan_user_ipv6=&wlan_user_mac=000000000000&wlan_ac_ip=&wlan_ac_name=&jsVersion=4.1.3&terminal_type=1&lang=zh-cn&v=%d&lang=zh",
		url.QueryEscape(userAccount), url.QueryEscape(password), ipStr, time.Now().UnixNano()/1e6)
	req, err = newRequest(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "http://10.50.255.11/")
	resp, err = httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("登录请求失败: %v", safeRequestError(err))
	}
	body, err = readResponseBody(resp)
	if err != nil {
		return fmt.Errorf("读取登录响应失败: %v", err)
	}

	re := regexp.MustCompile(`dr1003\s*\(\s*({.*?})\s*\)\s*`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return fmt.Errorf("登录响应解析失败，无法提取JSON（响应已省略）")
	}
	var result struct {
		Result int    `json:"result"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(matches[1], &result); err != nil {
		return fmt.Errorf("JSON解析失败: %v", err)
	}
	if result.Result != 1 {
		message := redactSecret(result.Msg, password)
		if strings.Contains(result.Msg, "已经在线") {
			globalLogger.Println("登录成功（IP已在线）")
			return nil
		}
		if strings.Contains(result.Msg, "停机") || strings.Contains(result.Msg, "状态异常") {
			return fmt.Errorf("登录失败: %s (账号可能被暂时封禁，请等待几分钟后再试)", message)
		}
		return fmt.Errorf("登录失败: %s", message)
	}
	globalLogger.Println("登录成功")
	return nil
}

func redactSecret(value, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[REDACTED]")
}

// safeRequestError strips net/http's URL field because the frozen portal
// protocol carries the password in the query string.
func safeRequestError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
