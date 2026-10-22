package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriteText_CountsAndBuckets(t *testing.T) {
	r := New()
	r.Observe("GET", "/api/v1/articles", 200, 3*time.Millisecond)
	r.Observe("GET", "/api/v1/articles", 200, 80*time.Millisecond)
	r.Observe("GET", "/api/v1/articles", 500, 40*time.Millisecond)

	var buf bytes.Buffer
	r.WriteText(&buf)
	out := buf.String()

	want := []string{
		"blog_up 1",
		`blog_http_requests_total{method="GET",route="/api/v1/articles",status="200"} 2`,
		`blog_http_requests_total{method="GET",route="/api/v1/articles",status="500"} 1`,
		`blog_http_requests_by_route_total{method="GET",route="/api/v1/articles"} 3`,
		`blog_http_request_duration_seconds_bucket{method="GET",route="/api/v1/articles",le="0.005"} 1`,
		`blog_http_request_duration_seconds_bucket{method="GET",route="/api/v1/articles",le="0.1"} 3`,
		`blog_http_request_duration_seconds_count{method="GET",route="/api/v1/articles"} 3`,
		"blog_go_goroutines",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Fatalf("输出缺少 %q\n--- 实际 ---\n%s", w, out)
		}
	}
}

func TestWriteText_UnmatchedRoute(t *testing.T) {
	r := New()
	r.Observe("GET", "", 404, time.Millisecond)
	var buf bytes.Buffer
	r.WriteText(&buf)
	if !strings.Contains(buf.String(), `route="unmatched"`) {
		t.Fatalf("空路由应归到 unmatched：\n%s", buf.String())
	}
}

func TestInFlight(t *testing.T) {
	r := New()
	r.IncInFlight()
	r.IncInFlight()
	r.DecInFlight()
	var buf bytes.Buffer
	r.WriteText(&buf)
	if !strings.Contains(buf.String(), "blog_http_in_flight 1") {
		t.Fatalf("在途计数错误：\n%s", buf.String())
	}
}

// 并发写入不应丢计数（本机无 race 检测器，这里至少验证总数正确）。
func TestObserveConcurrent(t *testing.T) {
	r := New()
	const n = 200
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			r.Observe("POST", "/api/v1/auth/login", 200, time.Millisecond)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	var buf bytes.Buffer
	r.WriteText(&buf)
	if !strings.Contains(buf.String(), `blog_http_requests_by_route_total{method="POST",route="/api/v1/auth/login"} 200`) {
		t.Fatalf("并发计数不一致：\n%s", buf.String())
	}
}
