package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUseDataBinary(t *testing.T) {
	cases := []struct {
		name  string
		st    *state
		image int64
		want  bool
	}{
		{"no state", nil, 100, false},
		{"same image, newer self-upgrade", &state{Version: "v2", BuildTime: 200, ImageBuildTime: 100}, 100, true},
		{"same image, rolled back to older build", &state{Version: "v0", BuildTime: 50, ImageBuildTime: 100}, 100, true},
		{"container rebuilt with new image", &state{Version: "v2", BuildTime: 200, ImageBuildTime: 100}, 300, false},
		{"container rebuilt with older image", &state{Version: "v2", BuildTime: 200, ImageBuildTime: 100}, 90, false},
	}
	for _, c := range cases {
		if got := useDataBinary(c.st, c.image); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

// buildFake 编出一个只会打印版本号的程序, 充当"新程序"
func buildFake(t *testing.T, dir, version string) string {
	t.Helper()
	src := filepath.Join(dir, "main.go")
	code := "package main\nimport (\"fmt\"; \"os\")\nfunc main() { if len(os.Args) > 1 && os.Args[1] == \"version\" { fmt.Println(\"" + version + "\") } }\n"
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "fake")
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake: %v\n%s", err, b)
	}
	return out
}

func TestApply(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("self-upgrade is linux only")
	}
	work := t.TempDir()
	bin := buildFake(t, t.TempDir(), "v2")
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)

	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	wd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	req := Request{Version: "v2", BuildTime: 200, Assets: map[string]Asset{runtime.GOARCH: {SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}}}
	if err := Apply(context.Background(), req, srv.URL, "tok", 100); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if gotAuth != "Bearer tok" || gotPath != "/node/release/v2/"+runtime.GOARCH {
		t.Fatalf("unexpected request auth=%q path=%q", gotAuth, gotPath)
	}
	var st state
	b, err := os.ReadFile(statePath())
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(b, &st)
	if st.Version != "v2" || st.BuildTime != 200 || st.ImageBuildTime != 100 {
		t.Fatalf("state = %+v", st)
	}
	if out, err := exec.Command(binPath(), "version").Output(); err != nil || string(out) != "v2\n" {
		t.Fatalf("installed binary: %q %v", out, err)
	}

	// sha 不符: 拒绝且不覆盖已装好的程序
	bad := req
	bad.Version = "v3"
	bad.Assets = map[string]Asset{runtime.GOARCH: {SHA256: "0000000000000000000000000000000000000000000000000000000000000000", Size: int64(len(body))}}
	if err := Apply(context.Background(), bad, srv.URL, "tok", 100); err == nil {
		t.Fatal("expected sha mismatch")
	}
	// 版本号不符 (下发 v3, 程序自报 v2): 试运行拦下
	wrong := req
	wrong.Version = "v3"
	if err := Apply(context.Background(), wrong, srv.URL, "tok", 100); err == nil {
		t.Fatal("expected version mismatch")
	}
	if b, _ := os.ReadFile(statePath()); !json.Valid(b) {
		t.Fatal("state corrupted")
	}
	if _, err := os.Stat(binPath() + ".download"); !os.IsNotExist(err) {
		t.Fatal("leftover download file")
	}
}
