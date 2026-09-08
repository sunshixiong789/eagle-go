// Package database owns the service Ent client. Business repositories stay
// inside their modules.
package database

import (
	"context"
	"database/sql"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/eagle-go/eagle/internal/platform/database/ent"
	"github.com/eagle-go/eagle/pkg/db"
	"github.com/eagle-go/eagle/pkg/healthx"
	"github.com/eagle-go/eagle/pkg/platform/config"
)

type Database struct {
	client  *ent.Client
	sql     *sql.DB
	dialect db.Dialect
}

func Open(c *config.Data) (*Database, func(), error) {
	cfg, err := dbConfig(c)
	if err != nil {
		return nil, nil, err
	}
	sqlDB, cleanup, err := db.NewMonitored(context.Background(), cfg, healthx.Default)
	if err != nil {
		return nil, nil, err
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB(cfg.Dialect.String(), sqlDB)))
	return &Database{client: client, sql: sqlDB, dialect: cfg.Dialect}, cleanup, nil
}

func dbConfig(c *config.Data) (db.Config, error) {
	database := c.GetDatabase()
	databaseDialect, err := db.ParseDialect(database.GetDriver())
	if err != nil {
		return db.Config{}, fmt.Errorf("database configuration: %w", err)
	}
	return db.Config{
		Dialect:         databaseDialect,
		DSN:             database.GetDsn(),
		MaxConns:        database.GetMaxConns(),
		MaxIdleConns:    database.GetMaxIdleConns(),
		MaxConnLifetime: database.GetMaxConnLifetime().AsDuration(),
		MaxConnIdleTime: database.GetMaxConnIdleTime().AsDuration(),
	}, nil
}

func (d *Database) Client() *ent.Client { return d.client }
func (d *Database) SQL() *sql.DB        { return d.sql }
func (d *Database) Dialect() db.Dialect { return d.dialect }

func IsNotFound(err error) bool { return ent.IsNotFound(err) }

func IsUniqueViolation(err error) bool {
	return db.IsUniqueViolation(err) || ent.IsConstraintError(err) && !db.HasDriverErrorCode(err)
}

func IsForeignKeyViolation(err error) bool { return db.IsForeignKeyViolation(err) }
