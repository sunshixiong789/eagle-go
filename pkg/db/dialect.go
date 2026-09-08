package db

import (
	"fmt"
	"net/url"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
)

// Dialect identifies the SQL database selected for this process.
type Dialect string

const (
	DialectPostgres Dialect = "postgres"
	DialectMySQL    Dialect = "mysql"
)

// ParseDialect accepts the configured database name. An empty value preserves
// compatibility with deployments created before the driver setting existed.
func ParseDialect(raw string) (Dialect, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(DialectPostgres), "postgresql":
		return DialectPostgres, nil
	case string(DialectMySQL):
		return DialectMySQL, nil
	default:
		return "", fmt.Errorf("unsupported database driver %q", raw)
	}
}

func (d Dialect) SQLDriver() string {
	if d == DialectMySQL {
		return "mysql"
	}
	return "pgx"
}

func (d Dialect) String() string { return string(d) }

// ValidateDSN rejects malformed connection strings before the process starts.
func ValidateDSN(dialect Dialect, dsn string) error {
	if dsn == "" {
		return fmt.Errorf("dsn is required")
	}
	switch dialect {
	case DialectPostgres:
		u, err := url.Parse(dsn)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			return fmt.Errorf("must be a postgres URL")
		}
	case DialectMySQL:
		cfg, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			return fmt.Errorf("invalid mysql DSN: %w", err)
		}
		if cfg.DBName == "" {
			return fmt.Errorf("mysql DSN must select a database")
		}
		if !cfg.ParseTime {
			return fmt.Errorf("mysql DSN must set parseTime=true")
		}
	default:
		return fmt.Errorf("unsupported database driver %q", dialect)
	}
	return nil
}
