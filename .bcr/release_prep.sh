#!/usr/bin/env bash
set -euo pipefail

TAG=$1
bazel run //bzl/private/release:do_a_release -- $TAG

# Emit the release notes stored in the intermediate file, 
# which will have been extracted by the markdown-extract workflow
# and placed into this file, per the parameter passed from the release 
# workflow to the release_ruleset workflow.
cat release_notes_from_changelog.md
