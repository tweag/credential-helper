#!/usr/bin/env bash
set -euo pipefail

TAG=$1
# The prefix is chosen to match what GitHub generates for source archives
PREFIX="credential-helper-${TAG:1}"
ARCHIVE="credential-helper-${TAG:1}.tar.gz"
git archive --format=tar.gz --prefix="${PREFIX}/" -o $ARCHIVE HEAD

# Emit the release notes stored in the intermediate file, 
# which will have been extracted by the markdown-extract workflow
# and placed into this file, per the parameter passed from the release 
# workflow to the release_ruleset workflow.
cat release_notes_from_changelog.md
