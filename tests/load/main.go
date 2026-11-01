// IPAM API 压测工具（M6-011 / M6-016）：无外部依赖，登录后并发打点，输出 RPS 与分位延迟。
//
// 单路径：
//
//	go run ./tests/load -base http://127.0.0.1:8443 -path /api/v1/dashboard \
//	  -user admin -password admin123 -concurrency 20 -duration 10s
//
// 多路径 + 报告（全链路容量报告用）：
//
//	go run ./tests/load -base http://127.0.0.1:8443 -concurrency 10 -duration 15s \
//	  -paths /api/v1/dashboard,/api/v1/subnets,/api/v1/logs?type=dns&pageSize=50 \
//	  -out tests/load/artifacts/fullstack.json
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Result 单个路径的压测结果（同时用于终端输出与 JSON 报告）。
type Result struct {
	Path        string  `json:"path"`
	Concurrency int     `json:"concurrency"`
	DurationMS  int64   `json:"durationMs"`
	Requests    int     `json:"requests"`
	Errors      int     `json:"errors"`
	RPS         float64 `json:"rps"`
	P50US       int64   `json:"p50us"`
	P95US       int64   `json:"p95us"`
	P99US       int64   `json:"p99us"`
	MaxUS       int64   `json:"maxUs"`
}

// Report 一次压测运行的完整报告。
type Report struct {
	Base       string    `json:"base"`
	User       string    `json:"user"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	Results    []Result  `json:"results"`
}

func main() {
	base := flag.String("base", "http://127.0.0.1:8443", "控制面地址")
	user := flag.String("user", "admin", "登录账号")
	password := flag.String("password", "admin123", "登录口令")
	path := flag.String("path", "/api/v1/dashboard", "压测路径（单条，兼容旧用法）")
	paths := flag.String("paths", "", "压测路径列表（逗号分隔，优先于 -path）")
	out := flag.String("out", "", "JSON 报告输出路径（可选）")
	concurrency := flag.Int("concurrency", 20, "并发数")
	duration := flag.Duration("duration", 10*time.Second, "每路径持续时间")
	warmup := flag.Duration("warmup", 0, "每路径预热时长（结果不计入）")
	insecure := flag.Bool("insecure", false, "跳过 TLS 证书校验（自签证书环境）")
	flag.Parse()

	targets := splitPaths(*paths)
	if len(targets) == 0 {
		targets = []string{*path}
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: *insecure}, //nolint:gosec // 自签开发环境显式开关
			MaxIdleConnsPerHost: *concurrency + 8,
		},
	}
	token, err := login(client, *base, *user, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
		os.Exit(1)
	}

	report := Report{Base: *base, User: *user, StartedAt: time.Now().UTC()}
	fmt.Printf("目标: %s  并发: %d  每路径: %s（预热 %s）\n", *base, *concurrency, *duration, *warmup)
	fmt.Printf("%-46s %10s %8s %8s %8s %8s %8s\n", "路径", "RPS", "p50", "p95", "p99", "max", "错误")
	failed := false
	for _, p := range targets {
		res := runPath(client, *base, token, p, *concurrency, *duration, *warmup)
		report.Results = append(report.Results, res)
		fmt.Printf("%-46s %10.1f %8s %8s %8s %8s %8d\n",
			truncate(p, 46), res.RPS, dur(res.P50US), dur(res.P95US), dur(res.P99US), dur(res.MaxUS), res.Errors)
		if res.Errors > 0 {
			failed = true
		}
	}
	report.FinishedAt = time.Now().UTC()
	if *out != "" {
		if err := writeReport(*out, report); err != nil {
			fmt.Fprintf(os.Stderr, "写报告失败: %v\n", err)
			failed = true
		} else {
			fmt.Printf("报告已写入 %s\n", *out)
		}
	}
	if failed {
		os.Exit(1)
	}
}

func runPath(client *http.Client, base, token, path string, concurrency int, duration, warmup time.Duration) Result {
	// path 可能带查询串，保持原样拼接。
	url := base + path
	if warmup > 0 {
		_ = burn(client, url, token, concurrency, warmup)
	}

	var mu sync.Mutex
	latencies := make([]time.Duration, 0, concurrency*2048)
	var errCount int
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				t0 := time.Now()
				req, _ := http.NewRequest(http.MethodGet, url, nil)
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
		return latencies[int(float64(len(latencies)-1)*p)]
	}
	if len(latencies) == 0 {
		return Result{Path: path, Concurrency: concurrency, DurationMS: elapsed.Milliseconds(), Errors: errCount}
	}
	return Result{
		Path:        path,
		Concurrency: concurrency,
		DurationMS:  elapsed.Milliseconds(),
		Requests:    len(latencies),
		Errors:      errCount,
		RPS:         float64(len(latencies)) / elapsed.Seconds(),
		P50US:       pct(0.50).Microseconds(),
		P95US:       pct(0.95).Microseconds(),
		P99US:       pct(0.99).Microseconds(),
		MaxUS:       latencies[len(latencies)-1].Microseconds(),
	}
}

// burn 预热：跑满给定时间但不采集结果，避免冷启动（连接池/JIT/缓存）污染指标。
func burn(client *http.Client, url, token string, concurrency int, d time.Duration) error {
	deadline := time.Now().Add(d)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				req, _ := http.NewRequest(http.MethodGet, url, nil)
				req.Header.Set("Authorization", "Bearer "+token)
				if resp, err := client.Do(req); err == nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
				}
			}
		}()
	}
	wg.Wait()
	return nil
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

func writeReport(path string, r Report) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func splitPaths(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func dur(us int64) string {
	d := time.Duration(us) * time.Microsecond
	if d < time.Millisecond {
		return fmt.Sprintf("%.0fµs", float64(us))
	}
	return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
}
