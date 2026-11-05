// Package upgrade 节点程序的容器内自更新
//
// 中控滚动升级时经 RPC `upgrade` 下发目标版本, 节点从中控下载对应架构的二进制
// (中控代理 GitHub Release), 校验 sha256 并试运行后落到持久卷 data/bin/, 优雅停服后 exec 原地换程序。
//
// 容器重启后镜像入口仍是镜像自带程序, 由 Launch 决定是否转交给 data/bin 里的程序:
// 记录升级时的镜像程序构建时间 (image_build_time), 镜像没换就用自更新程序, 镜像换了 (容器重建)
// 以镜像为准并清掉旧的自更新程序。"最近一次操作生效", 回滚到旧版本也不会被镜像覆盖。
//
// 防崩溃循环: 新程序连上中控前算"未验证", Launch 每转交一次计数, 连续 maxAttempts 次都没验证
// (启动即崩溃) 就退回镜像程序并清掉 data/bin, 否则节点会永远起不来, 中控也够不着它。
package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	binDir      = "data/bin"
	binName     = "proxy-go"
	stateName   = "release.json"
	maxBinSize  = 200 << 20
	verifyLimit = 15 * time.Second
	maxAttempts = 3
)

// state data/bin/release.json
type state struct {
	Version        string `json:"version"`
	BuildTime      int64  `json:"build_time"`
	ImageBuildTime int64  `json:"image_build_time"` // 升级时镜像自带程序的构建时间
	Healthy        bool   `json:"healthy"`          // 新程序已成功连上过中控
	Attempts       int    `json:"attempts"`         // 未验证状态下被 Launch 转交的次数
}

var (
	busy       atomic.Bool
	restarting atomic.Bool
	shutdownMu sync.Mutex
	shutdownFn func()
)

func binPath() string   { return filepath.Join(binDir, binName) }
func statePath() string { return filepath.Join(binDir, stateName) }

// RunningFromData 当前进程是否就是 data/bin 里的自更新程序
func RunningFromData() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	exe, _ = filepath.EvalSymlinks(exe)
	want, err := filepath.Abs(binPath())
	if err != nil {
		return false
	}
	return exe == want
}

func readState() (*state, error) {
	b, err := os.ReadFile(statePath())
	if err != nil {
		return nil, err
	}
	var s state
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Launch 进程最早调用: 镜像程序发现同一镜像下有自更新程序时 exec 过去, 不返回
//
// buildTime 为 0 (本地开发构建) 或非 Linux 时什么都不做。
func Launch(buildTime int64) {
	if runtime.GOOS != "linux" || buildTime == 0 || RunningFromData() {
		return
	}
	st, err := readState()
	if err != nil {
		return
	}
	if !useDataBinary(st, buildTime) {
		log.Printf("[Upgrade] 镜像已更新 (构建于 %d, 自更新时为 %d), 使用镜像自带程序并清理 %s", buildTime, st.ImageBuildTime, binDir)
		_ = os.RemoveAll(binDir)
		return
	}
	if !st.Healthy {
		if st.Attempts >= maxAttempts {
			log.Printf("[Upgrade] 自更新程序 %s 连续 %d 次启动都没连上中控, 退回镜像程序并清理 %s", st.Version, st.Attempts, binDir)
			_ = os.RemoveAll(binDir)
			return
		}
		st.Attempts++
		if b, err := json.Marshal(st); err == nil {
			_ = writeAtomic(statePath(), b)
		}
	}
	target, err := filepath.Abs(binPath())
	if err != nil {
		return
	}
	log.Printf("[Upgrade] 转交给自更新程序 %s (%s)", target, st.Version)
	if err := syscall.Exec(target, append([]string{target}, os.Args[1:]...), os.Environ()); err != nil {
		log.Printf("[Upgrade] exec 自更新程序失败, 继续使用镜像程序: %v", err)
	}
}

// useDataBinary 镜像程序没换过 (与自更新时记录的一致) 才转交; 镜像换了说明容器被重建, 以镜像为准
func useDataBinary(st *state, imageBuildTime int64) bool {
	return st != nil && st.ImageBuildTime == imageBuildTime
}

// MarkHealthy 自更新程序首次连上中控后调用, 之后不再计入崩溃回退
func MarkHealthy() {
	if !RunningFromData() {
		return
	}
	st, err := readState()
	if err != nil || st.Healthy {
		return
	}
	st.Healthy = true
	st.Attempts = 0
	if b, err := json.Marshal(st); err == nil {
		_ = writeAtomic(statePath(), b)
	}
}

// Asset 某个架构的二进制
type Asset struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Request 中控下发的升级参数; 下载地址由节点按中控地址自行拼, 不信任下发的 URL
type Request struct {
	Version   string           `json:"version"`
	BuildTime int64            `json:"build_time"`
	Assets    map[string]Asset `json:"assets"`
}

// Apply 下载、校验、试运行并落盘新程序; 成功后调用方再触发 Restart
func Apply(ctx context.Context, req Request, baseURL, token string, ownBuildTime int64) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("self-upgrade only supported on linux")
	}
	if req.Version == "" || strings.ContainsAny(req.Version, "/\\ ") {
		return fmt.Errorf("invalid version %q", req.Version)
	}
	asset, ok := req.Assets[runtime.GOARCH]
	if !ok || len(asset.SHA256) != 64 {
		return fmt.Errorf("release %s has no binary for %s", req.Version, runtime.GOARCH)
	}
	if ownBuildTime == 0 && !RunningFromData() {
		// 镜像程序没注入构建时间, 容器重启后 Launch 无法判断镜像是否换过, 自更新会悄悄失效
		return fmt.Errorf("image binary has no build time, rebuild the container once before self-upgrade")
	}
	if !busy.CompareAndSwap(false, true) {
		return fmt.Errorf("another upgrade is in progress")
	}
	defer busy.Store(false)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	tmp := binPath() + ".download"
	defer os.Remove(tmp)
	url := fmt.Sprintf("%s/node/release/%s/%s", strings.TrimRight(baseURL, "/"), req.Version, runtime.GOARCH)
	if err := download(ctx, url, token, tmp, asset); err != nil {
		return err
	}
	if err := verifyRuns(ctx, tmp, req.Version); err != nil {
		return err
	}

	imageBuild := ownBuildTime
	if RunningFromData() {
		// 自更新程序再升级: 镜像没变, 沿用原记录
		if st, err := readState(); err == nil {
			imageBuild = st.ImageBuildTime
		}
	}
	if err := os.Rename(tmp, binPath()); err != nil {
		return err
	}
	b, _ := json.Marshal(state{Version: req.Version, BuildTime: req.BuildTime, ImageBuildTime: imageBuild, Attempts: 0})
	if err := writeAtomic(statePath(), b); err != nil {
		return err
	}
	log.Printf("[Upgrade] 新程序 %s 已就绪", req.Version)
	return nil
}

func download(ctx context.Context, url, token, dst string, asset Asset) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBinSize+1))
	if err := f.Close(); copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		return fmt.Errorf("download: %w", copyErr)
	}
	if n > maxBinSize || (asset.Size > 0 && n != asset.Size) {
		return fmt.Errorf("download: size %d, expected %d", n, asset.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, asset.SHA256) {
		return fmt.Errorf("sha256 mismatch: got %s", got)
	}
	return nil
}

// verifyRuns 试运行 `<bin> version`, 拦住架构不符 / 卷挂载了 noexec / 损坏的程序
func verifyRuns(ctx context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(ctx, verifyLimit)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return fmt.Errorf("new binary failed to run: %w", err)
	}
	if got := strings.TrimSpace(string(out)); !strings.HasPrefix(got, version) {
		return fmt.Errorf("new binary reports version %q, expected %q", got, version)
	}
	return nil
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SetShutdown 由 main 注册优雅停服流程 (落盘统计 / 停后台任务 / 关 HTTP 服务)
func SetShutdown(fn func()) {
	shutdownMu.Lock()
	shutdownFn = fn
	shutdownMu.Unlock()
}

// Restarting main 在 HTTP 服务退出后据此判断是否等待 exec, 不能直接返回 (会结束进程)
func Restarting() bool { return restarting.Load() }

// Restart 优雅停服后 exec data/bin 里的新程序; 成功则不返回
func Restart() error {
	target, err := filepath.Abs(binPath())
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err != nil {
		return err
	}
	if !restarting.CompareAndSwap(false, true) {
		return errors.New("restart already in progress")
	}
	shutdownMu.Lock()
	fn := shutdownFn
	shutdownMu.Unlock()
	log.Printf("[Upgrade] 停服并切换到 %s", target)
	if fn != nil {
		fn()
	}
	err = syscall.Exec(target, append([]string{target}, os.Args[1:]...), os.Environ())
	// 走到这里说明 exec 失败, 服务已停, 只能退出让容器重启策略拉起 (Launch 会再试一次新程序)
	log.Printf("[Upgrade] exec 失败, 退出进程: %v", err)
	os.Exit(1)
	return err
}
