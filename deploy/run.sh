#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODE_FILE="${HOME}/.luna-edge/mode"

require_mode_file() {
  if [[ ! -f "${MODE_FILE}" ]]; then
    echo "mode file not found: ${MODE_FILE}" >&2
    echo "run ./prepare.sh first" >&2
    exit 1
  fi
}

current_mode() {
  require_mode_file
  tr -d '[:space:]' < "${MODE_FILE}"
}

mode_file_for_component() {
  local mode
  mode="$(current_mode)"
  local component="${1:-}"
  case "${mode}:${component}" in
    default:master)
      printf '%s\n' "${SCRIPT_DIR}/luna-edge-master.yaml"
      ;;
    default:slave)
      printf '%s\n' "${SCRIPT_DIR}/luna-edge-slave.yaml"
      ;;
    cilium:master)
      printf '%s\n' "${SCRIPT_DIR}/luna-edge-master-cilium-clustermesh.yaml"
      ;;
    cilium:slave)
      printf '%s\n' "${SCRIPT_DIR}/luna-edge-slave-cilium-clustermesh.yaml"
      ;;
    *)
      echo "unsupported mode/component: ${mode} ${component}" >&2
      exit 1
      ;;
  esac
}

apply_component() {
  local component="${1:-}"
  local file
  file="$(mode_file_for_component "${component}")"
  [[ -f "${file}" ]] || { echo "missing file: ${file}" >&2; exit 1; }
  kubectl apply -f "${file}"
}

delete_component() {
  local component="${1:-}"
  local file
  file="$(mode_file_for_component "${component}")"
  [[ -f "${file}" ]] || { echo "missing file: ${file}" >&2; exit 1; }
  kubectl delete -f "${file}" --ignore-not-found
}

set_mode() {
  local mode="${1:-}"
  require_mode_file
  case "${mode}" in
    default|cilium)
      printf '%s\n' "${mode}" > "${MODE_FILE}"
      echo "mode switched to ${mode}"
      ;;
    *)
      echo "usage: $0 mode <default|cilium>" >&2
      exit 1
      ;;
  esac
}

usage() {
  cat <<'EOF'
Usage:
  ./run.sh up <master|slave>
  ./run.sh down <master|slave>
  ./run.sh mode <default|cilium>
EOF
}

cmd="${1:-}"
case "${cmd}" in
  up)
    apply_component "${2:-}"
    ;;
  down)
    delete_component "${2:-}"
    ;;
  mode)
    set_mode "${2:-}"
    ;;
  *)
    usage
    exit 1
    ;;
esac
