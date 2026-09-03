// Package testkit 为模块集成测试提供真实 PostgreSQL 夹具，
// 生产组合根不会 import 它。
package testkit

import (
	"database/sql"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

type Postgres struct {
	DSN     string
	server  *embeddedpostgres.EmbeddedPostgres
	dataDir string
}

const postgresStartAttempts = 3

// StartPostgres 拉起一个独立的 embedded PostgreSQL 并执行全部 goose 迁移。
// instance 只用来隔离各测试包的运行目录，避免并行解压时互相争抢。
func StartPostgres(instance, database string) (*Postgres, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("locate user directory: %w", err)
	}
	runtimeDir := filepath.Join(home, ".embedded-postgres-go", "eagle-"+instance)
	var startErr error
	for attempt := 1; attempt <= postgresStartAttempts; attempt++ {
		dataDir, err := os.MkdirTemp("", "eagle-"+instance+"-postgres-")
		if err != nil {
			return nil, fmt.Errorf("create PostgreSQL data directory: %w", err)
		}
		port, err := availablePort()
		if err != nil {
			_ = os.RemoveAll(dataDir)
			return nil, err
		}
		server := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Username("eagle").Password("eagle").Database(database).
			Port(port).RuntimePath(runtimeDir).DataPath(dataDir).Logger(io.Discard))
		if err := server.Start(); err != nil {
			_ = os.RemoveAll(dataDir)
			startErr = err
			continue
		}
		p := &Postgres{
			DSN:     fmt.Sprintf("postgres://eagle:eagle@127.0.0.1:%d/%s?sslmode=disable", port, database),
			server:  server,
			dataDir: dataDir,
		}
		if err := RunMigrations(p.DSN); err != nil {
			_ = p.Close()
			return nil, err
		}
		return p, nil
	}
	return nil, fmt.Errorf("start embedded postgres after %d attempts: %w", postgresStartAttempts, startErr)
}

func (p *Postgres) Close() error {
	var stopErr error
	if p.server != nil {
		stopErr = p.server.Stop()
	}
	removeErr := os.RemoveAll(p.dataDir)
	if stopErr != nil {
		return stopErr
	}
	return removeErr
}

// RunMigrations 对 dsn 执行仓库根目录 migrations/ 下的全部 goose 迁移。
func RunMigrations(dsn string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(db, MigrationsDir()); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// MigrationsDir 返回仓库根目录下的 migrations/ 绝对路径。
// 以本文件位置推算而不是 os.Getwd：go test 的工作目录是被测包目录，
// 不同包深度不同。
func MigrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
}

func availablePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate PostgreSQL test port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("release PostgreSQL test port: %w", err)
	}
	return uint32(port), nil
}
