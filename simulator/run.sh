#!/usr/bin/env bash
set -euo pipefail

SIMULATOR_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SIMULATOR_DIR}/.." && pwd)"
BUILD_DIR="${SIMULATOR_BUILD_DIR:-${SIMULATOR_DIR}/build}"
OUTPUT_DIR="${SIMULATOR_OUTPUT_DIR:-${SIMULATOR_DIR}/output/latest}"
SCENE="${SIMULATOR_SCENE:-monitor}"
ANIMATION_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --scene)
      if [[ $# -lt 2 ]]; then
        echo "--scene requires a name" >&2
        exit 2
      fi
      SCENE="$2"
      shift 2
      ;;
    --animate)
      ANIMATION_ARGS=(--animate)
      shift
      ;;
    *)
      SCENE="$1"
      shift
      ;;
  esac
done

GENERATOR_ARGS=()
if command -v ninja >/dev/null 2>&1; then
  GENERATOR_ARGS=(-G Ninja)
fi

cmake -S "${SIMULATOR_DIR}" -B "${BUILD_DIR}" \
  "${GENERATOR_ARGS[@]}" \
  -DCMAKE_BUILD_TYPE=Debug \
  -DLV_BUILD_CONF_PATH="${REPO_ROOT}/third_party/lv_conf.h" \
  -DCONFIG_LV_BUILD_DEMOS=OFF \
  -DCONFIG_LV_BUILD_EXAMPLES=OFF
cmake --build "${BUILD_DIR}" --parallel

if [[ "${SCENE}" == "--list-scenes" ]]; then
  "${BUILD_DIR}/monitor_simulator" --list-scenes
  exit 0
fi

ctest --test-dir "${BUILD_DIR}" --output-on-failure
"${BUILD_DIR}/monitor_simulator" --scene "${SCENE}" --output "${OUTPUT_DIR}" --variant normal ${ANIMATION_ARGS[@]+"${ANIMATION_ARGS[@]}"}
"${BUILD_DIR}/monitor_simulator" --scene "${SCENE}" --output "${OUTPUT_DIR}" --variant inverted ${ANIMATION_ARGS[@]+"${ANIMATION_ARGS[@]}"}

if [[ "${SCENE}" == "monitor" ]]; then
  PREVIEW="${OUTPUT_DIR}/normal.png"
else
  PREVIEW="${OUTPUT_DIR}/${SCENE}_normal.png"
fi

printf '\nSimulator artifacts: %s\n' "${OUTPUT_DIR}"
printf 'Primary preview:     %s\n' "${PREVIEW}"
