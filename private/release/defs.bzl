load("@rules_pkg//pkg:providers.bzl", "PackageFilesInfo")

GOOS_LINUX = "linux"
GOOS_DARWIN = "darwin"
GOOS_WINDOWS = "windows"

GOARCH_386 = "386"
GOARCH_AMD64 = "amd64"
GOARCH_ARM64 = "arm64"
GOARCH_RISCV64 = "riscv64"

go_to_constraint_value = {
    GOOS_LINUX: "@platforms//os:linux",
    GOOS_DARWIN: "@platforms//os:macos",
    GOOS_WINDOWS: "@platforms//os:windows",
    GOARCH_386: "@platforms//cpu:x86_32",
    GOARCH_AMD64: "@platforms//cpu:x86_64",
    GOARCH_ARM64: "@platforms//cpu:arm64",
    GOARCH_RISCV64: "@platforms//cpu:riscv64",
}

_goos_list = [
    GOOS_LINUX,
    GOOS_DARWIN,
    GOOS_WINDOWS,
]

_goarch_list = [
    GOARCH_386,
    GOARCH_AMD64,
    GOARCH_ARM64,
    # TODO: fix rules_go upstream:
    # add riscv64 to BAZEL_GOARCH_CONSTRAINTS
    GOARCH_RISCV64,
]

_os_to_arches = {
   GOOS_LINUX: [GOARCH_386, GOARCH_AMD64, GOARCH_ARM64],
   GOOS_DARWIN: [GOARCH_AMD64, GOARCH_ARM64],
   # TODO: fix Windows build
   GOOS_WINDOWS: [],
}

def _generate_platforms():
    platforms = []
    for os in _goos_list:
        for arch in _os_to_arches[os]:
            platforms.append((os, arch))
    return platforms

def platform_name(tup):
    return tup[0] + "_" + tup[1]

def _parse_platform_name(name):
    return tuple(name.split("_"))

PLATFORMS = _generate_platforms()

_platform_names = [platform_name(platform) for platform in PLATFORMS]

ReleasePlatform = provider(fields = ["os", "arch", "platform"])

def _release_platform_flag_impl(ctx):
    tup = _parse_platform_name(ctx.build_setting_value)
    if tup not in PLATFORMS:
        fail("unknown release platform %s" % ctx.build_setting_value)

    return ReleasePlatform(os = tup[0], arch = tup[1], platform = Label(ctx.build_setting_value))

release_platform_flag = rule(
    implementation = _release_platform_flag_impl,
    build_setting = config.string(flag = True),
)

def _release_platforms_transition_impl(_settings, _attr):
    return {
        platform: {
            "//command_line_option:platforms": str(Label(platform)),
            "//command_line_option:strip": "always",
            "//command_line_option:compilation_mode": "opt",
            "@rules_go//go/config:pure": True,
            "@tweag-credential-helper//private/release:release_platform": platform,
        }
        for platform in _platform_names
    }


release_platforms_transition = transition(
    implementation = _release_platforms_transition_impl,
    inputs = [],
    outputs = [
        "//command_line_option:platforms",
        "//command_line_option:strip",
        "//command_line_option:compilation_mode",
        "@rules_go//go/config:pure",
        "@tweag-credential-helper//private/release:release_platform",
    ],
)

def _release_files(ctx):
    dest_src_map = {
        "tools/credential-helper": ctx.file.shell_stub,
    }
    output_group_info = {}
    for platform in _platform_names:
        src = ctx.split_attr.executable[platform]
        executable = src[DefaultInfo].files_to_run.executable
        basename = ctx.attr.basename if len(ctx.attr.basename) > 0 else executable.basename
        dest_src_map["bin/%s_%s" % (basename, platform)] = executable
        output_group_info["%s_files" % platform] = depset([executable])

    return [
        DefaultInfo(files = depset(dest_src_map.values())),
        OutputGroupInfo(**output_group_info),
        PackageFilesInfo(attributes = {}, dest_src_map = dest_src_map),
    ]

release_files = rule(
   implementation = _release_files,
   attrs = {
        "executable": attr.label(
            cfg = release_platforms_transition,
            mandatory = True,
        ),
        "basename": attr.string(),
        "shell_stub": attr.label(
            mandatory = True,
            allow_single_file = True,
        ),
    },
)

def _source_bundle(ctx):
    dest_src_map = {}
    for file in ctx.files.srcs:
        if not file.is_source:
            fail("Bundling non-source file %s" % file.path)
        dest_src_map[file.path] = file
    return [
        DefaultInfo(files = depset(dest_src_map.values())),
        PackageFilesInfo(attributes = {}, dest_src_map = dest_src_map),
    ]

source_bundle = rule(
   implementation = _source_bundle,
   attrs = {"srcs": attr.label_list(allow_files = True)},
)
