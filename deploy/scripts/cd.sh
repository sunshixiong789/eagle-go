#!/bin/sh
# 在云效主机部署任务的制品解压目录执行；rollback 只读取主机上的快照。
set -eu
set +x
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
case "${1:-deploy}" in
  deploy)
    image_file="${script_dir}/../../release/image.txt"
    [ -f "${image_file}" ] || { echo 'cd: release/image.txt is missing; use the CI release artifact' >&2; exit 1; }
    image=$(cat "${image_file}")
    if [ -n "${EAGLE_IMAGE:-}" ] && [ "${EAGLE_IMAGE}" != "${image}" ]; then
      echo 'cd: EAGLE_IMAGE conflicts with the release artifact' >&2
      exit 1
    fi
    export EAGLE_IMAGE="${image}"
    . "${script_dir}/registry-login.sh"
    ;;
  rollback) ;;
  *) echo 'usage: cd.sh [deploy|rollback]' >&2; exit 1 ;;
esac
exec sh "${script_dir}/deploy.sh" "${1:-deploy}"
