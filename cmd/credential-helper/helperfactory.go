package main

import "github.com/tweag/credential-helper/helperfactory/fallback"

// helperFactory is the built-in fallback helper factory
// when building with Go.
// Bazel uses a generated file instead.
var helperFactory = fallback.FallbackHelperFactory
