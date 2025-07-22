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

When reading secrets from environment variables, the following options exist:

- `.urls[].config.lookup_chain[].source`: `"env"` Source of the secret (environment variable)
- `.urls[].config.lookup_chain[].name`: Name of the environment variable to read
- `.urls[].config.lookup_chain[].binding`: Optional binding to a specific secret. If unspecified, it binds to the `"default"` secret.

When reading secrets from the system keyring, the following options exist:

- `.urls[].config.lookup_chain[].source`: `"kering"` Source of the secret (system keyring)
- `.urls[].config.lookup_chain[].service`: Service name used to store the secret in the keyring.
- `.urls[].config.lookup_chain[].binding`: Optional binding to a specific secret. If unspecified, it binds to the `"default"` secret.

## Secret bindings

In most cases, you only need a single secret to authenticate. In those cases, the `"default"` binding is used.
For some services, multiple secrets may be needed. In those cases, the documentation of the service specifies the name and purpose of a binding.
