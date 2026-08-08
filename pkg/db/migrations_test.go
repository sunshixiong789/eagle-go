package db_test

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
)

// 用独立端口，避免与 data 包的集成测试实例冲突（两者可能并行执行）。
const migrationTestPort = 55434

// 迁移的 up 能跑通不代表 down 也能。回滚脚本平时没人执行，
// 等到线上真要回滚才发现写错，代价极高——所以在 CI 里就跑一遍往返。
func TestMigrationsRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("定位用户目录: %v", err)
	}
	// 独立运行目录，避免与其他测试包并行时争抢解压目录
	runtimeDir := filepath.Join(home, ".embedded-postgres-go", "eagle-migrations")

	pg := embeddedpostgres.NewDatabase(
		embeddedpostgres.DefaultConfig().
			Username("eagle").
			Password("eagle").
			Database("eagle_migrate_test").
			Port(migrationTestPort).
			RuntimePath(runtimeDir).
			DataPath(filepath.Join(runtimeDir, "data")).
			Logger(io.Discard),
	)
	if err := pg.Start(); err != nil {
		t.Fatalf("启动 embedded postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Stop() })

	dsn := fmt.Sprintf(
		"postgres://eagle:eagle@127.0.0.1:%d/eagle_migrate_test?sslmode=disable",
		migrationTestPort)

	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	goose.SetLogger(goose.NopLogger())

	dir, err := filepath.Abs(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("resolve dir: %v", err)
	}

	if err := goose.Up(sqlDB, dir); err != nil {
		t.Fatalf("首次 up: %v", err)
	}
	assertSeedData(t, sqlDB)

	if err := goose.DownTo(sqlDB, dir, 0); err != nil {
		t.Fatalf("down-to 0: %v", err)
	}
	assertTablesDropped(t, sqlDB)

	// 再 up 一次：验证 down 确实把状态清干净了，
	// 而不是留下残留导致重建时主键冲突或对象已存在
	if err := goose.Up(sqlDB, dir); err != nil {
		t.Fatalf("回滚后重新 up: %v", err)
	}
	assertSeedData(t, sqlDB)
}

// 种子数据是权限体系的基线，缺了会导致鉴权全线失效。
//
// 这里断言的是不变量而不是行数：写死 count 的测试在任何人新增一个
// 权限节点时都会变红，除了制造噪音没有别的作用。
func assertSeedData(t *testing.T, db *sql.DB) {
	t.Helper()

	// Casbin 策略里必须有内置角色。角色本身在 Keycloak，
	// 这里存的是「角色 -> 权限码」映射
	for _, role := range []string{"admin", "user"} {
		var exists bool
		err := db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM casbin_rule WHERE ptype = 'p' AND v0 = $1)`, role).Scan(&exists)
		if err != nil {
			t.Fatalf("查询角色策略 %s: %v", role, err)
		}
		if !exists {
			t.Errorf("角色 %q 的策略缺失", role)
		}
	}

	// 权限码是 proto 注解里引用的值，对不上就是全线 403
	for _, code := range []string{
		"system:permission:add", "system:permission:edit",
		"system:role:assign", "system:dict:query",
	} {
		var exists bool
		err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sys_permission WHERE code = $1)`, code).Scan(&exists)
		if err != nil {
			t.Fatalf("查询权限码 %s: %v", code, err)
		}
		if !exists {
			t.Errorf("权限码 %q 缺失，对应接口将无法授权", code)
		}
	}

	// 安全不变量：普通角色不得携带任何写权限。
	// 种子数据一旦写错，新用户默认就能删库。
	var writeGrants int
	err := db.QueryRow(`
		SELECT count(*) FROM casbin_rule
		WHERE ptype = 'p' AND v0 = 'user'
		  AND v1 NOT LIKE '%:query'
		  AND v1 NOT LIKE '%:list'`).Scan(&writeGrants)
	if err != nil {
		t.Fatalf("查询 user 角色的写权限: %v", err)
	}
	if writeGrants != 0 {
		t.Errorf("user 角色被授予了 %d 项非只读权限，种子数据有误", writeGrants)
	}

	// user 角色更不该拿到通配策略，那等于全量放行
	var wildcards int
	err = db.QueryRow(`
		SELECT count(*) FROM casbin_rule
		WHERE ptype = 'p' AND v0 = 'user' AND v1 LIKE '%*%'`).Scan(&wildcards)
	if err != nil {
		t.Fatalf("查询 user 角色的通配策略: %v", err)
	}
	if wildcards != 0 {
		t.Errorf("user 角色被授予了 %d 条通配策略，等同于全量放行", wildcards)
	}

	// 但它必须确实有只读权限，否则说明策略根本没种进去
	var readGrants int
	err = db.QueryRow(
		`SELECT count(*) FROM casbin_rule WHERE ptype = 'p' AND v0 = 'user'`).Scan(&readGrants)
	if err != nil {
		t.Fatalf("查询 user 角色的权限数: %v", err)
	}
	if readGrants == 0 {
		t.Error("user 角色没有任何权限策略")
	}

	// 字典项必须挂在已存在的字典类型下（外键之外再确认一次数据自洽）
	var orphanDictData int
	err = db.QueryRow(`
		SELECT count(*) FROM sys_dict_data d
		WHERE NOT EXISTS (SELECT 1 FROM sys_dict_type t WHERE t.type = d.dict_type)`).Scan(&orphanDictData)
	if err != nil {
		t.Fatalf("查询孤儿字典项: %v", err)
	}
	if orphanDictData != 0 {
		t.Errorf("存在 %d 条没有对应类型的字典项", orphanDictData)
	}

	// 显式指定过 id 的表，序列必须被推到最大值之后，
	// 否则后续 INSERT 会撞主键
	var nextID, maxID int64
	if err := db.QueryRow(`SELECT max(id) FROM sys_permission`).Scan(&maxID); err != nil {
		t.Fatalf("读取权限最大 id: %v", err)
	}
	err = db.QueryRow(`SELECT nextval(pg_get_serial_sequence('sys_permission','id'))`).Scan(&nextID)
	if err != nil {
		t.Fatalf("读取权限序列: %v", err)
	}
	if nextID <= maxID {
		t.Errorf("权限序列 nextval = %d, 应大于已种入的最大 id(%d)", nextID, maxID)
	}
}

func assertTablesDropped(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, table := range []string{
		"sys_user_profile", "sys_permission",
		"sys_dict_type", "sys_dict_data", "casbin_rule",
	} {
		var exists bool
		err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("检查表 %s: %v", table, err)
		}
		if exists {
			t.Errorf("回滚后表 %s 仍然存在", table)
		}
	}
}
