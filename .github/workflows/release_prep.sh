#!/usr/bin/env bash
set -euo pipefail

# Prepare artifacts / release assets.
bazel run //bzl/private/release:do_a_release
