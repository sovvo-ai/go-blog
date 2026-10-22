// Package metrics 提供进程内的轻量指标采集与 Prometheus 文本导出。
//
// 为什么不用 prometheus/client_golang：V1.0 只需要「请求量 / 延迟 / 在途」三类指标，
// 引入官方 SDK 会带来一条独立的依赖链与注册表生命周期管理，
// 而这里 150 行就够用，且输出格式与 Prometheus 文本协议一致。
//
// 基数控制：标签只用「方法 + 路由模板（gin FullPath）+ 状态码」，
// 绝不用原始 URL，避免 /article/xxx 这类路径把指标基数打爆。
package metrics

import (
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// 延迟直方图分桶（秒）。覆盖 5ms–5s，超过按 +Inf 计。
var buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

type routeKey struct {
	method string
	route  string
}

type routeStat struct {
	mu       sync.Mutex
	counts   map[int]int64 // status -> count
	total    int64
	duration []int64 // 每个桶的累计计数（含 +Inf 位于末尾）
	sumNs    int64
}

func newRouteStat() *routeStat {
	return &routeStat{counts: make(map[int]int64), duration: make([]int64, len(buckets)+1)}
}

// Registry 是指标注册表。并发安全。
type Registry struct {
	mu       sync.RWMutex
	stats    map[routeKey]*routeStat
	inFlight int64
	started  time.Time
}

// New 构造 Registry。
func New() *Registry {
	return &Registry{stats: make(map[routeKey]*routeStat), started: time.Now()}
}

// IncInFlight 在请求进入时调用。
func (r *Registry) IncInFlight() { atomic.AddInt64(&r.inFlight, 1) }

// DecInFlight 在请求结束时调用。
func (r *Registry) DecInFlight() { atomic.AddInt64(&r.inFlight, -1) }

// Observe 记录一次请求。route 应传路由模板（gin 的 FullPath），不是实际路径。
func (r *Registry) Observe(method, route string, status int, d time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	key := routeKey{method: method, route: route}

	r.mu.RLock()
	st, ok := r.stats[key]
	r.mu.RUnlock()
	if !ok {
		r.mu.Lock()
		if st, ok = r.stats[key]; !ok {
			st = newRouteStat()
			r.stats[key] = st
		}
		r.mu.Unlock()
	}

	secs := d.Seconds()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.counts[status]++
	st.total++
	st.sumNs += d.Nanoseconds()
	for i, b := range buckets {
		if secs <= b {
			st.duration[i]++
		}
	}
	st.duration[len(buckets)]++ // +Inf
}

// WriteText 按 Prometheus 文本格式（0.0.4）输出。
func (r *Registry) WriteText(w io.Writer) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	r.mu.RLock()
	keys := make([]routeKey, 0, len(r.stats))
	for k := range r.stats {
		keys = append(keys, k)
	}
	r.mu.RUnlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].method < keys[j].method
	})

	fmt.Fprintf(w, "# HELP blog_up 进程存活（恒为 1）\n# TYPE blog_up gauge\nblog_up 1\n")
	fmt.Fprintf(w, "# HELP blog_uptime_seconds 进程启动至今秒数\n# TYPE blog_uptime_seconds gauge\nblog_uptime_seconds %.0f\n", time.Since(r.started).Seconds())
	fmt.Fprintf(w, "# HELP blog_http_in_flight 当前处理中的请求数\n# TYPE blog_http_in_flight gauge\nblog_http_in_flight %d\n", atomic.LoadInt64(&r.inFlight))

	fmt.Fprintf(w, "# HELP blog_http_requests_total HTTP 请求总数\n# TYPE blog_http_requests_total counter\n")
	// 同时输出按路由聚合的总数，方便直接算 QPS 与错误率
	fmt.Fprintf(w, "# HELP blog_http_requests_by_route_total 按路由聚合的请求数\n# TYPE blog_http_requests_by_route_total counter\n")
	for _, k := range keys {
		st := r.stats[k]
		st.mu.Lock()
		statuses := make([]int, 0, len(st.counts))
		for s := range st.counts {
			statuses = append(statuses, s)
		}
		sort.Ints(statuses)
		for _, s := range statuses {
			fmt.Fprintf(w, "blog_http_requests_total{method=%q,route=%q,status=\"%d\"} %d\n",
				k.method, k.route, s, st.counts[s])
		}
		fmt.Fprintf(w, "blog_http_requests_by_route_total{method=%q,route=%q} %d\n", k.method, k.route, st.total)
		st.mu.Unlock()
	}

	fmt.Fprintf(w, "# HELP blog_http_request_duration_seconds HTTP 请求耗时\n# TYPE blog_http_request_duration_seconds histogram\n")
	for _, k := range keys {
		st := r.stats[k]
		st.mu.Lock()
		for i, b := range buckets {
			fmt.Fprintf(w, "blog_http_request_duration_seconds_bucket{method=%q,route=%q,le=%q} %d\n",
				k.method, k.route, strconv.FormatFloat(b, 'g', -1, 64), st.duration[i])
		}
		fmt.Fprintf(w, "blog_http_request_duration_seconds_bucket{method=%q,route=%q,le=\"+Inf\"} %d\n",
			k.method, k.route, st.duration[len(buckets)])
		fmt.Fprintf(w, "blog_http_request_duration_seconds_sum{method=%q,route=%q} %.6f\n",
			k.method, k.route, float64(st.sumNs)/1e9)
		fmt.Fprintf(w, "blog_http_request_duration_seconds_count{method=%q,route=%q} %d\n",
			k.method, k.route, st.total)
		st.mu.Unlock()
	}

	fmt.Fprintf(w, "# HELP blog_go_goroutines 协程数\n# TYPE blog_go_goroutines gauge\nblog_go_goroutines %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "# HELP blog_go_heap_alloc_bytes 堆内存占用\n# TYPE blog_go_heap_alloc_bytes gauge\nblog_go_heap_alloc_bytes %d\n", mem.HeapAlloc)
	fmt.Fprintf(w, "# HELP blog_go_gc_cycles GC 次数\n# TYPE blog_go_gc_cycles counter\nblog_go_gc_cycles %d\n", mem.NumGC)
}
