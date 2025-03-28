PrebuiltHelperInfo = provider(
    doc = "A collection of prebuilt credential helper binaries",
    fields = {
        "helper": "Optional File of the prebuilt helper for the target platform. Will be None of no helper is available for the platform",
    },
)

def _prebuilt_helper_info_impl(ctx):
    return [PrebuiltHelperInfo(helper = ctx.file.helper)]

prebuilt_helper_info = rule(
    implementation = _prebuilt_helper_info_impl,
    attrs = {
        "helper": attr.label(
            mandatory = False,
            allow_single_file = True,
        ),
    },
    provides = [PrebuiltHelperInfo],
)

def _java_os_to_go_os(os):
    os = os.lower()
    if os in ["osx", "mac os x", "darwin"]:
        return "darwin"
    if os.startswith("windows"):
        return "windows"
    if os.startswith("linux"):
        return "linux"
    return os

def _java_arch_to_go_arch(arch):
    if arch in ["i386", "i486", "i586", "i686", "i786", "x86"]:
        return "386"
    if arch in ["amd64", "x86_64", "x64"]:
        return "amd64"
    if arch in ["aarch64", "arm64"]:
        return "arm64"

    # Some arches can be converted as-is, inlcuding
    # "ppc", "ppc64", "ppc64le", "s390x", "s390"
    return arch

def _prebuilt_collection_hub_repo_impl(rctx):
    select_arms = {"@rules_go//go/platform:" + k: v for (k, v) in rctx.attr.helpers.items()}
    select_arms |= {"//conditions:default": None}
    helper_rhs = "select({})".format(json.encode_indent(select_arms, prefix = "    ", indent = "    "))
    helper_rhs = helper_rhs.replace("null", "None")
    host_os_cpu = _java_os_to_go_os(rctx.os.name) + "_" + _java_arch_to_go_arch(rctx.os.arch)
    helper_available = host_os_cpu in rctx.attr.helpers.keys()
    availability_rhs = repr(helper_available)
    if len(rctx.attr.helpers) == 0:
        # empty select is illegal, replace with explicit None
        helper_rhs = "None"
    rctx.file(
        "BUILD.bazel",
        """
load("@bazel_skylib//rules:common_settings.bzl", "bool_setting")
load("@tweag-credential-helper//bzl/private/prebuilt:prebuilt.bzl", "prebuilt_helper_info")

prebuilt_helper_info(
    name = "prebuilt_helper_info",
    helper = {},
    visibility = ["//visibility:public"],
)

bool_setting(
    name = "prebuilt_available",
    build_setting_default = {},
    visibility = ["//visibility:public"],
)
""".format(helper_rhs, availability_rhs),
    )

prebuilt_collection_hub_repo = repository_rule(
    implementation = _prebuilt_collection_hub_repo_impl,
    attrs = {
        "helpers": attr.string_dict(),
    },
)

def _prebuilt_credential_helper_repo_impl(rctx):
    extension = "exe" if rctx.attr.os == "windows" else ""
    dot = "." if len(extension) > 0 else ""
    urls = [template.format(
        version = rctx.attr.version,
        os = rctx.attr.os,
        cpu = rctx.attr.cpu,
        dot = dot,
        extension = extension,
    ) for template in rctx.attr.url_templates]
    rctx.download(
        urls,
        output = "credential-helper.exe",
        executable = True,
        integrity = rctx.attr.integrity,
    )
    rctx.file(
        "BUILD.bazel",
        content = """exports_files(["credential-helper.exe"])""",
    )

_prebuilt_attrs = {
    "version": attr.string(mandatory = True),
    "integrity": attr.string(mandatory = True),
    "os": attr.string(values = ["darwin", "linux", "windows"]),
    "cpu": attr.string(values = ["386", "amd64", "arm64"]),
    "url_templates": attr.string_list(
        default = ["https://github.com/tweag/credential-helper/releases/download/{version}/credential_helper_{os}_{cpu}{dot}{extension}"],
    ),
}

prebuilt_credential_helper_repo = repository_rule(
    implementation = _prebuilt_credential_helper_repo_impl,
    attrs = _prebuilt_attrs,
)

_prebuilt_helper_collection = tag_class(attrs = {"name": attr.string(), "override": attr.bool(default = False)})
_prebuilt_helper_from_file = tag_class(attrs = {"collection": attr.string(), "file": attr.label()})
_prebuilt_helper_download = tag_class(attrs = {"collection": attr.string()} | _prebuilt_attrs)

def _lockfile_to_dict(lockfile, basename):
    requested_helpers = {}
    for item in lockfile:
        requested_helpers["%s_%s_%s" % (basename, item["os"], item["cpu"])] = item
    return requested_helpers

def _prebuilt_credential_helper_collection_for_module(ctx, mod):
    requested_helpers = {}
    collections = {}
    for collection_meta in mod.tags.collection:
        collections[collection_meta.name] = {"override": collection_meta.override, "helpers": {}}
    for from_file in mod.tags.from_file:
        lockfile = json.decode(ctx.read(from_file.file))
        helpers_from_lockfile = _lockfile_to_dict(lockfile, from_file.collection)
        requested_helpers.update(helpers_from_lockfile)
        for helper in helpers_from_lockfile.values():
            collections[from_file.collection]["helpers"][(helper["os"], helper["cpu"])] = "%s_%s_%s" % (from_file.collection, helper["os"], helper["cpu"])
    for download in mod.tags.download:
        name = "%s_%s_%s" % (download.collection, download.os, download.cpu)
        requested_helpers[name] = {member: getattr(download, member) for member in dir(download)}
        collections[download.collection]["helpers"][(download.os, download.cpu)] = "%s_%s_%s" % (from_file.collection, helper["os"], helper["cpu"])
    return (requested_helpers, collections)

def _prebuilt_credential_helpers(ctx):
    requested_helpers = {}
    collections = {}
    root_module = None
    for mod in ctx.modules:
        if mod.is_root:
            root_module = mod
            continue
        for_module = _prebuilt_credential_helper_collection_for_module(ctx, mod)
        for collection_name in for_module[1].keys():
            if collection_name in collections:
                fail("Duplicate definitions for prebuilt_credential_helpers %s. Only root module is allowed to override." % collection_name)
        requested_helpers.update(for_module[0])
        collections.update(for_module[1])
    root_module_direct_deps = []
    if root_module != None:
        for_root_module = _prebuilt_credential_helper_collection_for_module(ctx, root_module)
        for collection_name in for_root_module[1].keys():
            if collection_name in collections and not for_root_module[1][collection_name]["override"]:
                fail("Root module is redefining definition for prebuilt_credential_helpers %s. Set override to True if this is intended." % collection_name)
        requested_helpers.update(for_root_module[0])
        collections.update(for_root_module[1])
        root_module_direct_deps = for_root_module[1].keys()

    for item in requested_helpers.items():
        prebuilt_credential_helper_repo(
            name = item[0],
            **item[1]
        )
    for (collection_name, collection) in collections.items():
        helpers = {}
        for ((os, arch), helper_repo_name) in collection["helpers"].items():
            helpers["%s_%s" % (os, arch)] = "@%s//:credential-helper.exe" % helper_repo_name
        prebuilt_collection_hub_repo(
            name = collection_name,
            helpers = helpers,
        )

    return ctx.extension_metadata(
        root_module_direct_deps = root_module_direct_deps if ctx.root_module_has_non_dev_dependency else [],
        root_module_direct_dev_deps = [] if ctx.root_module_has_non_dev_dependency else root_module_direct_deps,
        reproducible = True,
    )

prebuilt_credential_helpers = module_extension(
    implementation = _prebuilt_credential_helpers,
    tag_classes = {
        "collection": _prebuilt_helper_collection,
        "from_file": _prebuilt_helper_from_file,
        "download": _prebuilt_helper_download,
    },
)
