#!/usr/bin/env bash
set -euo pipefail

marker="${E2E_MARKER:-/tmp/kb-shaped-marker.log}"
echo "${ADAPTER_EVENT} $(cat)" >>"$marker"
exit 0
