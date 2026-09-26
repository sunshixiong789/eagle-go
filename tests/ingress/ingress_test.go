// Package ingress 验证真实策略就绪检查驱动 HAProxy 停止转发和恢复。
package ingress

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	accessinfra "github.com/eagle-go/eagle/internal/access/infrastructure"
	"github.com/eagle-go/eagle/internal/platform/config"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/healthx"
	"github.com/eagle-go/eagle/pkg/otelx"
	"github.com/eagle-go/eagle/tests/testkit"
)

func TestPolicyLagRemovesTrafficAndRecovers(t *testing.T) {
	binary := os.Getenv("HAPROXY_BIN")
	if testing.Short() || binary == "" {
		t.Skip("使用 HAPROXY_BIN=/path/to/haproxy make test-ingress 执行真实代理验收")
	}
	database, err := testkit.StartDatabase("ingress", "eagle_ingress_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	db, closeDB, err := platformdb.Open(&config.Data{Database: &config.Data_Database{Driver: database.Driver, Dsn: database.DSN, MaxConns: 4, MaxIdleConns: 1}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeDB)
	store := accessinfra.NewPolicyStore(db)
	enforcer, err := accessinfra.NewEnforcer(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(accessinfra.RegisterPolicyHealth(store, enforcer))
	healthx.Default.MarkInitialized()
	healthAddr := freeAddress(t)
	shutdown, err := otelx.Setup(context.Background(), otelx.Config{ServiceName: "ingress-acceptance", MetricsAddr: healthAddr})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	var requests atomic.Int64
	// 业务端口保持可用，确保 503 来自代理摘流而非应用自身拒绝请求。
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); _, _ = io.WriteString(w, "application") }))
	defer app.Close()
	host, port, err := net.SplitHostPort(strings.TrimPrefix(app.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	_, healthPort, err := net.SplitHostPort(healthAddr)
	if err != nil {
		t.Fatal(err)
	}
	proxyAddr := freeAddress(t)
	configPath, err := filepath.Abs("../../deploy/ingress/haproxy.cfg")
	if err != nil {
		t.Fatal(err)
	}
	logFile, err := os.CreateTemp(t.TempDir(), "haproxy-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logFile.Close() }()
	cmd := exec.Command(binary, "-db", "-f", configPath)
	cmd.Env = append(os.Environ(), "EAGLE_INGRESS_BIND="+proxyAddr, "EAGLE_UPSTREAM_HOST="+host, "EAGLE_UPSTREAM_PORT="+port, "EAGLE_HEALTH_PORT="+healthPort)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(logFile.Name())
			t.Log(string(data))
		}
	}()
	client := &http.Client{Timeout: 2 * time.Second}
	proxyURL := "http://" + proxyAddr
	awaitStatus(t, client, proxyURL, 200, 10*time.Second)
	if _, err := db.Client().PolicyState.UpdateOneID(1).AddVersion(1).Save(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 使用真实的 30 秒宽限期及连续失败阈值，不缩短生产时间参数。
	awaitStatus(t, client, "http://"+healthAddr+"/readyz", 503, 40*time.Second)
	awaitStatus(t, client, proxyURL, 503, 8*time.Second)
	before := requests.Load()
	for range 10 {
		if code := status(client, proxyURL); code != 503 {
			t.Fatalf("unhealthy proxy status = %d", code)
		}
	}
	if requests.Load() != before {
		t.Fatal("unhealthy backend still received traffic")
	}
	if status(client, app.URL) != 200 || status(client, "http://"+healthAddr+"/livez") != 200 {
		t.Fatal("app or liveness stopped during readiness failure")
	}
	if err := enforcer.ReloadPolicy(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitStatus(t, client, "http://"+healthAddr+"/readyz", 200, 5*time.Second)
	awaitStatus(t, client, proxyURL, 200, 8*time.Second)
	t.Log("策略落后超过 30 秒 -> readiness 503 -> HAProxy 停止转发 -> 重载策略后恢复 200")
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func status(client *http.Client, url string) int {
	response, err := client.Get(url)
	if err != nil {
		return 0
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func awaitStatus(t *testing.T, client *http.Client, url string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if status(client, url) == want {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s did not reach HTTP %d within %s", url, want, timeout)
}
