# Chainguard apk Repository Authentication

This document explains how to setup your system for authenticating to a private [Chainguard][chainguard] apk repository (`apk.cgr.dev`) using the credential helper, for example to fetch `APKINDEX.tar.gz` and `*.apk` files for [rules_apko][rules_apko].

Chainguard's apk repositories serve flat files behind HTTP Basic authentication, so the [OCI helper](/docs/providers/oci.md) (which is used for the `cgr.dev` container registry) does not apply.

## Obtaining a pull token

Mint a pull token with [`chainctl`][chainctl]:

```
$ chainctl auth pull-token --repository=apk --output=env --parent=<ORG>
```

This prints `CHAINGUARD_IDENTITY_ID` and `CHAINGUARD_TOKEN`.
The HTTP Basic username for a pull token is its identity id, and each invocation of `chainctl auth pull-token` mints a new identity, so the identity id and token always rotate together.
For this reason, the helper expects both as a single credential of the form `<identity-id>:<token>`:

```
$ echo -n "${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"
```

## Authentication Methods

By default, the helper reads the credential from the `CGR_APK_CREDENTIAL` environment variable:

```
$ export CGR_APK_CREDENTIAL="${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"
```

Like most helpers, the source of the credential can be changed using a [lookup chain][lookup_chain], for example to read it from the system keyring or from Google Cloud Secret Manager.

## Configuration

Add to your `.bazelrc`:

```
common --credential_helper=apk.cgr.dev=%workspace%/tools/credential-helper
```

Optionally, configure the helper and its lookup chain in `.tweag-credential-helper.json`:

- `.urls[].helper`: `"chainguard-apk"` (name of the helper)
- `.urls[].config.lookup_chain`: The [lookup chain][lookup_chain] used to find the `default` secret binding (the `<identity-id>:<token>` credential). Defaults to:
  ```json
  [{"source": "env", "name": "CGR_APK_CREDENTIAL", "binding": "default"}]
  ```

Example:

```json
{
  "urls": [
    {
      "host": "apk.cgr.dev",
      "helper": "chainguard-apk",
      "config": {
        "lookup_chain": [
          {
            "source": "keyring",
            "service": "tweag-credential-helper:chainguard-apk"
          },
          {
            "source": "env",
            "name": "CGR_APK_CREDENTIAL"
          }
        ]
      }
    }
  ]
}
```

## Limitations

`apko lock` (for example `bazel run @rules_apko//apko lock ...`) downloads package indexes itself instead of using Bazel's downloader, so it does not use the credential helper.
Pass the credential to `apko` directly using its `HTTP_AUTH` environment variable:

```
$ export HTTP_AUTH="basic:apk.cgr.dev:${CHAINGUARD_IDENTITY_ID}:${CHAINGUARD_TOKEN}"
```

[chainctl]: https://edu.chainguard.dev/chainguard/chainctl-usage/
[chainguard]: https://www.chainguard.dev/
[lookup_chain]: /docs/lookup_chain.md
[rules_apko]: https://github.com/chainguard-dev/rules_apko
