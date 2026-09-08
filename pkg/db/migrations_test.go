package db_test

import (
	"database/sql"
	"flag"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/eagle-go/eagle/tests/testkit"
)

// 迁移的 up 能跑通不代表 down 也能。回滚脚本平时没人执行，
// 等到线上真要回滚才发现写错，代价极高——所以在 CI 里就跑一遍往返。
func TestMigrationsRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	flag.Parse()

	testDatabase, err := testkit.StartDatabase("migrations", "eagle_migrate_test")
	if err != nil {
		t.Fatalf("启动测试数据库: %v", err)
	}
	t.Cleanup(func() { _ = testDatabase.Close() })

	sqlDB, err := sql.Open(testDatabase.SQLDriver, testDatabase.DSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := goose.SetDialect(testDatabase.Driver); err != nil {
		t.Fatalf("set dialect: %v", err)
	}
	goose.SetLogger(goose.NopLogger())

	dir := testkit.MigrationsDirFor(testDatabase.Driver)
	assertLatestSchema(t, sqlDB, testDatabase.Driver)
	assertSeedData(t, sqlDB, testDatabase.Driver)

	if err := goose.DownTo(sqlDB, dir, 0); err != nil {
		t.Fatalf("down-to 0: %v", err)
	}
	assertTablesDropped(t, sqlDB, testDatabase.Driver)

	// 再 up 一次：验证 down 确实把状态清干净了，
	// 而不是留下残留导致重建时主键冲突或对象已存在
	if err := goose.Up(sqlDB, dir); err != nil {
		t.Fatalf("回滚后重新 up: %v", err)
	}
	assertLatestSchema(t, sqlDB, testDatabase.Driver)
	assertSeedData(t, sqlDB, testDatabase.Driver)
	if err := goose.DownTo(sqlDB, dir, 0); err != nil {
		t.Fatalf("最终 down-to 0: %v", err)
	}
}

func assertLatestSchema(t *testing.T, db *sql.DB, driver string) {
	t.Helper()
	if version, err := goose.GetDBVersion(db); err != nil {
		t.Fatalf("读取迁移版本: %v", err)
	} else if version != 2 {
		t.Fatalf("迁移版本 = %d, want 2", version)
	}
	for _, table := range []string{"user_account", "user_identity", "user_role_binding", "auth_session"} {
		exists, err := tableExists(db, driver, table)
		if err != nil {
			t.Fatalf("检查表 %s: %v", table, err)
		}
		if !exists {
			t.Errorf("最新迁移缺少表 %s", table)
		}
	}
}

// 种子数据是权限体系的基线，缺了会导致鉴权全线失效。
//
// 这里断言的是不变量而不是行数：写死 count 的测试在任何人新增一个
// 权限节点时都会变红，除了制造噪音没有别的作用。
func assertSeedData(t *testing.T, db *sql.DB, driver string) {
	t.Helper()

	// Casbin 策略里必须有内置角色。角色由 Eagle 令牌提供，
	// 这里存的是「角色 -> 权限码」映射
	for _, role := range []string{"admin", "user"} {
		var exists bool
		err := db.QueryRow(bind(driver,
			`SELECT EXISTS(SELECT 1 FROM casbin_rule WHERE ptype = 'p' AND v0 = $1)`), role).Scan(&exists)
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
		"system:role:assign", "system:dict:list",
	} {
		var exists bool
		err := db.QueryRow(bind(driver, `SELECT EXISTS(SELECT 1 FROM permission_definition WHERE code = $1)`), code).Scan(&exists)
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
	if err := db.QueryRow(`SELECT max(id) FROM navigation_node`).Scan(&maxID); err != nil {
		t.Fatalf("读取权限最大 id: %v", err)
	}
	query := `SELECT nextval(pg_get_serial_sequence('navigation_node','id'))`
	if driver == "mysql" {
		query = `SELECT auto_increment FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'navigation_node'`
	}
	err = db.QueryRow(query).Scan(&nextID)
	if err != nil {
		t.Fatalf("读取权限序列: %v", err)
	}
	if nextID <= maxID {
		t.Errorf("权限序列 nextval = %d, 应大于已种入的最大 id(%d)", nextID, maxID)
	}
}

func assertTablesDropped(t *testing.T, db *sql.DB, driver string) {
	t.Helper()

	for _, table := range []string{
		"auth_session", "social_identity", "user_account", "user_identity", "user_role_binding",
		"navigation_node", "permission_definition", "permission_tree_state",
		"sys_dict_type", "sys_dict_data", "casbin_rule",
		"authz_policy_state", "authz_policy_audit",
	} {
		exists, err := tableExists(db, driver, table)
		if err != nil {
			t.Fatalf("检查表 %s: %v", table, err)
		}
		if exists {
			t.Errorf("回滚后表 %s 仍然存在", table)
		}
	}
}

func tableExists(db *sql.DB, driver, table string) (bool, error) {
	if driver == "mysql" {
		var exists bool
		err := db.QueryRow(`SELECT EXISTS(
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = DATABASE() AND table_name = ?)`, table).Scan(&exists)
		return exists, err
	}
	var exists bool
	err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists)
	return exists, err
}

func bind(driver, query string) string {
	if driver == "mysql" {
		return strings.ReplaceAll(query, "$1", "?")
	}
	return query
}
