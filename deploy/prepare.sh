#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE_DIR="${HOME}/.luna-edge"
MODE_FILE="${MODE_DIR}/mode"
BASE_URL="${LUNA_EDGE_DEPLOY_BASE_URL:-https://raw.githubusercontent.com/jabberwocky238/luna-edge/main/deploy}"

FILES=(
  "luna-edge-master.yaml"
  "luna-edge-slave.yaml"
  "luna-edge-master-cilium-clustermesh.yaml"
  "luna-edge-slave-cilium-clustermesh.yaml"
  "run.sh"
)

mkdir -p "${MODE_DIR}"
if [[ ! -f "${MODE_FILE}" ]]; then
  printf 'default\n' > "${MODE_FILE}"
fi

for file in "${FILES[@]}"; do
  echo "downloading ${file}"
  curl -fsSL "${BASE_URL}/${file}" -o "${SCRIPT_DIR}/${file}"
done

echo "mode file: ${MODE_FILE}"
echo "current mode: $(cat "${MODE_FILE}")"
