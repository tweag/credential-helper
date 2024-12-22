load("@rules_go//go:def.bzl", "go_binary", "go_library", "GoLibrary")
load("//bzl/private/config:defs.bzl", "HelperTargetPlatformInfo")
load("//bzl/private/prebuilt:prebuilt.bzl", "PrebuiltHelperCollection")

def credential_helper(
    *,
    name,
    helperfactory,
    helperfactory_type_name = "Factory",
    cache,
    cache_type_name = "NewCache",
    extra_srcs = [],
    extra_deps = [Label("@tweag-credential-helper//cmd/root")],
    **kwargs):
    credential_helper_plugin(
        name = "{}_helperfactory".format(name),
        go_library = helperfactory,
        type_name = helperfactory_type_name,
        out = "{}_helperfactory.go".format(name),
        template = Label("@tweag-credential-helper//bzl/private/plugin:helperfactory.go.tpl"),
    )
    credential_helper_plugin(
        name = "{}_cache".format(name),
        go_library = cache,
        type_name = cache_type_name,
        out = "{}_cache.go".format(name),
        template = Label("@tweag-credential-helper//bzl/private/plugin:cache.go.tpl"),
    )
    go_library(
        name = "{}_lib".format(name),
        srcs = [
            "{}_helperfactory.go".format(name),
            "{}_cache.go".format(name),
            Label("@tweag-credential-helper//cmd/credential-helper:credential-helper.go"),
        ] + extra_srcs,
        deps = [
            helperfactory,
            cache,
        ] + extra_deps,
        importpath = "main",
    )
    go_binary(
        name = name,
        embed = ["{}_lib".format(name)],
        importpath = "main",
        **kwargs
    )

def _credential_helper_plugin_impl(ctx):
    go_info = ctx.attr.go_library[GoLibrary]
    ctx.actions.expand_template(
        template = ctx.file.template,
        output = ctx.outputs.out,
        substitutions = {
            "{{IMPORTPATH}}": go_info.importpath,
            "{{TYPE_NAME}}": ctx.attr.type_name,
        },
    )
    return [DefaultInfo(files = depset(direct = [ctx.outputs.out]))]


credential_helper_plugin = rule(
    implementation = _credential_helper_plugin_impl,
    attrs = {
        "go_library": attr.label(
            providers = [GoLibrary],
            mandatory = True,
        ),
        "type_name": attr.string(
            default = "Factory",
        ),
        "out": attr.output(
            mandatory = True,
        ),
        "template": attr.label(
            mandatory = True,
            allow_single_file = True,
        ),
    },
)

def _installer_impl(ctx):
    if ctx.attr.prebuilt_helper_collection != None:
        prebuilt_helper_collection = ctx.attr.prebuilt_helper_collection[PrebuiltHelperCollection].platform_to_helper
    else:
        prebuilt_helper_collection = {}
    target_platform_info = ctx.attr._os_cpu[HelperTargetPlatformInfo]
    os = target_platform_info.os
    cpu = target_platform_info.cpu

    # default to use helper from source
    helper = ctx.executable.credential_helper
    if (os, cpu) in prebuilt_helper_collection:
        # use prebuilt helper if possible
        helper = prebuilt_helper_collection[(os, cpu)]

    installer = ctx.actions.declare_file(
        ctx.attr.name + ".link",
    )
    ctx.actions.symlink(
        output = installer,
        target_file = helper,
        is_executable = True,
    )

    runfiles = ctx.runfiles()
    if helper == ctx.executable.credential_helper:
        runfiles = runfiles.merge_all([
            ctx.attr.credential_helper[DefaultInfo].default_runfiles,
        ])
    env = {"CREDENTIAL_HELPER_INSTALLER_RUN": "1"}
    env.update(ctx.attr.env)

    return [
        DefaultInfo(
            files = depset([installer]),
            runfiles = runfiles,
            executable = installer,
        ),
        RunEnvironmentInfo(env, ctx.attr.env_inherit)
    ]

installer = rule(
    implementation = _installer_impl,
    attrs = {
        "credential_helper": attr.label(
            doc = "The binary to install",
            executable = True,
            cfg = "target",
            default = Label("@tweag-credential-helper"),
        ),
        "prebuilt_helper_collection": attr.label(
            mandatory = False,
            providers = [PrebuiltHelperCollection],
        ),
        "env": attr.string_dict(
            doc = """Environment variables to set for the test execution.""",
        ),
        "env_inherit": attr.string_list(
            doc = """Environment variables to inherit from the external environment.""",
        ),
        "_os_cpu": attr.label(
            default = Label("//bzl/private/config:target_os_cpu"),
            providers = [HelperTargetPlatformInfo],
        ),
    },
    executable = True,
)
