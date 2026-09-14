package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNewRequestPreservesV1Headers(t *testing.T) {
	req, err := newRequest(context.Background(), http.MethodGet, "http://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{
		"User-Agent":                "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7",
		"Accept-Language":           "zh-CN,zh;q=0.9,en;q=0.8",
		"Accept-Encoding":           "gzip, deflate",
		"Connection":                "keep-alive",
		"Upgrade-Insecure-Requests": "1",
		"Cache-Control":             "max-age=0",
	}
	for key, value := range expected {
		if got := req.Header.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestLoginPreservesV1RequestContract(t *testing.T) {
	oldClient := httpClient
	oldLogger := globalLogger
	defer func() {
		httpClient = oldClient
		globalLogger = oldLogger
	}()
	globalLogger = NewStreamLogger(io.Discard)

	var mu sync.Mutex
	var requests []*http.Request
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(context.Background())
		mu.Lock()
		requests = append(requests, clone)
		mu.Unlock()
		body := "homepage"
		if strings.Contains(req.URL.Path, "loadConfig") {
			body = `dr1001({"code":1})`
		}
		if strings.Contains(req.URL.Path, "/login") {
			body = `dr1003({"result":1,"msg":"ok"})`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}

	if err := login(context.Background(), "student", "secret", "telecom"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 {
		t.Fatalf("got %d requests, want 3", len(requests))
	}
	if got := requests[0].URL.String(); got != "http://10.50.255.11/" {
		t.Fatalf("homepage URL = %q", got)
	}
	load := requests[1]
	if load.URL.Host != "10.50.255.11:801" || load.URL.Path != "/eportal/portal/page/loadConfig" {
		t.Fatalf("unexpected loadConfig URL: %s", load.URL)
	}
	assertQueryValue(t, load.URL, "callback", "dr1001")
	assertQueryValue(t, load.URL, "jsVersion", "4.X")
	assertExactQueryKeys(t, load.URL, map[string][]string{
		"callback": {"dr1001"}, "program_index": {""}, "wlan_vlan_id": {"0"},
		"wlan_user_ip": {load.URL.Query().Get("wlan_user_ip")}, "wlan_user_ipv6": {""},
		"wlan_user_ssid": {""}, "wlan_user_areaid": {""}, "wlan_ac_ip": {""},
		"wlan_ap_mac": {"000000000000"}, "gw_id": {"000000000000"},
		"jsVersion": {"4.X"}, "v": {load.URL.Query().Get("v")}, "lang": {"zh"},
	})
	loginReq := requests[2]
	if loginReq.URL.Host != "10.50.255.11:801" || loginReq.URL.Path != "/eportal/portal/login" {
		t.Fatalf("unexpected login URL: %s", loginReq.URL)
	}
	assertQueryValue(t, loginReq.URL, "callback", "dr1003")
	assertQueryValue(t, loginReq.URL, "user_account", ",0,student@telecom")
	assertQueryValue(t, loginReq.URL, "user_password", "secret")
	assertQueryValue(t, loginReq.URL, "jsVersion", "4.1.3")
	assertExactQueryKeys(t, loginReq.URL, map[string][]string{
		"callback": {"dr1003"}, "login_method": {"1"}, "user_account": {",0,student@telecom"},
		"user_password": {"secret"}, "wlan_user_ip": {loginReq.URL.Query().Get("wlan_user_ip")},
		"wlan_user_ipv6": {""}, "wlan_user_mac": {"000000000000"}, "wlan_ac_ip": {""},
		"wlan_ac_name": {""}, "jsVersion": {"4.1.3"}, "terminal_type": {"1"},
		"lang": {"zh-cn", "zh"}, "v": {loginReq.URL.Query().Get("v")},
	})
	if got := loginReq.Header.Get("Referer"); got != "http://10.50.255.11/" {
		t.Fatalf("Referer = %q", got)
	}
}

func assertExactQueryKeys(t *testing.T, parsed *url.URL, expected url.Values) {
	t.Helper()
	got := parsed.Query()
	if len(got) != len(expected) {
		t.Fatalf("query keys = %v, want %v", got, expected)
	}
	for key, values := range expected {
		actual := got[key]
		if strings.Join(actual, "\x00") != strings.Join(values, "\x00") {
			t.Errorf("query %s = %q, want %q", key, actual, values)
		}
	}
}

func assertQueryValue(t *testing.T, parsed *url.URL, key, expected string) {
	t.Helper()
	if got := parsed.Query().Get(key); got != expected {
		t.Errorf("query %s = %q, want %q", key, got, expected)
	}
}

func TestVisitHomepageHonorsCancellation(t *testing.T) {
	oldClient := httpClient
	oldLogger := globalLogger
	defer func() {
		httpClient = oldClient
		globalLogger = oldLogger
	}()
	globalLogger = NewStreamLogger(io.Discard)
	httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := visitHomepage(ctx); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}
}

func TestSafeRequestErrorRemovesSensitiveURL(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "http://portal/login?user_password=secret", Err: errors.New("network down")}
	got := safeRequestError(err).Error()
	if strings.Contains(got, "secret") || got != "network down" {
		t.Fatalf("unsafe error: %q", got)
	}
}

func TestRedactSecretRemovesPasswordFromPortalMessage(t *testing.T) {
	got := redactSecret("password secret was rejected", "secret")
	if strings.Contains(got, "secret") || got != "password [REDACTED] was rejected" {
		t.Fatalf("unsafe message: %q", got)
	}
}
