package infrastructure

import (
	"flag"
	"fmt"
	"os"
	"testing"

	_ "github.com/eagle-go/eagle/api/eagle/dictionary/v1"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
	"github.com/eagle-go/eagle/pkg/platform/config"
	"github.com/eagle-go/eagle/tests/testkit"
)

var testDB *platformdb.Database

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	testDatabase, err := testkit.StartDatabase("access", "eagle_access_test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	db, cleanup, err := platformdb.Open(&config.Data{Database: &config.Data_Database{
		Driver: testDatabase.Driver, Dsn: testDatabase.DSN, MaxConns: 4, MaxIdleConns: 1,
	}})
	if err != nil {
		_ = testDatabase.Close()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testDB = db
	code := m.Run()
	cleanup()
	if err := testDatabase.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}
