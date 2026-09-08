package db_test

import (
	"errors"
	"fmt"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eagle-go/eagle/pkg/db"
)

func TestSQLState(t *testing.T) {
	err := fmt.Errorf("insert product: %w", &pgconn.PgError{Code: db.SQLStateUniqueViolation})
	if got := db.SQLState(err); got != db.SQLStateUniqueViolation {
		t.Fatalf("SQLState() = %q", got)
	}
	if !db.IsUniqueViolation(err) {
		t.Fatal("wrapped unique violation was not recognized")
	}
	if db.IsForeignKeyViolation(err) {
		t.Fatal("unique violation recognized as foreign key violation")
	}
	if got := db.SQLState(errors.New("plain error")); got != "" {
		t.Fatalf("plain error SQLState() = %q", got)
	}
}

func TestMySQLErrorClassification(t *testing.T) {
	unique := fmt.Errorf("insert product: %w", &mysqldriver.MySQLError{Number: db.MySQLUniqueViolation})
	if !db.IsUniqueViolation(unique) || db.IsForeignKeyViolation(unique) {
		t.Fatal("mysql unique violation was classified incorrectly")
	}
	foreignKey := fmt.Errorf("insert child: %w", &mysqldriver.MySQLError{Number: db.MySQLForeignKeyViolation})
	if !db.IsForeignKeyViolation(foreignKey) || db.IsUniqueViolation(foreignKey) {
		t.Fatal("mysql foreign-key violation was classified incorrectly")
	}
	if got := db.MySQLErrorNumber(foreignKey); got != db.MySQLForeignKeyViolation {
		t.Fatalf("MySQLErrorNumber() = %d", got)
	}
}
