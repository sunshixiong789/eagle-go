// eagle-admin 为运维提供一次性的管理员初始化入口，不开放匿名 HTTP 提权接口。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/eagle-go/eagle/internal/auth/application"
	"github.com/eagle-go/eagle/internal/auth/domain"
	"github.com/eagle-go/eagle/internal/auth/infrastructure"
	"github.com/eagle-go/eagle/internal/platform/config"
	platformdb "github.com/eagle-go/eagle/internal/platform/database"
)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, env func(string) string, out io.Writer) error {
	flags := flag.NewFlagSet("eagle-admin", flag.ContinueOnError)
	subject := flags.String("subject", "", "已登录过的 Eagle 账号 subject")
	actor := flags.String("actor", "", "执行初始化的运维人员标识，用于审计")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if _, err := domain.NewRoleAssignment(*subject, []string{"admin"}, 1, *actor); err != nil {
		return err
	}
	audience, dsn := env("EAGLE_AUTH_AUDIENCE"), env("EAGLE_DATABASE_DSN")
	if audience == "" || len(audience) > 255 || dsn == "" {
		return fmt.Errorf("EAGLE_AUTH_AUDIENCE and EAGLE_DATABASE_DSN are required")
	}
	db, cleanup, err := platformdb.Open(&config.Data{Database: &config.Data_Database{Driver: env("EAGLE_DATABASE_DRIVER"), Dsn: dsn, MaxConns: 2, MaxIdleConns: 1}})
	if err != nil {
		return err
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	uc := application.NewAccountUsecase(infrastructure.NewAccountRepository(db, audience))
	if err := uc.BootstrapAdmin(ctx, *subject, *actor); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "管理员已初始化；目标账号重新登录或刷新后获得 admin 角色。")
	return err
}
