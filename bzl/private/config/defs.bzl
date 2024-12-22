HelperTargetPlatformInfo = provider(
    doc = "Information on the target platform of the credential helper",
    fields = {
        "os": "The OS (as GOOS)",
        "cpu": "The cpu / arch (as GOARCH)",
    },
)

ModuleVersionInfo = provider(
    doc = "Metadata on the version of a module",
    fields = {
        "version": "The version (as defined in module function of MODULE.bazel)",
    },
)

def _os_cpu_impl(ctx):
    return [HelperTargetPlatformInfo(
        os = ctx.attr.os,
        cpu = ctx.attr.cpu,
    )]

os_cpu = rule(
    implementation = _os_cpu_impl,
    attrs = {
        "os": attr.string(),
        "cpu": attr.string(),
    },
)

def _version_impl(ctx):
    return [ModuleVersionInfo(version = ctx.attr.version)]

version = rule(
    implementation = _version_impl,
    attrs = {"version": attr.string()},
)

def _version_repo_impl(rctx):
    rctx.file(
        "BUILD.bazel",
        content = """load("@tweag-credential-helper//bzl/private/config:defs.bzl", "version")

version(
    name = "tweag-credential-helper-version",
    version = "{}",
    visibility = ["//visibility:public"],
)
""".format(rctx.attr.version),
    )

version_repo = repository_rule(
    implementation = _version_repo_impl,
    attrs = {"version": attr.string()},
)

def _module_version_impl(ctx):
    if len(ctx.modules) != 1:
        fail("this extension should only be used by @tweag-credential-helper")
    module = ctx.modules[0]
    version_repo(
        name = "tweag-credential-helper-version",
        version = module.version,
    )
    return ctx.extension_metadata(
        root_module_direct_deps = [],
        root_module_direct_dev_deps = ["tweag-credential-helper-version"],
        reproducible = True,
    )

module_version = module_extension(
    implementation = _module_version_impl,
)
