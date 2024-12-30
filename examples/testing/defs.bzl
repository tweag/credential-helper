def _check_file_hash_test_impl(ctx):
    test_binary = ctx.actions.declare_file(ctx.attr.name + ".exe")
    ctx.actions.symlink(
        output = test_binary,
        target_file = ctx.executable._tool,
        is_executable = True,
    )

    runfiles = ctx.runfiles(
        transitive_files =  depset(transitive = [data[DefaultInfo].files for data in ctx.attr.data]),
    ).merge_all([data[DefaultInfo].default_runfiles for data in ctx.attr.data])
    return [
        DefaultInfo(
            files = depset([test_binary]),
            runfiles = runfiles,
            executable = test_binary,
        ),
    ]

check_file_hash_test = rule(
    implementation = _check_file_hash_test_impl,
    attrs = {
        "data": attr.label_list(allow_files = True),
        "_tool": attr.label(
            executable = True,
            cfg = "target",
            default = Label(":testing"),
        ),
    },
    test = True,
)
