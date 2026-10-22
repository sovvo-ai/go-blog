// 轻量压测工具：固定并发、固定请求数，输出 RPS 与延迟分位。
//
// 为什么自己写：阶段 7 只需要一个可重复、无外部依赖的基线数字，
// 装 wrk/hey 反而给新机器部署增加前置条件。20 行核心逻辑足够。
//
//	go run ./tools/loadtest -url http://127.0.0.1:8080/api/v1/articles -c 20 -n 2000
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:8080/healthz", "目标地址")
	conc := flag.Int("c", 20, "并发数")
	total := flag.Int("n", 1000, "总请求数")
	timeout := flag.Duration("timeout", 10*time.Second, "单请求超时")
	method := flag.String("method", http.MethodGet, "HTTP 方法")
	body := flag.String("data", "", "请求体（JSON）")
	flag.Parse()

	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			MaxIdleConns:        *conc * 2,
			MaxIdleConnsPerHost: *conc * 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	var (
		mu        sync.Mutex
		latencies = make([]time.Duration, 0, *total)
		errs      int64
		codes     = make(map[int]int)
		next      int64
		wg        sync.WaitGroup
	)

	start := time.Now()
	for i := 0; i < *conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if atomic.AddInt64(&next, 1) > int64(*total) {
					return
				}
				t0 := time.Now()
				var reader io.Reader
				if *body != "" {
					reader = strings.NewReader(*body)
				}
				req, rerr := http.NewRequest(*method, *url, reader)
				if rerr != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				if *body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				resp, err := client.Do(req)
				d := time.Since(t0)
				if err != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				mu.Lock()
				latencies = append(latencies, d)
				codes[resp.StatusCode]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	if len(latencies) == 0 {
		fmt.Fprintln(os.Stderr, "全部请求失败，请检查目标地址")
		os.Exit(1)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	pct := func(p float64) time.Duration {
		idx := int(float64(len(latencies)-1) * p)
		return latencies[idx]
	}

	fmt.Printf("目标      : %s\n", *url)
	fmt.Printf("并发/总数 : %d / %d\n", *conc, *total)
	fmt.Printf("总耗时    : %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("吞吐      : %.1f req/s\n", float64(len(latencies))/elapsed.Seconds())
	fmt.Printf("成功/失败 : %d / %d\n", len(latencies), atomic.LoadInt64(&errs))
	fmt.Printf("状态码    : %v\n", codes)
	fmt.Printf("延迟 p50  : %s\n", pct(0.50).Round(time.Microsecond))
	fmt.Printf("延迟 p95  : %s\n", pct(0.95).Round(time.Microsecond))
	fmt.Printf("延迟 p99  : %s\n", pct(0.99).Round(time.Microsecond))
	fmt.Printf("延迟 max  : %s\n", latencies[len(latencies)-1].Round(time.Microsecond))
}
