#!/usr/bin/env bash
set -euo pipefail

TAG=$1
# The prefix is chosen to match what GitHub generates for source archives
PREFIX="credential-helper-${TAG:1}"
ARCHIVE="credential-helper-${TAG:1}.tar.gz"
git archive --format=tar.gz --prefix="${PREFIX}/" -o $ARCHIVE HEAD

cat release_notes_from_changelog.md
