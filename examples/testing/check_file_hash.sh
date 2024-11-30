#!/usr/bin/env bash

set -euo pipefail
set -o errtrace

# This script checks the hash of a file against a known hash.

while [ $# -gt 0 ]; do
    path="$1"
    sha256="$2"
    echo $sha256 "*$path" | shasum -a 256 -c -
    shift 2
done
