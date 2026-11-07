# Artifact Keeper Authentication

This document explains how to setup your system for authenticating to an [Artifact Keeper][artifact-keeper] instance using the credential helper, for example when Artifact Keeper is a pull-through cache in front of Maven Central, npm, Go module proxies or other registries.

Artifact Keeper is self-hosted, so the helper has no fixed host: it serves whatever host you route to it.

## Obtaining a credential

Artifact Keeper accepts HTTP Basic auth with a user name and password, or with a service account name and one of its API tokens.
For read-only clients such as CI, create a service account with read scopes and mint an API token for it.
The helper expects the credential as a single string of the form `<user>:<token>`.

## Authentication Methods

By default, the helper reads the credential from the `ARTIFACT_KEEPER_CREDENTIAL` environment variable:

```
$ export ARTIFACT_KEEPER_CREDENTIAL="<service-account>:<token>"
```

Like most helpers, the source of the credential can be changed using a [lookup chain][lookup_chain], for example to read it from the system keyring or from Google Cloud Secret Manager.

If no credential is found, the helper sends no `Authorization` header instead of failing.
Artifact Keeper is usually listed ahead of the origin URL (for example with Bazel's `--downloader_config` rewrites), so an anonymous request that gets a `401` lets Bazel fall back to the origin, and machines without the credential still build.

## Configuration

Add to your `.bazelrc`, with your instance's host:

```
common --credential_helper=artifacts.example.com=%workspace%/tools/credential-helper
```

Configure the helper for that host in `.tweag-credential-helper.json`:

- `.urls[].helper`: `"artifact-keeper"` (name of the helper)
- `.urls[].config.lookup_chain`: The [lookup chain][lookup_chain] used to find the `default` secret binding (the `<user>:<token>` credential). Defaults to:
  ```json
  [{"source": "env", "name": "ARTIFACT_KEEPER_CREDENTIAL", "binding": "default"}]
  ```

Example:

```json
{
  "urls": [
    {
      "host": "artifacts.example.com",
      "helper": "artifact-keeper",
      "config": {
        "lookup_chain": [
          {
            "source": "keyring",
            "service": "tweag-credential-helper:artifact-keeper"
          },
          {
            "source": "env",
            "name": "ARTIFACT_KEEPER_CREDENTIAL"
          }
        ]
      }
    }
  ]
}
```

[artifact-keeper]: https://github.com/artifact-keeper/artifact-keeper
[lookup_chain]: /docs/lookup_chain.md
