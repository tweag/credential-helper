package main

import real "{{IMPORTPATH}}"

// newCache constructs the customizable Cache.
// It is generated using the template under
// @tweag-credential-helper//bzl/private/plugin:cache.go.tpl
// when building with Bazel.
// Building with Go directly, this would
// instead always use `cache.MemCache`.
var newCache = real.{{TYPE_NAME}}
