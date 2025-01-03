# OCI / Container Registry Authentication

This document explains how to setup your system for authenticating to various container registries using the credential helper.
The credential helper supports many container registries out of the box and can easily be configured or extended to do more.

## Configuration

Add to your `.bazelrc`:

```
# replace this with your registry of choice
common --credential_helper=index.docker.io=%workspace%/tools/credential-helper
```

### Usage with rules_oci

`rules_oci` tries to perform its own credential handling, but can be configured to use a credential helper instead.
Set the following in your `.bazelrc`: `common --repo_env=OCI_DISABLE_GET_TOKEN=1` to let the credential helper inject authentication headers.

## Default set of allowed registries

By default, the credential helper only allows accessing registries that are well-known.
This is a safety precaution which can be relaxed using the `$CREDENTIAL_HELPER_GUESS_OCI_REGISTRY` environment variable.
If your registry of choice is publicly accessible, consider adding it to the list:

- `*.app.snowflake.com`
- `*.azurecr.io`
- `cgr.dev`
- `docker.elastic.co`
- `ghcr.io`
- `index.docker.io`
- `nvcr.io`
- `public.ecr.aws`
- `quay.io`
- `registry.gitlab.com`

## Default flow

If no custom logic exists to obtain tokens for a specific registry,
the helper parses you docker config (`~/.docker/config.json`) to obtain credentials for registries.
This allows you to use any registry that can be used via `docker pull`, simply by configuring it in advance with `docker login`.

## Custom implementations

For selected registries, the credential helper implements custom logic for obtaining tokens.

### GitHub packages / `ghcr.io`

For the GitHub container registry, the credential helper uses the same token flow that is also used for the [GitHub api][doc-github].
You can use a different token for `ghcr.io` by setting the `$GHCR_TOKEN` environment variable.

[doc-github]: /docs/providers/github.md