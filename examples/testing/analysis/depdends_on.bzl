DependsOnInfo = provider(
    doc = "Tracks whether a target has a transitive dependency on @tweag-credential-helper//:tweag-credential-helper",
    fields = {
        "DependencyFound": "True iff any transitive dependency is @tweag-credential-helper//:tweag-credential-helper",
        "Chain": "The chain of dependencies that led to the dependency on @tweag-credential-helper//:tweag-credential-helper",
    },
)

def _depdends_on_aspect_impl(target, ctx):
    if target.label == ctx.attr._needle.label:
        # base case: we are at the needle
        return [DependsOnInfo(DependencyFound = True, Chain = [target.label])]
    for attr_name in dir(ctx.rule.attr):
        attr_value = getattr(ctx.rule.attr, attr_name)
        maybe_found = handle_provider_in_attr(target.label, attr_value, type(ctx.attr._needle))
        if maybe_found != None:
            return maybe_found
    return [DependsOnInfo(DependencyFound = False, Chain = [])]

def handle_provider_in_attr(label, attr_value, target_type):
    """Find all dependencies of the rule of type "Target".

    Those can come from:
     - attr.label (single Target)
     - attr.label_list (list of Targets)
     - attr.label_keyed_string_dict (dict of Target -> string)
     - attr.string_keyed_label_dict (dict of string -> Target)

    Args:
        label: String, The label of the current target being analyzed for dependencies.
        attr_value: String, The attribute value to inspect.
        target_type: String, The type to match against.

    Returns:
        A DependsOnInfo provider liste if a dependency chain is found or None if no dependency is found.
    """
    if type(attr_value) == target_type:
        if DependsOnInfo in attr_value:
            dep_info = attr_value[DependsOnInfo]
            if dep_info.DependencyFound:
                return [DependsOnInfo(DependencyFound = True, Chain = [label] + dep_info.Chain)]
    elif type(attr_value) == type([]):
        for item in attr_value:
            if type(item) == target_type and DependsOnInfo in item:
                dep_info = item[DependsOnInfo]
                if dep_info.DependencyFound:
                    return [DependsOnInfo(DependencyFound = True, Chain = [label] + dep_info.Chain)]
    elif type(attr_value) == type({}):
        for key, item in attr_value.items():
            if type(item) == target_type and DependsOnInfo in item:
                dep_info = item[DependsOnInfo]
                if dep_info.DependencyFound:
                    return [DependsOnInfo(DependencyFound = True, Chain = [label] + dep_info.Chain)]
            if type(key) == target_type and DependsOnInfo in key:
                dep_info = key[DependsOnInfo]
                if dep_info.DependencyFound:
                    return [DependsOnInfo(DependencyFound = True, Chain = [label] + dep_info.Chain)]
    return None

depdends_on_aspect = aspect(
    implementation = _depdends_on_aspect_impl,
    attr_aspects = ["*"],
    attrs = {
        "_needle": attr.label(default = "@tweag-credential-helper//:tweag-credential-helper"),
    },
)

def _depdends_on_impl(ctx):
    dep_info = ctx.attr.target[DependsOnInfo]
    return [dep_info]

depdends_on = rule(
    implementation = _depdends_on_impl,
    attrs = {
        "target": attr.label(aspects = [depdends_on_aspect]),
    },
)
