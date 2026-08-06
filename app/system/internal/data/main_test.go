package data

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/eagle-go/eagle/app/system/internal/conf"
)

// 集成测试跑在真实 PostgreSQL 上，但不依赖 Docker：
// embedded-postgres 会下载官方 PG 二进制并在本进程内拉起一个实例。
// 团队成员（尤其 Windows 机器）不必装 Docker 就能跑通全部数据层测试。
//
// Redis 侧用 miniredis——纯 Go 实现，进程内启动，无需外部依赖。
//
// 首次运行会下载 PG 二进制（约 100MB），之后从本地缓存启动，几秒即可。
// 用 `go test -short ./...` 可跳过这些测试。

const testPGPort = 55433

var testData *Data

func TestMain(m *testing.M) {
	// testing.Short() 依赖已解析的测试 flag，TestMain 里必须先手动 Parse
	flag.Parse()

	// -short 时不拉起任何外部依赖，直接跑（各用例自行 Skip）
	if testing.Short() {
		os.Exit(m.Run())
	}

	code, err := runWithFixtures(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "集成测试环境准备失败: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runWithFixtures(m *testing.M) (int, error) {
	pg := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Username("eagle").
			Password("eagle").
			Database("eagle_test").
			Port(testPGPort).
			// 默认会把日志打到 stdout，淹没测试输出
			Logger(io.Discard),
	)
	if err := pg.Start(); err != nil {
		return 0, fmt.Errorf("启动 embedded postgres: %w", err)
	}
	defer func() { _ = pg.Stop() }()

	dsn := fmt.Sprintf(
		"postgres://eagle:eagle@127.0.0.1:%d/eagle_test?sslmode=disable", testPGPort)

	if err := runMigrations(dsn); err != nil {
		return 0, fmt.Errorf("执行迁移: %w", err)
	}

	mr, err := miniredis.Run()
	if err != nil {
		return 0, fmt.Errorf("启动 miniredis: %w", err)
	}
	defer mr.Close()

	d, cleanup, err := newTestData(dsn, mr.Addr())
	if err != nil {
		return 0, fmt.Errorf("构造 Data: %w", err)
	}
	defer cleanup()

	testData = d
	return m.Run(), nil
}

// runMigrations 跑的是 db/migrations 下的真实迁移文件，
// 而不是让 ent 自动建表——后者会让迁移脚本本身失去验证，
// 且生产用的是 goose，测试却走另一条路径就失去了意义。
func runMigrations(dsn string) error {
	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	goose.SetLogger(goose.NopLogger())

	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "db", "migrations"))
	if err != nil {
		return fmt.Errorf("resolve migrations dir: %w", err)
	}
	if err := goose.Up(sqlDB, dir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

func newTestData(dsn, redisAddr string) (*Data, func(), error) {
	return NewData(
		&conf.Data{
			Database: &conf.Data_Database{Dsn: dsn, MaxConns: 4, MinConns: 1},
			Redis:    &conf.Data_Redis{Addr: redisAddr},
		},
		&conf.Auth{
			DictCacheTtl: durationpb.New(time.Minute),
		},
	)
}

// flushCache 清空 Redis，保持用例之间互不影响。
func flushCache(t *testing.T) {
	t.Helper()
	if err := testData.rdb.FlushAll(context.Background()).Err(); err != nil {
		t.Fatalf("清空缓存: %v", err)
	}
}

// redisClient 供需要直接断言缓存状态的用例使用。
func redisClient() *redis.Client { return testData.rdb }
