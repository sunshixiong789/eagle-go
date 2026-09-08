package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"entgo.io/ent/schema/field"

	"github.com/eagle-go/eagle/internal/platform/database/ent/migrate"
	"github.com/eagle-go/eagle/tests/testkit"
)

// Compare generated Ent metadata with goose's real database schema. SQL-only
// foreign keys, CHECKs, defaults and extra indexes are intentional; their behavior
// is covered by repository tests. Required columns and indexes must agree.
func TestEntMatchesGoose(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	testDatabase, err := testkit.StartDatabase("schema-parity", "eagle_schema_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testDatabase.Close() })
	db, err := sql.Open(testDatabase.SQLDriver, testDatabase.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, table := range migrate.Tables {
		t.Run(table.Name, func(t *testing.T) {
			assertColumns(t, db, testDatabase.Driver, table)
			assertIndexes(t, db, testDatabase.Driver, table)
		})
	}
	assertExactColumnNames(t, db, testDatabase.Driver, "casbin_rule", []string{"id", "ptype", "v0", "v1"})
	assertExactColumnNames(t, db, testDatabase.Driver, "authz_policy_audit", []string{
		"id", "policy_version", "action", "target", "actor_subject",
		"request_id", "trace_id", "before", "after", "created_at",
	})
	assertSelfParentRejected(t, db)
	query := `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_name <> 'goose_db_version'`
	if testDatabase.Driver == "mysql" {
		query = `SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version'`
	}
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	owned := map[string]bool{}
	for _, table := range migrate.Tables {
		owned[table.Name] = true
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if !owned[name] {
			t.Errorf("table %s missing from Ent schema", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func assertSelfParentRejected(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `
		INSERT INTO navigation_node (id, parent_id, name, type)
		VALUES (900000000, 900000000, 'invalid self parent', 1)`)
	if err == nil {
		t.Fatal("database accepted a navigation node as its own parent")
	}
}

func assertExactColumnNames(t *testing.T, db *sql.DB, driver, table string, want []string) {
	t.Helper()
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = $1
		ORDER BY ordinal_position`
	if driver == "mysql" {
		query = `
			SELECT column_name
			FROM information_schema.columns
			WHERE table_schema = DATABASE() AND table_name = ?
			ORDER BY ordinal_position`
	}
	rows, err := db.QueryContext(context.Background(), query, table)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s columns = %v, want %v", table, got, want)
	}
}

func assertColumns(t *testing.T, db *sql.DB, driver string, table *schema.Table) {
	t.Helper()
	query := `SELECT column_name, data_type, COALESCE(character_maximum_length,0), is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1`
	if driver == "mysql" {
		query = `SELECT column_name, data_type, COALESCE(character_maximum_length,0), is_nullable FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ?`
	}
	rows, err := db.QueryContext(context.Background(), query, table.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	type column struct {
		typ      string
		size     int64
		nullable bool
	}
	actual := map[string]column{}
	for rows.Next() {
		var name, typ, nullable string
		var size int64
		if err := rows.Scan(&name, &typ, &size, &nullable); err != nil {
			t.Fatal(err)
		}
		actual[name] = column{typ, size, nullable == "YES"}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(table.Columns) {
		t.Errorf("column count: goose=%d ent=%d", len(actual), len(table.Columns))
	}
	types := map[field.Type]string{field.TypeInt64: "bigint", field.TypeInt32: "integer", field.TypeBool: "boolean", field.TypeTime: "timestamp with time zone", field.TypeJSON: "jsonb"}
	if driver == "mysql" {
		types = map[field.Type]string{field.TypeInt64: "bigint", field.TypeInt32: "int", field.TypeBool: "tinyint", field.TypeTime: "datetime", field.TypeJSON: "json"}
	}
	for _, col := range table.Columns {
		wantType := types[col.Type]
		if col.Type == field.TypeString {
			wantType = "text"
			if col.Size > 0 {
				wantType = "character varying"
				if driver == "mysql" {
					wantType = "varchar"
				}
			}
		}
		if wantType == "" {
			t.Fatalf("add explicit %s mapping for %s.%s type %v", driver, table.Name, col.Name, col.Type)
		}
		got, ok := actual[col.Name]
		if !ok || got.typ != wantType || got.nullable != col.Nullable || (col.Type == field.TypeString && col.Size > 0 && got.size != col.Size) {
			t.Errorf("%s: goose=%+v ent=(%s,size=%d,nullable=%t)", col.Name, got, wantType, col.Size, col.Nullable)
		}
	}
}

func assertIndexes(t *testing.T, db *sql.DB, driver string, table *schema.Table) {
	t.Helper()
	query := `SELECT i.indisunique, i.indisprimary, string_agg(a.attname, ',' ORDER BY k.ordinality)
 FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum, ordinality)
 JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum=k.attnum
 WHERE n.nspname='public' AND c.relname=$1 AND i.indpred IS NULL AND i.indexprs IS NULL AND i.indisvalid AND k.ordinality <= i.indnkeyatts
 GROUP BY i.indexrelid,i.indisunique,i.indisprimary`
	if driver == "mysql" {
		query = `SELECT non_unique = 0, index_name = 'PRIMARY', GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR ',')
			FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ?
			GROUP BY index_name, non_unique`
	}
	rows, err := db.QueryContext(context.Background(), query, table.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	actual := map[string]bool{}
	for rows.Next() {
		var unique, primary bool
		var cols string
		if err := rows.Scan(&unique, &primary, &cols); err != nil {
			t.Fatal(err)
		}
		actual[fmt.Sprintf("%t/%t/%s", unique, primary, cols)] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	check := func(unique, primary bool, columns []*schema.Column) {
		names := make([]string, len(columns))
		for i, col := range columns {
			names[i] = col.Name
		}
		key := fmt.Sprintf("%t/%t/%s", unique, primary, strings.Join(names, ","))
		if !actual[key] {
			t.Errorf("missing index unique/primary/columns = %s", key)
		}
	}
	check(true, true, table.PrimaryKey)
	for _, col := range table.Columns {
		if col.Unique {
			check(true, false, []*schema.Column{col})
		}
	}
	for _, idx := range table.Indexes {
		check(idx.Unique, false, idx.Columns)
	}
}
