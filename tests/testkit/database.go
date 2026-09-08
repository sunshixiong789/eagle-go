package testkit

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pressly/goose/v3"

	"github.com/eagle-go/eagle/pkg/db"
)

// Database describes one isolated real database used by integration tests.
type Database struct {
	Driver    string
	SQLDriver string
	DSN       string
	close     func() error
}

func (d *Database) Close() error {
	if d == nil || d.close == nil {
		return nil
	}
	return d.close()
}

// StartDatabase starts embedded PostgreSQL by default. MySQL test runs use the
// externally provided server and create an isolated database for each package.
func StartDatabase(instance, database string) (*Database, error) {
	databaseDialect, err := db.ParseDialect(os.Getenv("EAGLE_TEST_DATABASE_DRIVER"))
	if err != nil {
		return nil, err
	}
	if databaseDialect == db.DialectMySQL {
		return StartMySQL(database)
	}
	pg, err := StartPostgres(instance, database)
	if err != nil {
		return nil, err
	}
	return &Database{
		Driver:    db.DialectPostgres.String(),
		SQLDriver: db.DialectPostgres.SQLDriver(),
		DSN:       pg.DSN,
		close:     pg.Close,
	}, nil
}

// RunMigrationsFor applies the migration set matching driver.
func RunMigrationsFor(driver, dsn string) error {
	databaseDialect, err := db.ParseDialect(driver)
	if err != nil {
		return err
	}
	sqlDB, err := sql.Open(databaseDialect.SQLDriver(), dsn)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := goose.SetDialect(databaseDialect.String()); err != nil {
		return fmt.Errorf("set migration dialect: %w", err)
	}
	goose.SetLogger(goose.NopLogger())
	if err := goose.Up(sqlDB, MigrationsDirFor(databaseDialect.String())); err != nil {
		return fmt.Errorf("run %s migrations: %w", databaseDialect, err)
	}
	return nil
}

func MigrationsDirFor(driver string) string {
	dir := MigrationsDir()
	if strings.EqualFold(driver, db.DialectMySQL.String()) {
		return filepath.Join(dir, "mysql")
	}
	return dir
}
