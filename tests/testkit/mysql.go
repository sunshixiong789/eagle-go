package testkit

import (
	"database/sql"
	"fmt"
	"os"
	"regexp"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/eagle-go/eagle/pkg/db"
)

var mysqlDatabaseName = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// StartMySQL creates an isolated schema on the MySQL server supplied by CI or
// the developer. The account in EAGLE_TEST_MYSQL_DSN needs CREATE/DROP DATABASE.
func StartMySQL(database string) (*Database, error) {
	if !mysqlDatabaseName.MatchString(database) {
		return nil, fmt.Errorf("invalid mysql test database name %q", database)
	}
	baseDSN := os.Getenv("EAGLE_TEST_MYSQL_DSN")
	if baseDSN == "" {
		return nil, fmt.Errorf("EAGLE_TEST_MYSQL_DSN is required for mysql integration tests")
	}
	cfg, err := mysqldriver.ParseDSN(baseDSN)
	if err != nil {
		return nil, fmt.Errorf("parse mysql test DSN: %w", err)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	admin, err := sql.Open(db.DialectMySQL.SQLDriver(), cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql admin connection: %w", err)
	}
	closeAdmin := true
	defer func() {
		if closeAdmin {
			_ = admin.Close()
		}
	}()
	if err := admin.Ping(); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	quoted := "`" + database + "`"
	if _, err := admin.Exec("DROP DATABASE IF EXISTS " + quoted); err != nil {
		return nil, fmt.Errorf("drop stale mysql test database: %w", err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + quoted + " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		return nil, fmt.Errorf("create mysql test database: %w", err)
	}
	cfg.DBName = database
	cfg.ParseTime = true
	testDSN := cfg.FormatDSN()
	if err := RunMigrationsFor(db.DialectMySQL.String(), testDSN); err != nil {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + quoted)
		return nil, err
	}
	closeAdmin = false
	return &Database{
		Driver:    db.DialectMySQL.String(),
		SQLDriver: db.DialectMySQL.SQLDriver(),
		DSN:       testDSN,
		close: func() error {
			_, dropErr := admin.Exec("DROP DATABASE IF EXISTS " + quoted)
			closeErr := admin.Close()
			if dropErr != nil {
				return dropErr
			}
			return closeErr
		},
	}, nil
}
