package main

import (
	"path/filepath"
	"testing"
)

func TestDatabaseSettings(t *testing.T) {
	tests := []struct {
		driver, dialect, sqlDriver, dir string
	}{
		{"postgres", "postgres", "pgx", "migrations"},
		{"postgresql", "postgres", "pgx", "migrations"},
		{"mysql", "mysql", "mysql", filepath.Join("migrations", "mysql")},
	}
	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			dialect, sqlDriver, dir, err := databaseSettings(tt.driver, "migrations")
			if err != nil {
				t.Fatal(err)
			}
			if dialect != tt.dialect {
				t.Fatalf("dialect = %q, want %q", dialect, tt.dialect)
			}
			if sqlDriver != tt.sqlDriver || dir != tt.dir {
				t.Fatalf("settings = %q, %q", sqlDriver, dir)
			}
		})
	}
	if _, _, _, err := databaseSettings("sqlite", "migrations"); err == nil {
		t.Fatal("unsupported driver was accepted")
	}
}
