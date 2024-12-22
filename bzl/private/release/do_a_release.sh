#!/usr/bin/env bash
set -eo pipefail
set -o errtrace

if [ -z "$BUILD_WORKSPACE_DIRECTORY" ]; then
    echo "DO NOT RUN THIS SCRIPT OUTSIDE BAZEL!" 1>&2
    echo "Usage: bazel run //bzl/private/release:do_a_release -- <RELEASE_TAG>" 1>&2
    exit 1
fi

if [ $# -ne 1 ]; then
    echo "Usage: bazel run //bzl/private/release:do_a_release -- <RELEASE_TAG>" 1>&2
    exit 1
fi

if [ "$1" != "$EXPECTED_RELEASE_TAG" ]; then
    echo "User supplied release tag (${1}) does not match tag deduced from Bazel module version (${EXPECTED_RELEASE_TAG})" 1>&2
    echo "Update and commit a different module version (in MODULE.bazel), if you are doing a release." 1>&2
    echo "Usage: bazel run //bzl/private/release:do_a_release -- <RELEASE_TAG>" 1>&2
    exit 1
fi

DIST_TAR=$(realpath "${DIST_TAR}")

git describe --exact-match --tags HEAD 2>/dev/null || error_code=$?
if [ "${error_code}" -ne 0 ]; then
    echo "Current commit is not a tag. Aborting." 1>&2
    exit 1
fi

commit=$(git rev-parse HEAD)
tag=$(git describe --exact-match --tags || echo invalid)

if [ "${tag}" != "$EXPECTED_RELEASE_TAG" ]; then
    echo "Bazel module version (${MODULE_VERSION}) dictates git tag to be ${EXPECTED_RELEASE_TAG}, but found ${tag}" 1>&2
    exit 1
fi

workdir=$(mktemp -d)
tar -xvf "${DIST_TAR}" -C "${workdir}"

assets=$(find "${workdir}" -type f)

gh release create \
  --draft \
  --target "${commit}" \
  "${tag}" \
  $assets
