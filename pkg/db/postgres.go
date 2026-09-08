package db

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	SQLStateUniqueViolation     = "23505"
	SQLStateForeignKeyViolation = "23503"
	MySQLUniqueViolation        = 1062
	MySQLForeignKeyRestricted   = 1451
	MySQLForeignKeyViolation    = 1452
)

// SQLState 返回 PostgreSQL 标准错误码，非 PostgreSQL 错误返回空串。
func SQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func IsUniqueViolation(err error) bool {
	return SQLState(err) == SQLStateUniqueViolation || MySQLErrorNumber(err) == MySQLUniqueViolation
}

func IsForeignKeyViolation(err error) bool {
	switch MySQLErrorNumber(err) {
	case MySQLForeignKeyRestricted, MySQLForeignKeyViolation:
		return true
	default:
		return SQLState(err) == SQLStateForeignKeyViolation
	}
}

// MySQLErrorNumber returns the server error number, or zero for other errors.
func MySQLErrorNumber(err error) uint16 {
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number
	}
	return 0
}

// HasDriverErrorCode distinguishes an unclassified Ent constraint wrapper from
// a driver error whose specific constraint kind is known not to be unique.
func HasDriverErrorCode(err error) bool {
	return SQLState(err) != "" || MySQLErrorNumber(err) != 0
}
