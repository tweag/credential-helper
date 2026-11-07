# Chainguard Libraries Authentication

This document explains how to setup your system for authenticating to [Chainguard Libraries][chainguard-libraries] (`libraries.cgr.dev`) using the credential helper, for example to fetch npm tarballs, Maven artifacts or Go modules through Bazel's downloader.

Chainguard Libraries serves each ecosystem as a top-level path of `libraries.cgr.dev` (`/javascript/`, `/java/`, `/python/`, `/go/`, ...) behind HTTP Basic authentication, and each ecosystem takes its own pull token.

## Obtaining a pull token

Mint a pull token for each ecosystem you use with [`chainctl`][chainctl], for example for JavaScript:

```
$ chainctl auth pull-token --repository=javascript --output=env --parent=<ORG>
```

This prints `CHAINGUARD_IDENTITY_ID` and `CHAINGUARD_TOKEN`.
As for the [apk helper](/docs/providers/chainguard-apk.md), the identity id and token rotate together, so the helper expects both as a single credential of the form `<identity-id>:<token>`:

```
$ echo -n "${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"
```

## Authentication Methods

By default, the helper reads the credential from the `CGR_LIBRARIES_CREDENTIAL` environment variable:

```
$ export CGR_LIBRARIES_CREDENTIAL="${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"
```

That is enough when you use a single ecosystem. With more than one, give each its own path-scoped entry and [lookup chain][lookup_chain] (see the example below). Credentials are cached per host and first path segment, so the token for one ecosystem is never served for another.

## Configuration

Add to your `.bazelrc`:

```
common --credential_helper=libraries.cgr.dev=%workspace%/tools/credential-helper
```

Optionally, configure the helper and its lookup chain in `.tweag-credential-helper.json`:

- `.urls[].helper`: `"chainguard-libraries"` (name of the helper)
- `.urls[].config.lookup_chain`: The [lookup chain][lookup_chain] used to find the `default` secret binding (the `<identity-id>:<token>` credential). Defaults to:
  ```json
  [{"source": "env", "name": "CGR_LIBRARIES_CREDENTIAL", "binding": "default"}]
  ```

Example with one token per ecosystem:

```json
{
  "urls": [
    {
      "host": "libraries.cgr.dev",
      "path": "/javascript*",
      "helper": "chainguard-libraries",
      "config": {
        "lookup_chain": [
          {"source": "env", "name": "CGR_LIBRARIES_JAVASCRIPT_CREDENTIAL"}
        ]
      }
    },
    {
      "host": "libraries.cgr.dev",
      "path": "/java/*",
      "helper": "chainguard-libraries",
      "config": {
        "lookup_chain": [
          {"source": "env", "name": "CGR_LIBRARIES_JAVA_CREDENTIAL"}
        ]
      }
    }
  ]
}
```

`/javascript*` also covers `/javascript-upstream/`, which serves packages Chainguard has not rebuilt and takes the same token.

[chainctl]: https://edu.chainguard.dev/chainguard/chainctl-usage/
[chainguard-libraries]: https://edu.chainguard.dev/chainguard/libraries/overview/
[lookup_chain]: /docs/lookup_chain.md
