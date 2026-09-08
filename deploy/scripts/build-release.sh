#!/bin/sh
# 云效构建任务：推送镜像，以 Buildx 返回的 digest 生成无密钥发布包。
set -eu
set +x
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH= cd -- "${script_dir}/../.." && pwd)
cd "${repo_dir}"
: "${EAGLE_IMAGE_REPOSITORY:?set the full ACR repository without a tag}"
case "${EAGLE_IMAGE_REPOSITORY}" in
  *://*|*@*|*[!a-z0-9._:/-]*|*/|/*) echo 'build: invalid image repository' >&2; exit 1 ;;
esac
case "${EAGLE_IMAGE_REPOSITORY##*/}" in
  *:*) echo 'build: repository must not include a tag' >&2; exit 1 ;;
esac
case "${EAGLE_IMAGE_REPOSITORY}" in
  */*) ;;
  *) echo 'build: a full registry/repository is required' >&2; exit 1 ;;
esac
commit=$(git rev-parse HEAD)
if [ -n "${CI_COMMIT_SHA:-}" ] && [ "${CI_COMMIT_SHA}" != "${commit}" ]; then
  echo 'build: CI_COMMIT_SHA does not match the checked-out commit' >&2
  exit 1
fi
if [ -n "$(git status --porcelain --untracked-files=normal)" ]; then
  echo 'build: commit all source changes before creating a release' >&2
  exit 1
fi
build_number=${BUILD_NUMBER:-local}
case "${build_number}" in
  *[!a-zA-Z0-9_.-]*) echo 'build: invalid BUILD_NUMBER' >&2; exit 1 ;;
esac
image_tag="${EAGLE_IMAGE_REPOSITORY}:${commit}-${build_number}"
for command in docker python3 tar; do command -v "${command}" >/dev/null; done
docker buildx version >/dev/null
mkdir -p dist
# 不允许失败重试上传上一次构建留下的包。
rm -f dist/eagle-release.tgz dist/release-image.txt
stage=$(mktemp -d "${repo_dir}/dist/release.XXXXXXXX")
trap 'rm -rf "${stage}"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
. "${script_dir}/registry-login.sh"

set -- docker buildx build --pull --push \
  --platform "${EAGLE_BUILD_PLATFORM:-linux/amd64}" \
  --build-arg "VERSION=${commit}" \
  --label "org.opencontainers.image.revision=${commit}" \
  --metadata-file "${stage}/metadata.json" --tag "${image_tag}"
if [ -n "${EAGLE_BUILDER_IMAGE:-}" ]; then set -- "$@" --build-arg "BUILDER_IMAGE=${EAGLE_BUILDER_IMAGE}"; fi
if [ -n "${EAGLE_RUNTIME_IMAGE:-}" ]; then set -- "$@" --build-arg "RUNTIME_IMAGE=${EAGLE_RUNTIME_IMAGE}"; fi
if [ -n "${GOPROXY:-}" ]; then set -- "$@" --build-arg "GOPROXY=${GOPROXY}"; fi
"$@" .
digest=$(python3 - "${stage}/metadata.json" <<'PY'
import json
import re
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    digest = json.load(source).get("containerimage.digest", "")
if not re.fullmatch(r"sha256:[0-9a-f]{64}", digest):
    sys.exit("build: Buildx did not return a valid image digest")
print(digest)
PY
)
image="${EAGLE_IMAGE_REPOSITORY}@${digest}"
mkdir -p "${stage}/deploy/scripts" "${stage}/release"
cp deploy/compose.app.yml "${stage}/deploy/"
cp deploy/scripts/cd.sh deploy/scripts/deploy.sh deploy/scripts/registry-login.sh "${stage}/deploy/scripts/"
printf '%s\n' "${image}" >"${stage}/release/image.txt"
printf '%s\n' "${commit}" >"${stage}/release/commit.txt"
COPYFILE_DISABLE=1 tar -czf "${stage}/eagle-release.tgz" -C "${stage}" deploy release
mv "${stage}/eagle-release.tgz" dist/eagle-release.tgz
cp "${stage}/release/image.txt" dist/release-image.txt
# 指定容器/VM 环境中可供后续步骤展示；跨任务发布以制品内 image.txt 为准。
if [ -n "${FLOW_ENV:-}" ]; then printf 'EAGLE_IMAGE=%s\n' "${image}" >>"${FLOW_ENV}"; fi
printf 'build: %s\nartifact: dist/eagle-release.tgz\n' "${image}"
