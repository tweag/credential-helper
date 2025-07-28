"""Analysis tests for the installer
See https://bazel.build/rules/testing#testing-rules
"""

load("@bazel_skylib//lib:partial.bzl", "partial")
load("@bazel_skylib//lib:unittest.bzl", "analysistest", "asserts", "unittest")
load(":depdends_on.bzl", "DependsOnInfo")

def _define_dependency_test(*, should_depend, build_mode):
    def _installer_depends_on_source_helper_test_impl(ctx):
        env = analysistest.begin(ctx)
        target_under_test = analysistest.target_under_test(env)
        asserts.equals(env, should_depend, target_under_test[DependsOnInfo].DependencyFound)
        expected_chain_length = 0
        if should_depend:
            expected_chain_length = 2
        asserts.equals(env, expected_chain_length, len(target_under_test[DependsOnInfo].Chain))
        return analysistest.end(env)

    return analysistest.make(
        _installer_depends_on_source_helper_test_impl,
        config_settings = {
            str(Label("@tweag-credential-helper//bzl/config:helper_build_mode")): build_mode,
        },
    )

_helper_depends_on_source_in_source_mode_test = _define_dependency_test(should_depend = True, build_mode = "from_source")
_helper_no_dep_on_source_in_auto_mode_test = _define_dependency_test(should_depend = False, build_mode = "auto")
_helper_no_dep_on_source_in_prebuilt_mode_test = _define_dependency_test(should_depend = False, build_mode = "prebuilt")

def installer_test_suite(name, target_under_test):
    """Generate test suite and test targets for installer dependency tests.

    Args:
      name: String, a unique name for the test-suite target.
      target_under_test: String, the target that will be tested.
    """
    unittest.suite(
        name,
        partial.make(_helper_depends_on_source_in_source_mode_test, target_under_test = target_under_test, size = "small"),
        partial.make(_helper_no_dep_on_source_in_auto_mode_test, target_under_test = target_under_test, size = "small"),
        partial.make(_helper_no_dep_on_source_in_prebuilt_mode_test, target_under_test = target_under_test, size = "small"),
    )
