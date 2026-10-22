// 视觉稿本地服务：静态托管 prototypes/ 并把 /api 代理到后端 8080。
// 仅开发预览用，不属于交付物；正式前端为 Nuxt 3 工程。
package main

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	port     = "3210"
	upstream = "http://127.0.0.1:8080"
	root     = "prototypes"
)

func main() {
	target, err := url.Parse(upstream)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r.URL.Path)), "/")
		if p == "" {
			p = "index.html"
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		if st, err := os.Stat(full); err != nil || st.IsDir() {
			// 无扩展名的路径回退 index（视觉稿不需要，防御性处理）
			if !strings.Contains(p, ".") {
				full = filepath.Join(root, "index.html")
			} else {
				http.NotFound(w, r)
				return
			}
		}
		http.ServeFile(w, r, full)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		r.Host = target.Host
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		resp, err := http.DefaultTransport.RoundTrip(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})

	log.Printf("prototype server on http://localhost:%s (api -> %s)", port, upstream)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
