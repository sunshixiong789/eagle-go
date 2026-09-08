package db_test

import (
	"strings"
	"testing"

	"github.com/eagle-go/eagle/pkg/db"
)

func TestParseDialect(t *testing.T) {
	tests := []struct {
		input string
		want  db.Dialect
	}{
		{"", db.DialectPostgres},
		{"postgres", db.DialectPostgres},
		{"postgresql", db.DialectPostgres},
		{"MYSQL", db.DialectMySQL},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := db.ParseDialect(tt.input)
			if err != nil || got != tt.want {
				t.Fatalf("ParseDialect(%q) = %q, %v", tt.input, got, err)
			}
		})
	}
	if _, err := db.ParseDialect("sqlite"); err == nil {
		t.Fatal("unsupported dialect was accepted")
	}
}

func TestValidateDSN(t *testing.T) {
	tests := []struct {
		name    string
		dialect db.Dialect
		dsn     string
		wantErr string
	}{
		{"postgres", db.DialectPostgres, "postgres://eagle:eagle@localhost/eagle", ""},
		{"postgres wrong scheme", db.DialectPostgres, "mysql://localhost/eagle", "postgres URL"},
		{"mysql", db.DialectMySQL, "eagle:eagle@tcp(localhost:3306)/eagle?parseTime=true", ""},
		{"mysql database required", db.DialectMySQL, "eagle:eagle@tcp(localhost:3306)/?parseTime=true", "select a database"},
		{"mysql parse time required", db.DialectMySQL, "eagle:eagle@tcp(localhost:3306)/eagle", "parseTime=true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := db.ValidateDSN(tt.dialect, tt.dsn)
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
