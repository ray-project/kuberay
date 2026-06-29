#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

DIFFROOT="${SCRIPT_ROOT}/pkg"
OPENAPI_REPORT="${SCRIPT_ROOT}/hack/openapi-violations.report"
TMP_DIFFROOT="${SCRIPT_ROOT}/_tmp/pkg"
TMP_OPENAPI_REPORT="${SCRIPT_ROOT}/_tmp/openapi-violations.report"
_tmp="${SCRIPT_ROOT}/_tmp"

cleanup() {
  rm -rf "${_tmp}"
}
trap "cleanup" EXIT SIGINT

cleanup

mkdir -p "${TMP_DIFFROOT}"
cp -a "${DIFFROOT}"/* "${TMP_DIFFROOT}"
if [[ -f "${OPENAPI_REPORT}" ]]; then
  cp -a "${OPENAPI_REPORT}" "${TMP_OPENAPI_REPORT}"
fi

"${SCRIPT_ROOT}/hack/update-codegen.sh"
echo "diffing ${DIFFROOT} against freshly generated codegen"
ret=0
diff -Naupr "${DIFFROOT}" "${TMP_DIFFROOT}" || ret=$?
echo "diffing ${OPENAPI_REPORT} against freshly generated openapi violations report"
diff -Naupr "${OPENAPI_REPORT}" "${TMP_OPENAPI_REPORT}" || ret=$?
cp -a "${TMP_DIFFROOT}"/* "${DIFFROOT}"
if [[ -f "${TMP_OPENAPI_REPORT}" ]]; then
  cp -a "${TMP_OPENAPI_REPORT}" "${OPENAPI_REPORT}"
fi
if [[ $ret -eq 0 ]]
then
  echo "${DIFFROOT} up to date."
else
  echo "${DIFFROOT} is out of date. Please run 'make generate'"
  exit 1
fi
