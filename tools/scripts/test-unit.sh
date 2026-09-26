#!/bin/sh
# 自动纳入业务分层及公共包；数据库适配和进程启动由完整测试覆盖。
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo_dir"
all_packages=$(go list ./internal/... ./pkg/...)
packages=$(printf '%s\n' "$all_packages" | awk '
  /\/internal\/platform\/server($|\/)/ { print; next }
  /\/internal\/[^\/]+\/(domain|application|interfaces)($|\/)/ { print; next }
  /\/pkg\/(db|otelx)($|\/)/ { next }
  /\/pkg\// { print }
')
test -n "$packages" || { echo 'No unit packages discovered' >&2; exit 1; }

case "${1:-unit}" in
  unit)
    # 包路径由 go list 产生，需要按空白展开成参数。
    go test -race $packages
    ;;
  coverage)
    profile=$(mktemp)
    trap 'rm -f "$profile"' EXIT
    trap 'exit 130' INT
    trap 'exit 143' TERM
    go test -covermode=atomic -coverprofile="$profile" $packages
    awk -v min="${2:-80}" '
      NR == 1 || /\.pb\.go:/ { next }
      {
        pkg = $1
        sub(/\/[^\/]+$/, "", pkg)
        total[pkg] += $2
        if ($3 > 0) covered[pkg] += $2
      }
      END {
        if (length(total) == 0) { print "No coverage statements found"; exit 1 }
        for (pkg in total) {
          if (total[pkg] == 0) continue
          coverage = 100 * covered[pkg] / total[pkg]
          printf "%s: %.1f%% (minimum %.1f%%)\n", pkg, coverage, min
          if (coverage + 0.000001 < min) failed = 1
        }
        exit failed
      }
    ' "$profile"
    ;;
  *) echo 'usage: test-unit.sh [unit|coverage [minimum]]' >&2; exit 1 ;;
esac
