package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// adminStaticRoot Next.js export 的产物根目录
const adminStaticRoot = "web/out"

// serveAdminStatic 托管 Next.js 静态导出 (output: 'export' + basePath: '/admin' + trailingSlash: true) 的产物
//
// 关键约束:
//
//  1. 安全: 用 filepath.Clean + 前缀校验阻止 ../ 越界
//  2. 路径分类: "资源" (含扩展名) 走文件回源, 缺失即 404; "路由" (无扩展名) 走 index.html 回源, 缺失返回 404.html
//  3. 目录请求 → 重写到 <dir>/index.html, 不依赖 http.ServeFile 的目录默认行为, 避免对 trailingSlash 模式产生 301 重定向风暴
//  4. RSC payload (Next 客户端导航预取的 *.txt): 显式 Content-Type 为 text/x-component, 否则 Next 解析失败会触发硬跳转把用户带到 .txt URL
//  5. 不存在的路由不再 fallback 到 root index.html (会让 SPA router 看到错误 URL, 再次触发 RSC fetch 死循环), 而是返回 404.html
func serveAdminStatic(w http.ResponseWriter, r *http.Request) {
	// 剥掉 /admin 前缀, 余下的相对路径用于映射到 web/out 下
	rel := strings.TrimPrefix(r.URL.Path, "/admin")
	if rel == "" {
		rel = "/"
	}

	// 安全: 用 filepath.Join 自带 Clean 把 .. 吃掉, 再校验最终路径仍在静态根目录下
	target := filepath.Join(adminStaticRoot, rel)
	absRoot, _ := filepath.Abs(adminStaticRoot)
	absTarget, _ := filepath.Abs(target)
	if absRoot == "" || absTarget == "" || !strings.HasPrefix(absTarget, absRoot) {
		http.NotFound(w, r)
		return
	}

	info, err := os.Stat(target)
	switch {
	case err == nil && info.IsDir():
		// 目录 → 取该目录下的 index.html
		target = filepath.Join(target, "index.html")
		if _, err := os.Stat(target); err != nil {
			serveAdminNotFound(w, r)
			return
		}
	case err == nil:
		// 文件存在, 后续按扩展名设置头部
	default:
		// 文件不存在: 资源类按 404 处理, 路由类 Next 已为存在的路由生成 <route>/index.html (上面 IsDir 已处理),
		// 走到这里说明请求的路径既不是资源也不是已生成的路由 → 一律 404
		serveAdminNotFound(w, r)
		return
	}

	// Next.js RSC 客户端导航预取的 payload, Content-Type 必须是 text/x-component;
	// 否则 Next 解析失败会回退到硬跳转, 把用户带到 ".../index.txt" URL
	if strings.HasSuffix(target, string(filepath.Separator)+"index.txt") || strings.HasSuffix(target, "/index.txt") {
		w.Header().Set("Content-Type", "text/x-component; charset=utf-8")
	}

	// 静态资源 (含 hash) 用强缓存; HTML / index.txt 用 no-cache 防止逻辑变更不生效
	applyAdminCacheHeaders(w, target)

	http.ServeFile(w, r, target)
}

// serveAdminNotFound 优先返回 Next 的 404.html (有完整布局), 缺失再退化为纯 404
func serveAdminNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	notFound := filepath.Join(adminStaticRoot, "404.html")
	if _, err := os.Stat(notFound); err == nil {
		w.WriteHeader(http.StatusNotFound)
		http.ServeFile(w, r, notFound)
		return
	}
	http.Error(w, "Not found", http.StatusNotFound)
}

// applyAdminCacheHeaders 按文件类型设置缓存头, 资源走强缓存, HTML / RSC 走 no-cache
func applyAdminCacheHeaders(w http.ResponseWriter, target string) {
	// _next/static 下的资源都带 hash, 永久强缓存
	if strings.Contains(target, string(filepath.Separator)+"_next"+string(filepath.Separator)+"static"+string(filepath.Separator)) ||
		strings.Contains(target, "/_next/static/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	// HTML / RSC payload 必须每次都拿最新, 否则发版后老用户看到混合状态
	if strings.HasSuffix(target, ".html") || strings.HasSuffix(target, ".txt") {
		w.Header().Set("Cache-Control", "no-cache")
		return
	}
	// 其他静态资源 (favicon, svg) 给短缓存
	w.Header().Set("Cache-Control", "public, max-age=3600")
}
