// Command migrate is the deployment-time schema migration job shared by all
// services. The selected directory determines database ownership.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	dir := flag.String("dir", "", "base migration directory")
	driver := flag.String("driver", defaultDriver(), "database driver: postgres or mysql")
	dsn := flag.String("dsn", os.Getenv("EAGLE_DATABASE_DSN"), "database DSN")
	command := flag.String("command", "up", "goose command: up, down, status")
	flag.Parse()
	if *dir == "" || *dsn == "" {
		panic("migrate: -dir and -dsn/EAGLE_DATABASE_DSN are required")
	}
	dialect, sqlDriver, migrationDir, err := databaseSettings(*driver, *dir)
	if err != nil {
		panic(err)
	}
	db, err := sql.Open(sqlDriver, *dsn)
	if err != nil {
		panic(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.PingContext(context.Background()); err != nil {
		panic(err)
	}
	if err := goose.SetDialect(dialect); err != nil {
		panic(err)
	}
	switch *command {
	case "up":
		err = goose.Up(db, migrationDir)
	case "down":
		err = goose.Down(db, migrationDir)
	case "status":
		err = goose.Status(db, migrationDir)
	default:
		err = fmt.Errorf("unsupported command %q", *command)
	}
	if err != nil {
		panic(err)
	}
}

func defaultDriver() string {
	if driver := os.Getenv("EAGLE_DATABASE_DRIVER"); driver != "" {
		return driver
	}
	return "postgres"
}

func databaseSettings(driver, baseDir string) (dialect, sqlDriver, migrationDir string, err error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "postgres", "postgresql":
		return "postgres", "pgx", baseDir, nil
	case "mysql":
		return "mysql", "mysql", filepath.Join(baseDir, "mysql"), nil
	default:
		return "", "", "", fmt.Errorf("unsupported database driver %q", driver)
	}
}
