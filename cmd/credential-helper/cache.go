package main

import "github.com/tweag/credential-helper/cache"

// newCache constructs the built-in MemCache
// when building with Go.
// Bazel uses a generated file instead.
var newCache = cache.NewMemCache
