PrebuiltHelperCollection = provider(
    doc = "A collection of prebuilt credential helper binaries",
    fields = {
        "platform_to_helper": "Dict from platform tuples (GOOS, GOARCH) to prebuilt helper binary (File)",
    },
)

def _prebuilt_collection_impl(ctx):
    platform_to_helper = {}
    for item in ctx.attr.helpers.items():
        os_arch = item[1].split("_")
        os = os_arch[0]
        arch = os_arch[1]
        default_info = item[0][DefaultInfo]
        files = default_info.files.to_list()
        if len(files) != 1:
            fail("expected single file for helper %s_%s of collection %s", (os, arch, ctx.attr.name))
        platform_to_helper[(os, arch)] = files[0]
    return [PrebuiltHelperCollection(platform_to_helper = platform_to_helper)]

prebuilt_collection = rule(
    implementation = _prebuilt_collection_impl,
    attrs = {
        "helpers": attr.label_keyed_string_dict(allow_files = True),
    },
    provides = [PrebuiltHelperCollection],
)

def _prebuilt_collection_hub_repo_impl(rctx):
    rctx.file(
        "BUILD.bazel",
        """load("@tweag-credential-helper//bzl/private/prebuilt:prebuilt.bzl", "prebuilt_collection")

prebuilt_collection(
    name = "collection",
    helpers = {},
    visibility = ["//visibility:public"],
)
""".format(json.encode({str(k): v for (k,v) in rctx.attr.helpers.items()})),
    )

prebuilt_collection_hub_repo = repository_rule(
    implementation = _prebuilt_collection_hub_repo_impl,
    attrs = {
        "helpers": attr.label_keyed_string_dict(allow_files = True),
    },
)

def _prebuilt_credential_helper_impl(rctx):
    urls = [template.format(
        version = rctx.attr.version,
        os = rctx.attr.os,
        cpu = rctx.attr.cpu,
    ) for template in rctx.attr.url_templates]
    rctx.download(
        urls,
        output = "helper",
        executable = True,
        integrity = rctx.attr.integrity
    )
    rctx.file(
        "BUILD.bazel",
        content = """exports_files(["helper"])""",
    )

_prebuilt_attrs = {
    "version": attr.string(mandatory = True),
    "integrity": attr.string(mandatory = True),
    "os": attr.string(values = ["darwin", "linux"]),
    "cpu": attr.string(values = ["386", "amd64", "arm64"]),
    "url_templates": attr.string_list(
        default = ["https://github.com/tweag/credential-helper/releases/download/{version}/helper_{os}_{cpu}"],
    ),
}

prebuilt_credential_helper = repository_rule(
    implementation = _prebuilt_credential_helper_impl,
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
        prebuilt_credential_helper(
            name = item[0],
            **item[1],
        )
    for (collection_name, collection) in collections.items():
        helpers_label_keyed_string_dict = {}
        for ((os, arch), helper_repo_name) in collection["helpers"].items():
            helpers_label_keyed_string_dict["@%s//:helper" % helper_repo_name] = "%s_%s" % (os, arch)
        prebuilt_collection_hub_repo(
            name = collection_name,
            helpers = helpers_label_keyed_string_dict,
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
