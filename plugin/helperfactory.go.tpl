package main

import real "{{IMPORTPATH}}"

// helperFactory is the customizable factory.
// It is generated using the template under
// @tweag-credential-helper//plugin:helperfactory.go.tpl
// when building with Bazel.
// Building with Go directly, this would
// instead always use `fallback.FallbackHelperFactory`.
var helperFactory = real.{{TYPE_NAME}}
