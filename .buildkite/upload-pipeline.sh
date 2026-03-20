#!/bin/bash

# Uploads the appropriate Buildkite step definitions based on changed paths.
#
# - Pull Requests
#  - Upload .buildkite/test-historyserver-e2e.yml if any of the following files are changed:
#    - historyserver/
#    - .buildkite/test-historyserver-e2e.yml
#    - .buildkite/build-historyserver.sh
#  - Otherwise, upload the main bundle without history server E2E.
#
# - Non-PR Builds (e.g., pushes to master): Upload everything, including history server E2E.
#
# Optional override (environment variable):
#   KUBERAY_CI_PIPELINE=all|main-bundle|historyserver
#
# Description:
#   - KUBERAY_CI_PIPELINE=all: Upload everything, including history server E2E.
#   - KUBERAY_CI_PIPELINE=main-bundle: Upload the main bundle without history server E2E.
#   - KUBERAY_CI_PIPELINE=historyserver: Upload only the history server E2E pipeline.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

upload_historyserver() {
  echo "+++ Uploading Pipeline: History Server"
  buildkite-agent pipeline upload .buildkite/test-historyserver-e2e.yml
}

upload_main_bundle() {
  echo "+++ Uploading Pipeline: Main Bundle (no history server)"
  # Order matches previous multi-file uploads; concat YAML step lists.
  cat \
    .buildkite/test-e2e.yml \
    .buildkite/test-sample-yamls.yml \
    .buildkite/test-kubectl-plugin-e2e.yml \
    .buildkite/test-python-client.yml \
    | buildkite-agent pipeline upload
}

upload_all() {
  echo "+++ Uploading Pipeline: All (including history server)"
  cat \
    .buildkite/test-e2e.yml \
    .buildkite/test-sample-yamls.yml \
    .buildkite/test-kubectl-plugin-e2e.yml \
    .buildkite/test-python-client.yml \
    .buildkite/test-historyserver-e2e.yml \
    | buildkite-agent pipeline upload
}

is_historyserver_path() {
  local f="$1"
  case "${f}" in
    # TODO(jwj): Consider adding dashboard/ to the historyserver path.
    historyserver/* | .buildkite/test-historyserver-e2e.yml | .buildkite/build-historyserver.sh)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

resolve_pr_mode() {
  local base="${BUILDKITE_PULL_REQUEST_BASE_BRANCH:-master}"
  # TODO(jwj): Might need to tune depth.
  if ! git fetch origin "${base}" --depth=500; then
    echo "!!! git fetch origin ${base} failed; defaulting to all (including history server)" >&2
    echo "all"
    return
  fi

  local changed
  if ! changed="$(git diff --name-only "origin/${base}"...HEAD)"; then
    echo "!!! git diff failed; defaulting to all (including history server)" >&2
    echo "all"
    return
  fi

  if [[ -z "${changed// }" ]]; then
    echo "!!! no changes; defaulting to all (including history server)" >&2
    echo "all"
    return
  fi

  local historyserver_changed=false
  local other_changed=false

  while IFS= read -r file; do
    [[ -z "${file}" ]] && continue
    if is_historyserver_path "${file}"; then
      historyserver_changed=true
    else
      other_changed=true
    fi
  done <<< "${changed}"

  if [[ "${historyserver_changed}" == "true" && "${other_changed}" == "true" ]]; then
    echo "all"
  elif [[ "${historyserver_changed}" == "true" ]]; then
    echo "historyserver"
  else
    echo "main-bundle"
  fi
}

case "${KUBERAY_CI_PIPELINE:-}" in
  all | ALL)
    upload_all
    exit 0
    ;;
  main-bundle | main | MAIN)
    upload_main_bundle
    exit 0
    ;;
  historyserver | historyserver-only | hs-only)
    upload_historyserver
    exit 0
    ;;
esac

if [[ "${BUILDKITE_PULL_REQUEST:-false}" == "false" ]]; then
  # For example, pushes to master.
  upload_all
else
  mode="$(resolve_pr_mode)"
  echo "+++ PR Path Mode: ${mode}"
  case "${mode}" in
    historyserver)
      upload_historyserver
      ;;
    main-bundle)
      upload_main_bundle
      ;;
    all)
      upload_all
      ;;
    *)
      echo "!!! unexpected mode '${mode}'; uploading all (including history server)" >&2
      upload_all
      ;;
  esac
fi
