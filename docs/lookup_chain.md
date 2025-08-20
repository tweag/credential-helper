# Secret lookup chains

The credential helper needs to be flexible when obtaining secrets from the environment.
To support different users with different needs, the configuration file `.tweag-credential-helper.json` allows you to specify where to read secrets from, including the order of preference.

## Setup steps

While some services require specific setup steps, you can login to most services with the following generic steps.
The credential helper tries to explain how to login, based on a given uri. Examples:

```
$ credential-helper setup-uri https://github.com/my-org/project/releases/download/v1.2.3/my-artifact.tar.gz
$ credential-helper setup-uri https://raw.githubusercontent.com/my-org/project/6012...a5de28/file.txt
$ credential-helper setup-uri https://storage.googleapis.com/bucket/path/to/object
$ credential-helper setup-uri https://my-bucket.s3.amazonaws.com/path/to/object
$ credential-helper setup-uri https://org-id.r2.cloudflarestorage.com/bucket/path/to/object
$ credential-helper setup-uri https://index.docker.io/v2/library/hello-world/blobs/sha256:d2c94e...7264ac5a
```

When using an environment variable, simply `export` the secret value in your shell, like this (replace `SECRET_NAME` with the actual environment variable used for authentication and `secret_value` with the real secret):

```
$ export SECRET_NAME=secret_value
```

Please note that environment variables easily leak by accident. It is generally more desirable to use a dedicated store for sensitive values. For this purpose, the credential helper can read secrets from the system keyring.
When using the system keyring, you need to know the service name that is used to retrieve the secret.
Simply run the following command to login (replace `secret_value` with the actual secret and `[service-name]` with the name of the secret):

```
$ echo -ne "secret_value" | tools/credential-helper setup-keyring [service-name]
```


## Configuration

Most helpers support lookup chains, unless specifically noted otherwise.
When configuring the helper for a url, you can also define lookup chains.

The lookup chain is an array where each entry specifies a source to try in order. The first successful lookup wins.

### Environment Variable Source

When reading secrets from environment variables:

- `.urls[].config.lookup_chain[].source`: `"env"` - Source of the secret (environment variable)
- `.urls[].config.lookup_chain[].name`: Name of the environment variable to read
- `.urls[].config.lookup_chain[].binding`: Optional binding to a specific secret. If unspecified, it binds to the `"default"` secret.

Example:
```json
{
  "source": "env",
  "name": "GITHUB_TOKEN",
  "binding": "default"
}
```

### Keyring Source

When reading secrets from the system keyring:

- `.urls[].config.lookup_chain[].source`: `"keyring"` - Source of the secret (system keyring)
- `.urls[].config.lookup_chain[].service`: Service name used to store the secret in the keyring
- `.urls[].config.lookup_chain[].binding`: Optional binding to a specific secret. If unspecified, it binds to the `"default"` secret.

Example:
```json
{
  "source": "keyring",
  "service": "github-pat",
  "binding": "default"
}
```

### Static Source

For hardcoded values (use with caution):

- `.urls[].config.lookup_chain[].source`: `"static"` - Source of the secret (static value)
- `.urls[].config.lookup_chain[].name`: The static value to return
- `.urls[].config.lookup_chain[].binding`: Optional binding to a specific secret. If unspecified, it binds to the `"default"` secret.

Example:
```json
{
  "source": "static",
  "name": "hardcoded-token-value",
  "binding": "default"
}
```

### Google Source

For Google Cloud authentication:

- `.urls[].config.lookup_chain[].source`: `"google"` - Source of the secret (Google Cloud credentials)
- `.urls[].config.lookup_chain[].token_type`: Type of token to mint: `"access"` (default) or `"id"`/`"jwt"`
  - `"access"`: Returns a Google OAuth2 access token (requires `scopes`)
  - `"id"` or `"jwt"`: Returns a Google OIDC ID token (optionally uses `audience`)
- `.urls[].config.lookup_chain[].scopes`: Array of OAuth2 scopes (used when `token_type` is `"access"`)
- `.urls[].config.lookup_chain[].audience`: OIDC audience (used when `token_type` is `"id"` or `"jwt"`)
- `.urls[].config.lookup_chain[].binding`: Optional binding to a specific secret. If unspecified, it binds to the `"default"` secret.

Examples:
```json
{
  "source": "google",
  "token_type": "access",
  "scopes": ["https://www.googleapis.com/auth/cloud-platform"],
  "binding": "default"
}
```

```json
{
  "source": "google",
  "token_type": "id",
  "audience": "https://my-service.example.com",
  "binding": "default"
}
```

The Google source uses Application Default Credentials (ADC) to obtain tokens. You can authenticate using:
- `gcloud auth application-default login` for user credentials
- Service account key files via `GOOGLE_APPLICATION_CREDENTIALS` environment variable
- Workload Identity in GKE/Cloud Run
- Other Google Cloud authentication mechanisms

## Secret bindings

In most cases, you only need a single secret to authenticate. In those cases, the `"default"` binding is used.
For some services, multiple secrets may be needed. In those cases, the documentation of the service specifies the name and purpose of a binding.
