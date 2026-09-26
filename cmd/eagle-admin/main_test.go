package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/tests/testkit"
)

func TestBootstrapCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("需要真实数据库")
	}
	database, err := testkit.StartDatabase("admincmd", "eagle_admincmd_test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	db, err := sql.Open(database.SQLDriver, database.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO user_account (subject, display_name) VALUES ('bootstrap-account', 'Operator')`); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		return map[string]string{"EAGLE_AUTH_AUDIENCE": "admin-command", "EAGLE_DATABASE_DRIVER": database.Driver, "EAGLE_DATABASE_DSN": database.DSN}[k]
	}
	var out bytes.Buffer
	args := []string{"-subject", "bootstrap-account", "-actor", "test-operator"}
	if err := run(args, env, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("missing success output")
	}
	if err := run(args, env, &out); !errors.Is(err, domain.ErrAdminAlreadyInitialized) {
		t.Fatalf("repeat = %v", err)
	}
	var actor string
	if err := db.QueryRow(`SELECT actor_subject FROM account_role_audit WHERE action='admin.bootstrap'`).Scan(&actor); err != nil || actor != "test-operator" {
		t.Fatalf("audit actor %q: %v", actor, err)
	}
}

func TestBootstrapRejectsIncompleteInput(t *testing.T) {
	for _, args := range [][]string{nil, {"-subject", "x"}, {"-subject", "x", "-actor", "ops"}, {"-subject", "x", "-actor", "ops", "unexpected"}} {
		var out bytes.Buffer
		if err := run(args, func(string) string { return "" }, &out); err == nil || out.Len() != 0 {
			t.Fatalf("accepted incomplete input %v", args)
		}
	}
}
