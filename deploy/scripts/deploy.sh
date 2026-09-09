#!/bin/sh
# 云效 ECS 单机部署。快照保存镜像、配置、Compose；数据库只向前迁移。
set -eu
set +x

fail() { echo "deploy: $*" >&2; exit 1; }
action=${1:-deploy}
case "${action}" in deploy|rollback) ;; *) fail 'usage: deploy.sh [deploy|rollback]' ;; esac
case "${DEPLOY_ENV:-}" in development|testing|production) ;; *) fail 'DEPLOY_ENV must be development, testing or production' ;; esac
deploy_root=${EAGLE_DEPLOY_ROOT:-/opt/eagle}
case "${deploy_root}" in /*) ;; *) fail 'EAGLE_DEPLOY_ROOT must be absolute' ;; esac
timeout=${EAGLE_DEPLOY_TIMEOUT:-90}
case "${timeout}" in ''|0|*[!0-9]*) fail 'EAGLE_DEPLOY_TIMEOUT must be a positive integer' ;; esac
for command in docker flock install mktemp; do command -v "${command}" >/dev/null || fail "${command} is required"; done
docker compose version >/dev/null
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
deploy_dir="${deploy_root}/${DEPLOY_ENV}"
project_name="eagle-${DEPLOY_ENV}"
umask 077
install -d -m 0750 "${deploy_dir}" "${deploy_dir}/releases"
exec 9>"${deploy_dir}/deploy.lock"
flock -n 9 || fail "another ${DEPLOY_ENV} deployment is still running"

read_pointer() {
  pointer=$1
  [ -f "${deploy_dir}/${pointer}" ] || return 0
  release_name=$(cat "${deploy_dir}/${pointer}")
  case "${release_name}" in release.*) ;; *) fail "invalid ${pointer} release pointer" ;; esac
  case "${release_name}" in *[!a-zA-Z0-9.-]*) fail "invalid ${pointer} release pointer" ;; esac
  [ -d "${deploy_dir}/releases/${release_name}" ] || fail "missing ${pointer} release snapshot"
  printf '%s\n' "${deploy_dir}/releases/${release_name}"
}

save_pointer() {
  printf '%s\n' "${2##*/}" >"${deploy_dir}/$1.next"
  mv -f "${deploy_dir}/$1.next" "${deploy_dir}/$1"
}

compose() (
  snapshot=$1
  shift
  # Shell 环境优先于 --env-file。必须清掉清单插值变量，否则回滚仍会发布新镜像。
  unset EAGLE_IMAGE EAGLE_BIND_ADDRESS EAGLE_HTTP_PORT EAGLE_METRICS_PORT EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY
  export EAGLE_ENV_FILE="${snapshot}/runtime.env"
  docker compose --project-name "${project_name}" --env-file "${snapshot}/compose.env" \
    -f "${snapshot}/compose.yml" "$@"
)

start() {
  compose "$1" up -d --no-deps --pull never --wait --wait-timeout "${timeout}" eagle
}

# 接管旧版脚本留下的运行文件，首次升级也能回滚。
if [ ! -f "${deploy_dir}/current" ] && [ -f "${deploy_dir}/runtime.env" ]; then
  [ -f "${deploy_dir}/compose.yml" ] || fail 'legacy runtime.env has no compose.yml'
  legacy=$(mktemp -d "${deploy_dir}/releases/release.XXXXXXXX")
  cp "${deploy_dir}/runtime.env" "${legacy}/runtime.env"
  cp "${deploy_dir}/runtime.env" "${legacy}/compose.env"
  cp "${deploy_dir}/compose.yml" "${legacy}/compose.yml"
  chmod 0600 "${legacy}"/*.env
  save_pointer current "${legacy}"
fi
current=$(read_pointer current)
candidate=
application_touched=false
committed=false
cleanup() {
  result=$?
  trap - EXIT INT TERM
  if [ "${committed}" = false ] && [ "${application_touched}" = true ]; then
    if [ -n "${current}" ]; then
      echo 'deploy: restoring the previous application snapshot' >&2
      if start "${current}"; then
        echo 'deploy: previous application is healthy; this deployment still failed' >&2
      else
        echo 'deploy: rollback failed; operator intervention required' >&2
      fi
    else
      echo 'deploy: first release failed; removing the failed application' >&2
      compose "${candidate}" rm --stop --force eagle || true
    fi
  fi
  if [ "${result}" -ne 0 ] && [ -n "${candidate}" ]; then
    echo "deploy: failed snapshot retained at ${candidate}" >&2
  fi
  exit "${result}"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

write_env_value() {
  key=$1
  value=$2
  case "${value}" in
    *"
"*|*"'"*|*"$(printf '\r')"*) fail "${key} contains an unsupported quote or newline" ;;
  esac
  # Compose 单引号字面量保留密码中的 $、# 和反斜杠；不 source 配置。
  printf "%s='%s'\n" "${key}" "${value}"
}

if [ "${action}" = rollback ]; then
  candidate=$(read_pointer previous)
  [ -n "${current}" ] && [ -n "${candidate}" ] || fail 'no previous successful release to roll back to'
else
  # 远端必须显式提供身份和数据库设置，禁止退回模板里的本地开发默认值。
  required_variables='EAGLE_IMAGE EAGLE_DATABASE_DSN EAGLE_AUTH_ISSUER EAGLE_AUTH_AUDIENCE EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID'
  for variable_name in ${required_variables}; do
    eval "variable_value=\${${variable_name}:-}"
    [ -n "${variable_value}" ] || fail "${variable_name} is required"
  done
  # 仅接收 digest，标签即使是提交 SHA 也可能被仓库覆盖。
  printf '%s\n' "${EAGLE_IMAGE}" | LC_ALL=C grep -Eq '^[a-z0-9][a-z0-9._:/-]*@sha256:[0-9a-f]{64}$' || fail 'EAGLE_IMAGE must be a full repository@sha256:digest'
  case "${EAGLE_DATABASE_DRIVER:-postgres}" in postgres|mysql) ;; *) fail 'unsupported database driver' ;; esac
  case "${EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY}" in /*) ;; *) fail 'signing key directory must be absolute' ;; esac
  case "${EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID}" in *[!a-zA-Z0-9_.-]*) fail 'invalid signing key ID' ;; esac
  [ -r "${EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY}/${EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID}.pem" ] || fail 'active signing key file is missing or unreadable'
  candidate=$(mktemp -d "${deploy_dir}/releases/release.XXXXXXXX")
  install -m 0644 "${script_dir}/../compose.app.yml" "${candidate}/compose.yml"
  # 不把私密变量写进部署元数据，也不把主机路径传入应用配置。
  {
    write_env_value EAGLE_IMAGE "${EAGLE_IMAGE}"
    write_env_value EAGLE_BIND_ADDRESS "${EAGLE_BIND_ADDRESS:-127.0.0.1}"
    write_env_value EAGLE_HTTP_PORT "${EAGLE_HTTP_PORT:-8000}"
    write_env_value EAGLE_METRICS_PORT "${EAGLE_METRICS_PORT:-9101}"
    write_env_value EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY "${EAGLE_AUTH_SIGNING_KEY_HOST_DIRECTORY}"
  } >"${candidate}/compose.env"
  {
    write_env_value EAGLE_DATABASE_DRIVER "${EAGLE_DATABASE_DRIVER:-postgres}"
    write_env_value EAGLE_DATABASE_DSN "${EAGLE_DATABASE_DSN}"
    write_env_value EAGLE_AUTH_ISSUER "${EAGLE_AUTH_ISSUER}"
    write_env_value EAGLE_AUTH_AUDIENCE "${EAGLE_AUTH_AUDIENCE}"
    write_env_value EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID "${EAGLE_AUTH_ACTIVE_SIGNING_KEY_ID}"
    # 文档只允许开发部署启用，测试和生产忽略误传的开启开关。
    if [ "${DEPLOY_ENV}" = development ]; then
      write_env_value EAGLE_SERVER_SWAGGER_ENABLED "${EAGLE_SERVER_SWAGGER_ENABLED:-true}"
      write_env_value EAGLE_SERVER_SWAGGER_PATH "${EAGLE_SERVER_SWAGGER_PATH:-/swagger}"
    else
      write_env_value EAGLE_SERVER_SWAGGER_ENABLED false
    fi
    # 可选覆盖只在 Flow 显式设置时写入，其余值取镜像中的共用模板。
    optional_variables='EAGLE_SERVER_HTTP_TIMEOUT EAGLE_DATABASE_MAX_CONNS EAGLE_DATABASE_MAX_IDLE_CONNS EAGLE_DATABASE_MAX_CONN_LIFETIME EAGLE_DATABASE_MAX_CONN_IDLE_TIME EAGLE_AUTH_ACCESS_TOKEN_TTL EAGLE_AUTH_REFRESH_TOKEN_TTL EAGLE_AUTH_GOOGLE_ENABLED EAGLE_AUTH_GOOGLE_CLIENT_ID EAGLE_AUTH_APPLE_ENABLED EAGLE_AUTH_APPLE_CLIENT_ID EAGLE_OBSERVABILITY_OTLP_ENDPOINT EAGLE_OBSERVABILITY_OTLP_INSECURE EAGLE_OBSERVABILITY_TRACE_SAMPLE_RATIO EAGLE_OBSERVABILITY_LOG_LEVEL'
    for variable_name in ${optional_variables}; do
      eval "variable_set=\${${variable_name}+yes}"
      if [ "${variable_set}" = yes ]; then
        eval "variable_value=\${${variable_name}}"
        write_env_value "${variable_name}" "${variable_value}"
      fi
    done
  } >"${candidate}/runtime.env"
fi

compose "${candidate}" config --quiet
if [ "${action}" = deploy ]; then
  # 拉取同一 digest，一次性迁移成功前不修改运行实例及 current/previous 指针。
  compose "${candidate}" --profile migration pull eagle migrate
  compose "${candidate}" --profile migration run --rm --no-deps migrate
fi
application_touched=true
start "${candidate}"
if [ -n "${current}" ]; then save_pointer previous "${current}"; fi
save_pointer current "${candidate}"
committed=true
echo "deploy: ${action} succeeded (${candidate##*/})"
compose "${candidate}" ps
