# Static HTTP Credentials

The `http` helper sends a static secret for plain HTTP(S) downloads: HTTP Basic auth, a bearer token, or a raw header value.
It covers servers that take a fixed credential rather than a token exchange, such as Artifactory, Nexus, [Artifact Keeper][artifact-keeper], an HTTP cache, or Chainguard's private apk and Libraries repositories.

For container registries, use the [OCI helper](/docs/providers/oci.md); for gRPC remote execution and caching, use the [remoteapis helper](/docs/providers/remoteapis.md).

## Configuration

Add each host to your `.bazelrc`:

```
common --credential_helper=artifacts.example.com=%workspace%/tools/credential-helper
```

and route it to the `http` helper in `.tweag-credential-helper.json`:

- `.urls[].helper`: `"http"`
- `.urls[].config.auth_method`: how the secret is sent. Defaults to `"basic_auth"`.
  - `"basic_auth"`: the secret is `<user>:<password>` and is sent as `Authorization: Basic <base64>`.
  - `"bearer"`: the secret is sent as `Authorization: Bearer <secret>`.
  - `"header"`: the secret is sent verbatim in the header named by `header_name`.
- `.urls[].config.header_name`: the header for `"header"`.
- `.urls[].config.allow_missing`: when `true`, a missing secret means "send no header" instead of an error (see below). Defaults to `false`.
- `.urls[].config.lookup_chain`: the [lookup chain][lookup_chain] for the `default` secret binding. Defaults to the `CREDENTIAL_HELPER_HTTP_SECRET` environment variable, then the `tweag-credential-helper:http` keyring entry. Well-known hosts (below) have their own defaults.

Credentials are cached per host and first path segment, since servers often take a different credential per repository at the top of the path.
Give path-scoped entries with different credentials different first path segments.

## Pull-through caches and `allow_missing`

A pull-through cache is usually listed ahead of the origin URL, for example with Bazel's `--downloader_config` rewrites.
With `allow_missing`, a machine that does not have the credential sends an anonymous request, gets a `401` from the cache, and Bazel falls back to the next URL instead of failing the download.

Example for an [Artifact Keeper][artifact-keeper] instance, where the secret is a service account name and one of its API tokens:

```json
{
  "urls": [
    {
      "host": "artifacts.example.com",
      "helper": "http",
      "config": {
        "allow_missing": true,
        "lookup_chain": [
          {"source": "env", "name": "ARTIFACT_KEEPER_CREDENTIAL"}
        ]
      }
    }
  ]
}
```

## Chainguard apk and Libraries

`apk.cgr.dev` (private [Chainguard][chainguard] apk repositories, for example for [rules_apko][rules_apko]) and `libraries.cgr.dev` ([Chainguard Libraries][chainguard-libraries]) need no `.tweag-credential-helper.json` entry: the helper is picked for them automatically and reads `CGR_APK_CREDENTIAL` or `CGR_LIBRARIES_CREDENTIAL`.
You still need the `--credential_helper` lines in `.bazelrc`.

Both use a Chainguard pull token sent as HTTP Basic auth. Mint one with [`chainctl`][chainctl]:

```
$ chainctl auth pull-token --repository=apk --output=env --parent=<ORG>
```

It prints `CHAINGUARD_IDENTITY_ID` and `CHAINGUARD_TOKEN`.
The Basic username is the token's identity id, and each `chainctl auth pull-token` invocation mints a new identity, so the two always rotate together and are stored as one `<identity-id>:<token>` value:

```
$ export CGR_APK_CREDENTIAL="${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"
```

Chainguard Libraries serves each ecosystem as a top-level path (`/javascript/`, `/java/`, `/python/`, `/go/`), and each takes its own pull token (`--repository=javascript` and so on).
With more than one ecosystem, give each a path-scoped entry with its own lookup chain:

```json
{
  "urls": [
    {
      "host": "libraries.cgr.dev",
      "path": "/javascript*",
      "helper": "http",
      "config": {"lookup_chain": [{"source": "env", "name": "CGR_LIBRARIES_JAVASCRIPT_CREDENTIAL"}]}
    },
    {
      "host": "libraries.cgr.dev",
      "path": "/java/*",
      "helper": "http",
      "config": {"lookup_chain": [{"source": "env", "name": "CGR_LIBRARIES_JAVA_CREDENTIAL"}]}
    }
  ]
}
```

`/javascript*` also covers `/javascript-upstream/`, which takes the same token.

`apko lock` (for example `bazel run @rules_apko//apko lock ...`) downloads package indexes itself instead of using Bazel's downloader, so it does not use the credential helper.
Pass the credential to `apko` directly with its `HTTP_AUTH` environment variable:

```
$ export HTTP_AUTH="basic:apk.cgr.dev:${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"
```

[artifact-keeper]: https://github.com/artifact-keeper/artifact-keeper
[chainctl]: https://edu.chainguard.dev/chainguard/chainctl-usage/
[chainguard]: https://www.chainguard.dev/
[chainguard-libraries]: https://edu.chainguard.dev/chainguard/libraries/overview/
[lookup_chain]: /docs/lookup_chain.md
[rules_apko]: https://github.com/chainguard-dev/rules_apko
