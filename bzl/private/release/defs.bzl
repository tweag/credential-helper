load("@rules_pkg//pkg:mappings.bzl", "pkg_attributes")
load("@rules_pkg//pkg:providers.bzl", "PackageFilesInfo")
load("//bzl/private/config:defs.bzl", "ModuleVersionInfo")

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

# buildifier: disable=unused-variable
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
    GOOS_WINDOWS: [GOARCH_AMD64, GOARCH_ARM64],
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

ReleasePlatformInfo = provider(doc = "Holds information about a platform configuration", fields = ["os", "arch", "platform"])

def _release_platform_flag_impl(ctx):
    tup = _parse_platform_name(ctx.build_setting_value)
    if tup not in PLATFORMS:
        fail("unknown release platform %s" % ctx.build_setting_value)

    return ReleasePlatformInfo(os = tup[0], arch = tup[1], platform = Label(ctx.build_setting_value))

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
            "@tweag-credential-helper//bzl/private/release:release_platform": platform,
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
        "@tweag-credential-helper//bzl/private/release:release_platform",
    ],
)

DEFAULT_ATTRIBUTES = pkg_attributes(mode = "0644")
EXECUTABLE_ATTRIBUTES = pkg_attributes(mode = "0755")

OverrideSourceFilesInfo = provider(
    doc = """Provider representing overrides for a rules_pkg PackageFilesInfo""",
    fields = {
        "attributes": """Attribute information, represented as a `dict`.

Keys are strings representing attribute identifiers, values are
arbitrary data structures that represent the associated data.  These are
most often strings, but are not explicitly defined.

For known attributes and data type expectations, see the Common
Attributes documentation in the `rules_pkg` reference.
        """,
        "dest_src_map": """Map of file destinations to sources.

Sources are represented by bazel `File` structures.""",
    },
)

OfflineBuildDistdirInfo = provider(
    doc = """Provider representing the contents of a Bazel "--distdir".""",
    fields = {
        "basename_file_map": """Map of basename to File""",
        "files": "Depset of File whose basename shall be used as-is",
    },
)

BCRModuleVersionInfo = provider(
    doc = """Provider representing a version of a BCR module.""",
    fields = {
        "module_name": "Name of the module",
        "version": "The module version",
        "source_archive": "An archive File containing the module source",
        "source_archive_basename": "A basename for the source archive",
        "metadata_template": "A File containing a base template for metadata.json",
    },
)

def _release_files(ctx):
    output_group_info = {}
    version = ctx.attr.version[ModuleVersionInfo].version
    module_version = ctx.actions.declare_file("%s_module_version" % ctx.attr.name)
    git_tag = ctx.actions.declare_file("%s_git_tag" % ctx.attr.name)
    ctx.actions.write(module_version, content = version)
    ctx.actions.write(git_tag, content = "v" + version)
    output_group_info["version"] = depset([module_version, git_tag])
    lockfile_args = ctx.actions.args()
    lockfile_args.add("--version", version)
    dest_src_map = {
        "tools/credential-helper.sh": ctx.file.shell_stub,
    }
    attributes = {
        "tools/credential-helper.sh": EXECUTABLE_ATTRIBUTES,
    }
    distdir_contents = {}
    for platform in _platform_names:
        src = ctx.split_attr.executable[platform]
        executable = src[DefaultInfo].files_to_run.executable
        basename = ctx.attr.basename if len(ctx.attr.basename) > 0 else executable.basename

        # ensure we copy the extension from the executable (for Windows)
        dot_extension = ""
        if len(executable.extension) > 0 and not basename.endswith("." + executable.extension):
            dot_extension = "." + executable.extension
        filename_basename = "%s_%s%s" % (basename, platform, dot_extension)
        filename = "bin/" + filename_basename
        dest_src_map[filename] = executable
        attributes[filename] = EXECUTABLE_ATTRIBUTES
        distdir_contents[filename_basename] = executable
        output_group_info["%s_files" % platform] = depset([executable])
        lockfile_args.add("--helper", "%s=%s" % (platform, executable.path))
    lockfile = ctx.actions.declare_file("%s_lockfile.json" % ctx.attr.name)
    lockfile_args.add(lockfile)
    ctx.actions.run(
        outputs = [lockfile],
        inputs = [file[DefaultInfo].files_to_run.executable for file in ctx.attr.executable],
        executable = ctx.executable.lockfile_generator,
        arguments = [lockfile_args],
    )
    output_group_info["lockfile"] = depset([lockfile])

    return [
        DefaultInfo(files = depset(dest_src_map.values())),
        OutputGroupInfo(**output_group_info),
        PackageFilesInfo(attributes = attributes, dest_src_map = dest_src_map),
        OverrideSourceFilesInfo(
            attributes = {"prebuilt_lockfile.json": DEFAULT_ATTRIBUTES},
            dest_src_map = {"prebuilt_lockfile.json": lockfile},
        ),
        OfflineBuildDistdirInfo(
            basename_file_map = distdir_contents,
            files = depset(),
        ),
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
        "lockfile_generator": attr.label(
            executable = True,
            default = Label("//bzl/private/lockfile"),
            cfg = "exec",
        ),
        "version": attr.label(
            default = "@tweag-credential-helper-version",
            providers = [ModuleVersionInfo],
        ),
    },
)

def _offline_bundle_impl(ctx):
    mapped_contents = ctx.attr.distdir_contents[OfflineBuildDistdirInfo].basename_file_map
    extra_files = ctx.attr.distdir_contents[OfflineBuildDistdirInfo].files
    contents = {}
    for f in extra_files.to_list():
        contents[f.basename] = f
    for basename, f in mapped_contents.items():
        contents[basename] = f

    distdir_args = ctx.actions.args()
    for basename, f in contents.items():
        distdir_args.add("--file", "%s=%s" % (basename, f.path))

    distdir_tree_artifact = ctx.actions.declare_directory(ctx.attr.name + ".distdir")
    distdir_args.add(distdir_tree_artifact.path)
    ctx.actions.run(
        outputs = [distdir_tree_artifact],
        inputs = contents.values(),
        executable = ctx.executable.distdir_generator,
        arguments = [distdir_args],
    )

    return [DefaultInfo(files = depset([distdir_tree_artifact]))]

offline_bundle = rule(
    implementation = _offline_bundle_impl,
    attrs = {
        "distdir_contents": attr.label(
            providers = [OfflineBuildDistdirInfo],
            mandatory = True,
        ),
        "distdir_generator": attr.label(
            executable = True,
            default = Label("//bzl/private/distdir"),
            cfg = "exec",
        ),
    },
)

def _source_bundle_impl(ctx):
    attributes = {}
    dest_src_map = {}
    for file in ctx.files.srcs:
        if not file.is_source:
            fail("Bundling non-source file %s" % file.path)
        dest_src_map[file.path] = file
        attributes[file.path] = DEFAULT_ATTRIBUTES
        if file.extension in ["exe", "sh"] or file.path in ["tools/credential-helper"]:
            attributes[file.path] = EXECUTABLE_ATTRIBUTES
    for override in ctx.attr.overrides:
        override = override[OverrideSourceFilesInfo]
        attributes.update(override.attributes)
        dest_src_map.update(override.dest_src_map)
    return [
        DefaultInfo(files = depset(dest_src_map.values())),
        PackageFilesInfo(attributes = attributes, dest_src_map = dest_src_map),
    ]

source_bundle = rule(
    implementation = _source_bundle_impl,
    attrs = {
        "srcs": attr.label_list(allow_files = True),
        "overrides": attr.label_list(providers = [OverrideSourceFilesInfo]),
    },
)

def _versioned_filename_info_impl(ctx):
    file = ctx.file.src
    basename = file.basename
    destdir = ctx.attr.destdir
    slash = "/" if len(destdir) > 0 else ""
    extension = ctx.attr.extension if len(ctx.attr.extension) > 0 else file.extension
    dot = "." if len(extension) > 0 else ""
    path = file.path
    stem = basename.removesuffix(dot + extension)
    dest = ctx.attr.path_template.format(
        basename = basename,
        destdir = destdir,
        slash = slash,
        extension = extension,
        dot = dot,
        stem = stem,
        path = path,
        version = ctx.attr.version[ModuleVersionInfo].version,
    )
    dest_basename = ctx.attr.path_template.format(
        basename = basename,
        destdir = "",
        slash = "",
        extension = extension,
        dot = dot,
        stem = stem,
        path = path,
        version = ctx.attr.version[ModuleVersionInfo].version,
    )
    dest_src_map = {dest: file}
    return [
        DefaultInfo(files = depset(dest_src_map.values())),
        PackageFilesInfo(attributes = {dest: ctx.attr.attributes}, dest_src_map = dest_src_map),
        BCRModuleVersionInfo(
            module_name = "tweag-credential-helper",
            version = ctx.attr.version[ModuleVersionInfo].version,
            source_archive = ctx.file.src,
            source_archive_basename = dest_basename,
            metadata_template = ctx.file._metadata_template,
        ),
    ]

versioned_filename_info = rule(
    implementation = _versioned_filename_info_impl,
    attrs = {
        "src": attr.label(allow_single_file = True),
        "destdir": attr.string(),
        "extension": attr.string(),
        "path_template": attr.string(default = "{destdir}{slash}{stem}-v{version}{dot}{extension}"),
        "attributes": attr.string(),
        "version": attr.label(
            default = "@tweag-credential-helper-version",
            providers = [ModuleVersionInfo],
        ),
        "_metadata_template": attr.label(
            allow_single_file = True,
            default = "//:.bcr/metadata.template.json",
        ),
    },
)

def _offline_bcr_impl(ctx):
    bcr_info = ctx.attr.src_tar[BCRModuleVersionInfo]
    request = {
        "module_name": bcr_info.module_name,
        "version": bcr_info.version,
        "source_path": bcr_info.source_archive.path,
        "override_source_basename": bcr_info.source_archive_basename,
        "metadata_template_path": bcr_info.metadata_template.path,
    }
    request_file = ctx.actions.declare_file(ctx.attr.name + "_local_module_" + bcr_info.module_name + ".json")
    ctx.actions.write(request_file, content = json.encode(request))
    bcr_args = ctx.actions.args()
    bcr_args.add("--add-local-module", request_file.path)
    bcr_tree_artifact = ctx.actions.declare_directory(ctx.attr.name + ".local")
    bcr_args.add(bcr_tree_artifact.path)
    ctx.actions.run(
        outputs = [bcr_tree_artifact],
        inputs = [request_file, bcr_info.source_archive, bcr_info.metadata_template],
        executable = ctx.executable.bcr_generator,
        arguments = [bcr_args],
    )

    bazel_dep = ctx.actions.declare_file(ctx.attr.name + "_local_module_" + bcr_info.module_name + ".bazel_dep")
    ctx.actions.write(bazel_dep, content = """bazel_dep(
    name = "{name}",
    version = "{version}",
)
""".format(name = bcr_info.module_name, version = bcr_info.version))

    bcr = depset([bcr_tree_artifact])
    output_group_info = {
        "bcr": bcr,
        bcr_info.module_name: depset([bazel_dep]),
    }
    return [
        DefaultInfo(files = bcr),
        OutputGroupInfo(**output_group_info),
    ]

offline_bcr = rule(
    implementation = _offline_bcr_impl,
    attrs = {
        "src_tar": attr.label(
            providers = [BCRModuleVersionInfo],
            mandatory = True,
        ),
        "bcr_generator": attr.label(
            executable = True,
            default = Label("//bzl/private/bcr"),
            cfg = "exec",
        ),
    },
)

def _do_a_release_impl(ctx):
    out = ctx.actions.declare_file("do_a_release.link.sh")
    ctx.actions.symlink(
        output = out,
        target_file = ctx.file.script,
        is_executable = True,
    )
    env = {
        "EXPECTED_RELEASE_TAG": "v" + ctx.attr.version[ModuleVersionInfo].version,
        "MODULE_VERSION": ctx.attr.version[ModuleVersionInfo].version,
        "DIST_TAR": ctx.file.dist_tar.short_path,
    }
    inherited_environment = ["GH_TOKEN", "GITHUB_TOKEN"]
    return [
        DefaultInfo(
            files = depset([out]),
            runfiles = ctx.runfiles(files = [ctx.file.dist_tar]),
            executable = out,
        ),
        RunEnvironmentInfo(env, inherited_environment),
    ]

do_a_release = rule(
    implementation = _do_a_release_impl,
    attrs = {
        "script": attr.label(
            default = ":do_a_release.sh",
            allow_single_file = True,
        ),
        "dist_tar": attr.label(
            default = ":dist_tar",
            allow_single_file = True,
        ),
        "version": attr.label(
            default = "@tweag-credential-helper-version",
            providers = [ModuleVersionInfo],
        ),
    },
    executable = True,
)
