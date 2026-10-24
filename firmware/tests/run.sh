#!/usr/bin/env bash
set -euo pipefail
TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${MONITOR_TEST_BUILD_DIR:-${TEST_DIR}/build/host}"
cmake -S "${TEST_DIR}" -B "${BUILD_DIR}" -DMONITOR_TEST_HOST=ON -DCMAKE_BUILD_TYPE=Debug
cmake --build "${BUILD_DIR}" --parallel
ctest --test-dir "${BUILD_DIR}" --output-on-failure "$@"
