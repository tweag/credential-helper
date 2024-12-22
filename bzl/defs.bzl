load(
    "//bzl/private/plugin:plugin.bzl",
    _credential_helper = "credential_helper",
    _installer = "installer",
)
load(
    "//bzl/private/prebuilt:prebuilt.bzl",
    _prebuilt_credential_helpers = "prebuilt_credential_helpers",
)

credential_helper = _credential_helper
installer = _installer
prebuilt_credential_helpers = _prebuilt_credential_helpers
