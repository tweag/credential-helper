# Remote Build Execution (RBE) services / remote API Authentication

This document explains how to setup your system for authenticating to remote build execution (RBE) services that are based on [remote api gRPC protocols][remote-apis] for Bazel, Buck2, BuildStream, Reclient, Goma Server, Pants, Please, Recc, Soong, and more.

Remote APIs include the following:

- Remote Execution
- Remote Caching
- Build Event UI
- Remote Assets / Remote Downloads

... and probably more that are offered by a wide range of software and SaaS solutions.

When using one of the following services, you can directly jump to the matching setup steps:

- [BuildBuddy Cloud](#section-buildbuddy-cloud)
- [Self-hosted BuildBuddy](#section-buildbuddy-self-hosted)
- [BuildBarn](#section-buildbarn)
- [bazel-remote](#section-bazel-remote)
- [Google Cloud Storage (GCS) bucket as HTTP/1.1 cache](/docs/providers/gcs.md)


## Configuration

Configuration depends on the service and authentication mechanism used.
While mTLS cannot be setup using a credential helper, any authentication scheme based on HTTP headers should work.

The configuration in `.tweag-credential-helper.json` supports the following values:

- `.urls[].helper`: `"remoteapis"` (name of the helper)
- `.urls[].config.auth_method`: one of
    - `"header"`: Default. Send a HTTP header with the value being the default secret.
    - `"basic_auth"` Used by `bazel-remote`. Send the default secret containing username and password (`username:password`) as a basic auth header.
- `.urls[].config.header_name`: Name of the HTTP header used for authentication. Example: use `"x-buildbuddy_api_key"` for BuildBuddy.
- `.urls[].config.lookup_chain`: The [lookup chain][lookup_chain] used to find the `default` secret. Defaults to:
    ```json
    [
        {
            "source": "env",
            "name": "CREDENTIAL_HELPER_REMOTEAPIS_SECRET",
            "binding": "default"
        },
        {
            "source": "keyring",
            "name": "tweag-credential-helper:remoteapis",
            "binding": "default"
        }
    ]
    ```


### <a name="section-buildbuddy-cloud"></a> BuildBuddy Cloud (remote.buildbuddy.io)

Add to your `.bazelrc`:

```
common --credential_helper=remote.buildbuddy.io=%workspace%/tools/credential-helper
```

BuildBuddy uses a the `x-buildbuddy-api-key` HTTP header for authentication.
Visit the [setup page][buildbuddy-setup], and copy the secret after `x-buildbuddy-api-key=`.

If you are not using a configuration file, you can authenticate with an environment variable or a keyring secret:

- Set `$BUILDBUDDY_API_KEY` to the value of the `x-buildbuddy-api-key` HTTP header.
- Set the `tweag-credential-helper:buildbuddy_api_key` secret to the value of the `x-buildbuddy-api-key` HTTP header:

    ```
    $ echo -ne "$BUILDBUDDY_API_KEY" | tools/credential-helper setup-keyring tweag-credential-helper:buildbuddy_api_key
    ```

If you need to customize the HTTP header or secret used, read the next section on setting up self-hosted BuildBuddy instead:

### <a name="section-buildbuddy-self-hosted"></a> Self-hosted BuildBuddy

In the following snippets, we assume that your BuildBuddy instance is hosted under `buildbuddy.acme.com`. Replace this hostname with your own.

Add to your `.bazelrc`:

```
common --credential_helper=buildbuddy.acme.com=%workspace%/tools/credential-helper
```

Add to your `.tweag-credential-helper.json`:
```json
{
    "urls": [
        {
            "host": "buildbuddy.acme.com",
            "helper": "remoteapis",
            "config": {
                "auth_method": "header",
                "header_name": "x-buildbuddy-api-key",
                "lookup_chain": [
                    {
                        "source": "env",
                        "name": "BUILDBUDDY_API_KEY"
                    },
                    {
                        "source": "keyring",
                        "service": "tweag-credential-helper:buildbuddy_api_key"
                    }
                ]
            }
        }
    ]
}
```

### <a name="section-buildbarn"></a> BuildBarn

BuildBarn supports a variety of authentication mechanisms specified in the Jsonnet key `authenticationPolicy`.
Only the polcies `jwt` and `remote` can be used to authenticate using HTTP headers (at the time of writing).
In the following snippets, we assume that your BuildBarn instance is hosted under `buildbarn.acme.com`. Replace this hostname with your own.

#### JWT authentication

Configure the BuildBarn Jsonnet for `jwt` (more setup needed - setting up and distributing keys is out of the scope of this document). It is also assumed that the user of Bazel already has access to a jwt in the `$BUILDBARN_API_KEY` environment variable, which must be encoded as follows: `Bearer <TOKEN>`.

```Jsonnet
authenticationPolicy: {
  jwt: {
    jwksFile: "some/file/path.jwks"
    ...
  }
}
```

#### Custom auth middleware / remote auth

Configure the BuildBarn Jsonnet for `remote` (more setup needed - setting up the authentication middleware is out of the scope of this document). We assume that `x-buildbarn-api-key` is the header forwarded to the authentication middleware. It is also assumed that the user of Bazel already has access to a token in the `$BUILDBARN_API_KEY` environment variable, which must be encoded as-is.
Replace the endpoint address with the address of your custom authentication middleware.

```Jsonnet
authenticationPolicy: {
  remote: {
    headers: ["x-buildbarn-api-key"]
    endpoint: {
      address: "address:port"
      ...
    }
    ...
  }
}
```

#### Bazel and credential-helper Configuration

Add to your `.bazelrc`:

```
common --credential_helper=buildbarn.acme.com=%workspace%/tools/credential-helper
```

Add to your `.tweag-credential-helper.json`:
```json
{
    "urls": [
        {
            "host": "buildbarn.acme.com",
            "helper": "remoteapis",
            "config": {
                "auth_method": "header",
                "header_name": "Authorization",
                "lookup_chain": [
                    {
                        "source": "env",
                        "name": "BUILDBARN_API_KEY"
                    },
                    {
                        "source": "keyring",
                        "service": "tweag-credential-helper:buildbarn_api_key"
                    }
                ]
            }
        }
    ]
}
```

When using the system keyring, login with the following command:

```
$ echo -ne "$BUILDBARN_API_KEY" | tools/credential-helper setup-keyring tweag-credential-helper:buildbarn_api_key
```

### <a name="section-bazel-remote"></a> bazel-remote

The only header-based authentication scheme [bazel-remote][bazel-remote] supports at the time of writing is basic auth (username and password).

In the following snippets, we assume that your bazel-remote instance is hosted under `bazel-remote.acme.com`. Replace this hostname with your own.
Additionally, we assume that the user already created a `.htpasswd` file under `/etc/bazel-remote/.htpasswd` for bazel-remote that contains credentials for the user.

Add to your bazel-remote configuration yaml:

```yaml
htpasswd_file: /etc/bazel-remote/.htpasswd
```

Add to your `.bazelrc`:

```
common --credential_helper=bazel-remote.acme.com=%workspace%/tools/credential-helper
```

Add to your `.tweag-credential-helper.json`:
```json
{
    "urls": [
        {
            "host": "bazel-remote.acme.com",
            "helper": "remoteapis",
            "config": {
                "auth_method": "basic_auth",
                "lookup_chain": [
                    {
                        "source": "env",
                        "name": "CREDENTIAL_HELPER_REMOTEAPIS_SECRET"
                    },
                    {
                        "source": "keyring",
                        "service": "tweag-credential-helper:remoteapis"
                    }
                ]
            }
        }
    ]
}
```

Users can either export the `$CREDENTIAL_HELPER_REMOTEAPIS_SECRET` environment variable, or login with the system keyring using the following command:

```
$ echo -ne "username:password" | tools/credential-helper setup-keyring tweag-credential-helper:remoteapis
```


[remote-apis]: https://github.com/bazelbuild/remote-apis
[buildbuddy-setup]: https://app.buildbuddy.io/docs/setup/
[lookup_chain]: /docs/lookup_chain.md
[bazel-remote]: https://github.com/buchgr/bazel-remote
