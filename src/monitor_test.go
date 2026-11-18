package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCheckInternetEndpointContract(t *testing.T) {
	oldURLs := checkURLs
	oldClient := httpClient
	defer func() {
		checkURLs = oldURLs
		httpClient = oldClient
	}()

	tests := []struct {
		name     string
		endpoint internetEndpoint
		status   int
		body     string
		want     bool
	}{
		{
			name:     "empty 204 is online",
			endpoint: internetEndpoint{url: "http://check.test/generate_204", status: http.StatusNoContent},
			status:   http.StatusNoContent,
			want:     true,
		},
		{
			name:     "non-empty 204 is not online",
			endpoint: internetEndpoint{url: "http://check.test/generate_204", status: http.StatusNoContent},
			status:   http.StatusNoContent,
			body:     "captive portal",
			want:     false,
		},
		{
			name:     "Dr.COM page is offline",
			endpoint: internetEndpoint{url: "http://check.test/page", status: http.StatusOK, keyword: "百度"},
			status:   http.StatusOK,
			body:     "Dr.COMWebLoginID",
			want:     false,
		},
		{
			name:     "wrong status is not online",
			endpoint: internetEndpoint{url: "http://check.test/page", status: http.StatusOK, keyword: "百度"},
			status:   http.StatusInternalServerError,
			body:     "百度",
			want:     false,
		},
		{
			name:     "expected 200 body is online",
			endpoint: internetEndpoint{url: "http://check.test/page", status: http.StatusOK, keyword: "百度"},
			status:   http.StatusOK,
			body:     "百度",
			want:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checkURLs = []internetEndpoint{test.endpoint}
			httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.status,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(test.body)),
					Request:    req,
				}, nil
			})}
			if got := checkInternet(context.Background()); got != test.want {
				t.Fatalf("checkInternet() = %v, want %v", got, test.want)
			}
		})
	}
}
