# Developer Guide

Most users can directly use the credential helper as a Bazel module by following the [README](/README.md).
If you want to fix a bug or develop a new feature, follow this guide to setup your environment.

## Setting up a development environment

The following steps allow you to build the project and use a development version in your own Bazel module.

### Install Bazel

On non-NixOS systems, [Bazelisk is the recommended way of installing Bazel][install-bazelisk].
NixOS requires a patched version of Bazel, which is available in nixpkgs (`bazel_7` is the newest version at the time of writing):

```
# Using flakes
$ nix shell nixpkgs#bazel_7

# Traditional
$ nix-shell -p bazel_7
```

### Build the code and run unit tests

To build & test everything at once:

```
$ bazel build //...
$ bazel test //...
```

Building the helper binary directly works too:

```
# helper binary for target platform
$ bazel build //:tweag-credential-helper

# Build helper binary without Bazel (using Go)
$ go build ./cmd/credential-helper

# runnable installer target
$ bazel run //installer

# all helper binaries bundled in a release
$ bazel build //bzl/private/release

# build the full release distribution and list the contents
$ bazel build //bzl/private/release:dist_tar
$ tar -tvf bazel-bin/bzl/private/release/dist.tar
```

### Run integration tests

The CI runs integration tests from the [examples directory](/examples) in addition to running `bazel test` on the code.
You can do this locally as well:

```
# list all integration tests
$ bazel query 'kind(sh_test, //examples/...)'

# run all
$ bazel test //examples:integration_tests
```

Please note that the `full` test requires real credentials for all cloud services supported by the credential helper, so expect this to fail unless you are logged in properly.

### Use a development version of the credential helper

Outside of Bazel, you can just [build the helper](#build-the-code-and-run-unit-tests) and invoke the binary directly:

```
echo '{"uri": "https://example.com/foo"}' | credential-helper get
```

In a Bazel project, you can use a local checkout of the `@tweag-credential-helper` module.
Simply add the credential helper using a `bazel_dep` and then override it:

`MODULE.bazel`

```starlark
module(name = "my_own_bazel_module", version = "0.0.0")

bazel_dep(name = "tweag-credential-helper", version = "0.0.0")

# replace path with an absolute or relative path to your local checkout
local_path_override(
    module_name = "tweag-credential-helper",
    path = "/path/to/github.com/tweag/credential-helper",
)
```

With this setup, you can then follow the regular user guide and run the installer as usual:

```
$ bazel run @tweag-credential-helper//installer
```

[install-bazelisk]: https://bazel.build/install/bazelisk
