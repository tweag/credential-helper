load("@rules_go//go:def.bzl", "go_binary", "go_library", "GoLibrary")

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
        template = Label("@tweag-credential-helper//plugin:helperfactory.go.tpl"),
    )
    credential_helper_plugin(
        name = "{}_cache".format(name),
        go_library = cache,
        type_name = cache_type_name,
        out = "{}_cache.go".format(name),
        template = Label("@tweag-credential-helper//plugin:cache.go.tpl"),
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
    installer = ctx.actions.declare_file(
        ctx.attr.name + ".link",
    )
    ctx.actions.symlink(
        output = installer,
        target_file = ctx.executable._installer_bin,
        is_executable=True,
    )

    runfiles = ctx.runfiles().merge_all([
        ctx.attr.credential_helper[DefaultInfo].default_runfiles,
        ctx.attr._installer_bin[DefaultInfo].default_runfiles,
    ])
    env = {
        "CREDENTIAL_HELPER_INSTALLER_SOURCE": ctx.expand_location(
            "$(rlocationpath {})".format(ctx.executable.credential_helper.owner),
            [ctx.attr.credential_helper],
        ),
    }
    for k, v in ctx.attr.env.items():
        env[k] = ctx.expand_location(v, [ctx.attr.credential_helper])

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
        "env": attr.string_dict(
            doc = """Environment variables to set for the test execution.
            The values (but not keys) are subject to
            [location expansion](https://bazel.build/reference/be/make-variables#predefined_label_variables) but not full
            """,
        ),
        "env_inherit": attr.string_list(
            doc = """Environment variables to inherit from the external environment.""",
        ),
        "_installer_bin": attr.label(
            executable = True,
            cfg = "target",
            default = Label("@tweag-credential-helper//installer:installer_bin"),
        ),
    },
    executable = True,
)

# TODO: allow using prebuilt credential_helper
# and have convenience method with hashes and URLs
# in release tar
# prebuilt_credential_helper = repository_rule(
#
# )
