load("@bazel_skylib//rules:common_settings.bzl", "BuildSettingInfo")
load("@rules_go//go:def.bzl", "GoLibrary", "go_binary", "go_library")
load("//bzl/private/config:defs.bzl", "HelperTargetPlatformInfo")
load("//bzl/private/prebuilt:prebuilt.bzl", "PrebuiltHelperInfo")

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
    build_mode = ctx.attr._helper_build_mode[BuildSettingInfo].value
    allow_from_source = False
    allow_prebuilt = False
    if build_mode == "auto":
        allow_from_source = True
        allow_prebuilt = True
    elif build_mode == "from_source":
        allow_from_source = True
        allow_prebuilt = False
    elif build_mode == "prebuilt":
        allow_from_source = False
        allow_prebuilt = True

    prebuilt_helper = None
    if ctx.attr.prebuilt_helper != None:
        prebuilt_helper = ctx.attr.prebuilt_helper[PrebuiltHelperInfo].helper
    target_platform_info = ctx.attr._os_cpu[HelperTargetPlatformInfo]
    os = target_platform_info.os
    cpu = target_platform_info.cpu

    destination = ctx.attr._default_install_destination_windows[BuildSettingInfo].value if os == "windows" else ctx.attr._default_install_destination_unix[BuildSettingInfo].value
    target_specific_destination = ctx.attr.destination_windows if os == "windows" else ctx.attr.destination_unix
    if len(target_specific_destination) > 0:
        destination = target_specific_destination

    helper = None

    # fall back to use helper from source (if available and allowed)
    if allow_from_source and ctx.executable.credential_helper != None:
        helper = ctx.executable.credential_helper

    # use prebuilt helper instead (if available for platform and allowed)
    if allow_prebuilt and prebuilt_helper != None:
        helper = prebuilt_helper

    if helper == None:
        if build_mode == "from_source":
            fail("Requested helper to be built from source, but none avalable in installer(name = \"%s\"). Configure one via the credential_helper label." % ctx.attr.name)
        if build_mode == "prebuilt":
            fail("Requested prebuilt helper but no matching prebuilt helper binary available for platform %s_%s in installer(name = \"%s\"). Register a matching prebuilt or allow building from source." % (os, cpu, ctx.attr.name))
        fail("No matching helper binary available in installer(name = \"%s\")" % ctx.attr.name)

    installer = ctx.actions.declare_file(
        ctx.attr.name + ".exe",
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
    if len(destination) > 0:
        env["CREDENTIAL_HELPER_INSTALLER_DESTINATION"] = destination
    env.update(ctx.attr.env)

    return [
        DefaultInfo(
            files = depset([installer]),
            runfiles = runfiles,
            executable = installer,
        ),
        RunEnvironmentInfo(env, ctx.attr.env_inherit),
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
        "prebuilt_helper": attr.label(
            mandatory = False,
            providers = [PrebuiltHelperInfo],
        ),
        "destination_unix": attr.string(
            doc = """Install destination used on Unix. Can use prefixes like %workspace% to use workspace-relative destinations. 
            If unset, destination is set to @tweag-credential-helper//bzl/config:default_install_destination_unix.
            If both are unset, destination is set to well-known helper workdir that is targeted by shell wrapper.""",
            mandatory = False,
        ),
        "destination_windows": attr.string(
            doc = """Install destination used on Windows. Can use prefixes like %workspace% to use workspace-relative destinations.
            If unset, destination is set to @tweag-credential-helper//bzl/config:default_install_destination_windows,
            which defaults to a path inside of the Bazel workspace (shell wrapper doesn't work on Windows).""",
            mandatory = False,
        ),
        "env": attr.string_dict(
            doc = """Environment variables to set for the test execution.""",
        ),
        "env_inherit": attr.string_list(
            doc = """Environment variables to inherit from the external environment.""",
        ),
        "_helper_build_mode": attr.label(
            default = Label("//bzl/config:helper_build_mode"),
            providers = [BuildSettingInfo],
        ),
        "_default_install_destination_unix": attr.label(
            default = Label("//bzl/config:default_install_destination_unix"),
            providers = [BuildSettingInfo],
        ),
        "_default_install_destination_windows": attr.label(
            default = Label("//bzl/config:default_install_destination_windows"),
            providers = [BuildSettingInfo],
        ),
        "_os_cpu": attr.label(
            default = Label("//bzl/private/config:target_os_cpu"),
            providers = [HelperTargetPlatformInfo],
        ),
    },
    executable = True,
)
