// IPAM API 压测工具（M6-011）：无外部依赖，登录后并发打点，输出 RPS 与分位延迟。
//
// 用法：
//
//	go run ./tests/load -base http://127.0.0.1:8443 -path /api/v1/dashboard \
//	  -user admin -password admin123 -concurrency 20 -duration 10s
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

func main() {
	base := flag.String("base", "http://127.0.0.1:8443", "控制面地址")
	user := flag.String("user", "admin", "登录账号")
	password := flag.String("password", "admin123", "登录口令")
	path := flag.String("path", "/api/v1/dashboard", "压测路径")
	concurrency := flag.Int("concurrency", 20, "并发数")
	duration := flag.Duration("duration", 10*time.Second, "持续时间")
	flag.Parse()

	client := &http.Client{Timeout: 15 * time.Second}
	token, err := login(client, *base, *user, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
		os.Exit(1)
	}

	var mu sync.Mutex
	latencies := make([]time.Duration, 0, *concurrency*1000)
	var errCount int
	deadline := time.Now().Add(*duration)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				t0 := time.Now()
				req, _ := http.NewRequest(http.MethodGet, *base+*path, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := client.Do(req)
				cost := time.Since(t0)
				ok := err == nil && resp != nil && resp.StatusCode < 400
				if resp != nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
				mu.Lock()
				latencies = append(latencies, cost)
				if !ok {
					errCount++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	pct := func(p float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}
		idx := int(float64(len(latencies)-1) * p)
		return latencies[idx]
	}
	fmt.Printf("目标: %s%s\n", *base, *path)
	fmt.Printf("并发: %d  时长: %s  请求: %d  错误: %d\n", *concurrency, elapsed.Round(time.Millisecond), len(latencies), errCount)
	fmt.Printf("RPS: %.1f\n", float64(len(latencies))/elapsed.Seconds())
	fmt.Printf("p50: %s  p95: %s  p99: %s\n", pct(0.50).Round(time.Microsecond), pct(0.95).Round(time.Microsecond), pct(0.99).Round(time.Microsecond))
	if errCount > 0 {
		os.Exit(1)
	}
}

func login(client *http.Client, base, user, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": user, "password": password})
	resp, err := client.Post(base+"/api/v1/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("响应缺少 accessToken")
	}
	return out.AccessToken, nil
}
