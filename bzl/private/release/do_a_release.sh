#!/usr/bin/env bash
set -eo pipefail
set -o errtrace

# Bazel should set this to the root directory.
if [ -z "$BUILD_WORKSPACE_DIRECTORY" ]; then
    echo "DO NOT RUN THIS SCRIPT OUTSIDE BAZEL!" 1>&2
    echo "Usage: bazel run //bzl/private/release:do_a_release" 1>&2
    exit 1
fi

git describe --exact-match --tags HEAD 2>/dev/null && error_code=$? || error_code=$? 
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

# Unpack the distribution tarball into the dist directory, for release_files (release_ruleset).
dist_dir="${BUILD_WORKSPACE_DIRECTORY}/dist"
if [ -d "$dist_dir" ]; then
    echo "Distribution folder already exists: ${dist_dir}" 1>&2
    exit 1
else
    mkdir "${dist_dir}"
fi
DIST_TAR=$(realpath "${DIST_TAR}")
tar -xvf "${DIST_TAR}" -C "${dist_dir}" 1>&2
