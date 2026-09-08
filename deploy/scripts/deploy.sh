#!/bin/sh

# 云效主机部署入口。数据库不在这里创建，只连接环境侧预先准备好的 RDS。
# 使用方式见 docs/aliyun-flow-deployment.md。
set -eu

required_variables="EAGLE_IMAGE DEPLOY_ENV EAGLE_DATABASE_DSN EAGLE_AUTH_ISSUER EAGLE_AUTH_AUDIENCE EAGLE_AUTH_SIGNING_SECRET"
for variable_name in ${required_variables}; do
  eval "variable_value=\${${variable_name}:-}"
  if [ -z "${variable_value}" ]; then
    echo "deploy: ${variable_name} is required" >&2
    exit 1
  fi
done

case "${DEPLOY_ENV}" in
  development|testing) ;;
  *)
    echo "deploy: DEPLOY_ENV must be development or testing" >&2
    exit 1
    ;;
esac

case "${EAGLE_IMAGE}" in
  *:latest|latest)
    echo "deploy: mutable latest image is not allowed" >&2
    exit 1
    ;;
esac

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source_compose="${script_dir}/../compose.app.yml"
deploy_root="${EAGLE_DEPLOY_ROOT:-/opt/eagle}"
deploy_dir="${deploy_root}/${DEPLOY_ENV}"
compose_file="${deploy_dir}/compose.yml"
runtime_env="${deploy_dir}/runtime.env"
next_env="${deploy_dir}/runtime.env.next"
previous_env="${deploy_dir}/runtime.env.previous"
project_name="eagle-${DEPLOY_ENV}"

install -d -m 0750 "${deploy_dir}"
install -m 0644 "${source_compose}" "${compose_file}"
umask 077
exec 9>"${deploy_dir}/deploy.lock"
if ! flock -n 9; then
  echo "deploy: another ${DEPLOY_ENV} deployment is still running" >&2
  exit 1
fi

write_env_value() {
  key=$1
  value=$2
  case "${value}" in
    *"
"*|*"'"*)
      echo "deploy: ${key} contains an unsupported quote or newline" >&2
      exit 1
      ;;
  esac
  # 单引号阻止 Compose 把 DSN 密码中的 $ 当作二次变量插值。
  printf "%s='%s'\n" "${key}" "${value}"
}

write_env() {
  target=$1
  {
    write_env_value EAGLE_IMAGE "${EAGLE_IMAGE}"
    write_env_value EAGLE_ENV_FILE "${runtime_env}"
    write_env_value EAGLE_BIND_ADDRESS "${EAGLE_BIND_ADDRESS:-127.0.0.1}"
    write_env_value EAGLE_HTTP_PORT "${EAGLE_HTTP_PORT:-8000}"
    write_env_value EAGLE_METRICS_PORT "${EAGLE_METRICS_PORT:-9101}"
    write_env_value EAGLE_DATABASE_DRIVER "${EAGLE_DATABASE_DRIVER:-postgres}"
    write_env_value EAGLE_DATABASE_DSN "${EAGLE_DATABASE_DSN}"
    write_env_value EAGLE_AUTH_ISSUER "${EAGLE_AUTH_ISSUER}"
    write_env_value EAGLE_AUTH_AUDIENCE "${EAGLE_AUTH_AUDIENCE}"
    write_env_value EAGLE_AUTH_SIGNING_SECRET "${EAGLE_AUTH_SIGNING_SECRET}"
    write_env_value EAGLE_AUTH_ACCESS_TOKEN_TTL "${EAGLE_AUTH_ACCESS_TOKEN_TTL:-900s}"
    write_env_value EAGLE_AUTH_REFRESH_TOKEN_TTL "${EAGLE_AUTH_REFRESH_TOKEN_TTL:-2592000s}"
    write_env_value EAGLE_AUTH_GOOGLE_ENABLED "${EAGLE_AUTH_GOOGLE_ENABLED:-false}"
    write_env_value EAGLE_AUTH_GOOGLE_CLIENT_ID "${EAGLE_AUTH_GOOGLE_CLIENT_ID:-}"
    write_env_value EAGLE_AUTH_APPLE_ENABLED "${EAGLE_AUTH_APPLE_ENABLED:-false}"
    write_env_value EAGLE_AUTH_APPLE_CLIENT_ID "${EAGLE_AUTH_APPLE_CLIENT_ID:-}"
    write_env_value EAGLE_OBSERVABILITY_OTLP_ENDPOINT "${EAGLE_OBSERVABILITY_OTLP_ENDPOINT:-}"
    write_env_value EAGLE_OBSERVABILITY_OTLP_INSECURE "${EAGLE_OBSERVABILITY_OTLP_INSECURE:-true}"
    write_env_value EAGLE_OBSERVABILITY_TRACE_SAMPLE_RATIO "${EAGLE_OBSERVABILITY_TRACE_SAMPLE_RATIO:-0.1}"
    write_env_value EAGLE_OBSERVABILITY_LOG_LEVEL "${EAGLE_OBSERVABILITY_LOG_LEVEL:-info}"
  } >"${target}"
}

compose() {
  compose_env=$1
  shift
  docker compose --project-name "${project_name}" --env-file "${compose_env}" -f "${compose_file}" "$@"
}

# 同一环境由文件锁串行发布。先拉镜像、执行同版本迁移，再替换服务；
# 迁移失败不会触碰当前运行实例。
if [ -f "${runtime_env}" ]; then
  cp "${runtime_env}" "${previous_env}"
fi
write_env "${next_env}"
mv "${next_env}" "${runtime_env}"

if ! compose "${runtime_env}" pull || ! compose "${runtime_env}" --profile migration run --rm migrate; then
  echo "deploy: image pull or migration failed; the running application was not changed" >&2
  if [ -f "${previous_env}" ]; then
    mv "${previous_env}" "${runtime_env}"
  fi
  exit 1
fi

if ! compose "${runtime_env}" up -d --remove-orphans --wait --wait-timeout "${EAGLE_DEPLOY_TIMEOUT:-90}" eagle; then
  echo "deploy: readiness failed, restoring the previous application image" >&2
  if [ -f "${previous_env}" ]; then
    mv "${previous_env}" "${runtime_env}"
    compose "${runtime_env}" up -d --remove-orphans --wait --wait-timeout "${EAGLE_DEPLOY_TIMEOUT:-90}" eagle
  fi
  exit 1
fi

rm -f "${previous_env}"
compose "${runtime_env}" ps
