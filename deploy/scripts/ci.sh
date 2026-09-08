#!/bin/sh
# 云效质量门禁；all 为发布前完整检查，各子命令可拆成独立任务。
set -eu
set +x
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "${script_dir}/../.." && pwd)
cd "${repo_dir}"

check() {
  : "${EAGLE_CI_BASE_REF:?set the fetched target branch, e.g. origin/master, or a verified base commit}"
  base=$(git merge-base HEAD "${EAGLE_CI_BASE_REF}")
  if [ "${base}" = "$(git rev-parse HEAD)" ]; then
    echo 'ci: the base equals HEAD; set EAGLE_CI_BASE_REF to the pre-merge verified commit' >&2
    exit 1
  fi
  sh tools/scripts/check-migrations.sh "${base}"
  make "${repo_dir}/bin/buf"
  ./bin/buf lint
  ./bin/buf breaking api --against ".git#ref=${base},subdir=api"
  make api config ent
  if [ -n "$(git status --porcelain --untracked-files=all -- api pkg/platform/config internal/platform/database/ent openapi.yaml go.mod go.sum tools/go.mod tools/go.sum)" ]; then
    echo 'ci: generated code differs; run make generate and commit the results' >&2
    exit 1
  fi
  make build
  go vet ./...
  go -C tools vet ./...
  make lint validate-deploy test-deploy
}

test_go() {
  if [ "$(id -u)" = 0 ]; then
    echo 'ci: PostgreSQL initdb cannot run as root; run this job as a non-root build user (see docs/aliyun-flow-deployment.md)' >&2
    exit 1
  fi
  # PostgreSQL 测试用 embedded-postgres，不使用部署环境的 DSN。
  unset EAGLE_TEST_DATABASE_DRIVER EAGLE_TEST_MYSQL_DSN
  mkdir -p dist/coverage
  make test-coverage
  go test -race -coverprofile=dist/coverage/eagle.out ./...
  go -C tools test ./...
}

test_database() (
  driver=$1
  docker info >/dev/null
  make "${repo_dir}/bin/goose"
  container=
  trap 'if [ -n "${container}" ]; then docker rm -f "${container}" >/dev/null; fi' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  if [ "${driver}" = mysql ]; then
    container=$(docker run -d --publish 127.0.0.1::3306 \
      --env MYSQL_ROOT_PASSWORD=eagle-ci --env MYSQL_ROOT_HOST=% --env MYSQL_DATABASE=eagle_ci \
      --health-cmd='mysqladmin ping -h 127.0.0.1 -peagle-ci' \
      --health-interval=2s --health-timeout=3s --health-retries=60 mysql:8.4)
    container_port=3306
    migration_dir=migrations/mysql
  else
    container=$(docker run -d --publish 127.0.0.1::5432 \
      --env POSTGRES_USER=eagle --env POSTGRES_PASSWORD=eagle-ci --env POSTGRES_DB=eagle_ci \
      --health-cmd='pg_isready -h 127.0.0.1 -U eagle -d eagle_ci' \
      --health-interval=2s --health-timeout=3s --health-retries=60 postgres:17-alpine)
    container_port=5432
    migration_dir=migrations
  fi
  attempts=0
  while [ "$(docker inspect --format '{{.State.Health.Status}}' "${container}")" != healthy ]; do
    attempts=$((attempts + 1))
    if [ "${attempts}" -ge 90 ]; then echo "ci: ${driver} did not become healthy" >&2; exit 1; fi
    sleep 2
  done
  port=$(docker port "${container}" "${container_port}/tcp")
  port=${port##*:}
  if [ "${driver}" = mysql ]; then
    dsn="root:eagle-ci@tcp(127.0.0.1:${port})/eagle_ci?parseTime=true&loc=UTC&charset=utf8mb4"
    EAGLE_TEST_MYSQL_DSN="${dsn}" make test-mysql
  else
    dsn="postgres://eagle:eagle-ci@127.0.0.1:${port}/eagle_ci?sslmode=disable"
  fi
  ./bin/goose -dir "${migration_dir}" "${driver}" "${dsn}" up
  ./bin/goose -dir "${migration_dir}" "${driver}" "${dsn}" down-to 0
  ./bin/goose -dir "${migration_dir}" "${driver}" "${dsn}" up
)

case "${1:-all}" in
  all) check; test_go; test_database postgres; test_database mysql ;;
  check) check ;;
  test) test_go ;;
  postgres|mysql) test_database "$1" ;;
  *) echo 'usage: ci.sh [all|check|test|postgres|mysql]' >&2; exit 1 ;;
esac
