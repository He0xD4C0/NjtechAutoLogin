package main

import (
	"context"
	"strings"
	"time"
)

var checkURLs = []struct {
	url     string
	keyword string
	bytes   string
}{
	{url: "http://www.msftncsi.com/ncsi.txt", bytes: "Microsoft NCSI"},
	{url: "http://captive.apple.com/hotspot-detect.html", keyword: "Success"},
	{url: "http://www.baidu.com", keyword: "百度"},
	{url: "http://connect.rom.miui.com/generate_204", bytes: ""},
	{url: "http://www.qq.com", keyword: "腾讯"},
	{url: "http://www.163.com", keyword: "网易"},
}

func checkInternet(ctx context.Context) bool {
	for _, endpoint := range checkURLs {
		req, err := newRequest(ctx, "GET", endpoint.url, nil)
		if err != nil {
			continue
		}
		req.Close = true
		resp, err := httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			continue
		}
		body, err := readResponseBody(resp)
		if err != nil {
			continue
		}
		if strings.Contains(string(body), "Dr.COMWebLoginID") {
			return false
		}
		if endpoint.bytes != "" {
			if strings.TrimSpace(string(body)) == endpoint.bytes {
				return true
			}
		} else if endpoint.keyword != "" {
			if strings.Contains(string(body), endpoint.keyword) {
				return true
			}
		} else {
			return true
		}
	}
	return false
}

func waitCtx(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func tryLoginWithRetry(ctx context.Context, cfg Config, maxAttempts int) bool {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return false
		}
		if attempt > 0 {
			globalLogger.Printf("重新登录，第 %d 次重试", attempt)
		}
		err := login(ctx, cfg.Username, cfg.Password, cfg.Provider)
		if err == nil {
			globalLogger.Println("登录成功")
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		globalLogger.Printf("登录失败: %v", err)
		if strings.Contains(err.Error(), "停机") || strings.Contains(err.Error(), "状态异常") {
			globalLogger.Println("账号异常，放弃重试")
			return false
		}
		if attempt < maxAttempts-1 && !waitCtx(ctx, 3*time.Second) {
			return false
		}
	}
	return false
}

func monitorLoop(ctx context.Context, cfg Config) {
	const (
		onlineCheckInterval  = 5 * time.Minute
		offlineRetryInterval = 15 * time.Minute
		maxReconnectAttempts = 2
		maxInitialAttempts   = 3
	)

	globalLogger.Println("开始初始连接...")
	online := tryLoginWithRetry(ctx, cfg, maxInitialAttempts)
	if online {
		globalLogger.Println("初始连接成功")
	} else if ctx.Err() == nil {
		globalLogger.Println("初始连接失败，进入离线重试状态")
	}

	for ctx.Err() == nil {
		if online {
			globalLogger.Printf("在线检测：%v 后检查", onlineCheckInterval)
			if !waitCtx(ctx, onlineCheckInterval) {
				return
			}
			if checkInternet(ctx) {
				globalLogger.Println("在线检测通过")
			} else if ctx.Err() == nil {
				globalLogger.Println("检测到掉线，尝试重连")
				online = tryLoginWithRetry(ctx, cfg, maxReconnectAttempts)
				if !online && ctx.Err() == nil {
					globalLogger.Printf("重连 %d 次均失败，%v 后重新尝试", maxReconnectAttempts, offlineRetryInterval)
				}
			}
		} else {
			globalLogger.Printf("离线重试：%v 后尝试重连", offlineRetryInterval)
			if !waitCtx(ctx, offlineRetryInterval) {
				return
			}
			online = tryLoginWithRetry(ctx, cfg, maxReconnectAttempts)
			if online {
				globalLogger.Println("离线重试成功，进入在线监控")
			}
		}
	}
}
