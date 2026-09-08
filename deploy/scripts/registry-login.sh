#!/bin/sh
# 由 build-release.sh / cd.sh source；凭据由 Flow 私密变量注入。
set +x
if [ -n "${EAGLE_ACR_USERNAME:-}${EAGLE_ACR_PASSWORD:-}" ]; then
  : "${EAGLE_ACR_USERNAME:?EAGLE_ACR_USERNAME is required}"
  : "${EAGLE_ACR_PASSWORD:?EAGLE_ACR_PASSWORD is required}"
  : "${EAGLE_ACR_REGISTRY:?EAGLE_ACR_REGISTRY is required}"
  printf '%s' "${EAGLE_ACR_PASSWORD}" |
    docker login "${EAGLE_ACR_REGISTRY}" --username "${EAGLE_ACR_USERNAME}" --password-stdin
fi
