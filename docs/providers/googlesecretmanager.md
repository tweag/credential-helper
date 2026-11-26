# Google Secret Manager Authentication

This document explains how to setup your system for authenticating to [Google Secret Manager][secretmanager] using the credential helper. This allows Bazel's downloader (and repository rules) to fetch secret payloads directly from the Secret Manager REST API.

## IAM Setup

In order to access a secret, you need a Google Cloud user- or service account with read access to the secret versions you want to access (`secretmanager.versions.access`, contained in the role `roles/secretmanager.secretAccessor`). No other permissions are needed.
You can grant access to a single secret with:

```bash
gcloud secrets add-iam-policy-binding <SECRET_NAME> \
    --project=<PROJECT> \
    --member=user:<EMAIL> \
    --role=roles/secretmanager.secretAccessor
```

Refer to [Google's documentation][secretmanager-iam] for more information.

## Authentication Methods

### Option 1: Using gcloud CLI as a regular user (Recommended)

1. Install the [Google Cloud SDK][gcloud-install]
2. Run:
   ```bash
   gcloud auth application-default login
   ```
3. Follow the browser prompts to authenticate

### Option 2: Using a Service Account Key, OpenID Connect or other authentication mechanisms

1. Follow [Google's documentation][google-cloud-auth] for choosing and setting up your method of choice
2. Ensure your method of choice sets the [Application Default Credentials (ADC)][adc] environment variable (`GOOGLE_APPLICATION_CREDENTIALS`)
3. Alternatively, check that the credentials file is in a well-known location (`$HOME/.config/gcloud/application_default_credentials.json`)

## Configuration

Add to your `.bazelrc`:

```
common --credential_helper=secretmanager.googleapis.com=%workspace%/tools/credential-helper
```

You can then access secret versions from repository rules using Bazel's downloader, for example:

```starlark
http_file(
    name = "my_secret",
    url = "https://secretmanager.googleapis.com/v1/projects/<PROJECT>/secrets/<SECRET_NAME>/versions/<VERSION>:access",
)
```

## Limitations

The `:access` endpoint of the REST API does not return the raw secret payload. It returns a JSON envelope with the payload base64-encoded in `.payload.data`, which consumers need to decode themselves:

```json
{
  "name": "projects/<PROJECT_NUMBER>/secrets/<SECRET_NAME>/versions/<VERSION>",
  "payload": {
    "data": "<BASE64_ENCODED_SECRET>"
  }
}
```

Additionally, the envelope contains the numeric project ID and secret name, so the response for `versions/latest` changes whenever a new secret version is added. Pin numeric secret versions (and avoid checksumming the envelope in a lockfile) unless you are sure the secret never rotates.

## Troubleshooting

### HTTP 401 or 403 error codes

```
ERROR: Target parsing failed due to unexpected exception: java.io.IOException: Error downloading [https://secretmanager.googleapis.com/...] to ...: GET returned 401 Unauthorized
```

First, verify your credentials are valid: `gcloud auth application-default print-access-token`.
Then ensure the user you are logged in as has access to the secret version using `gcloud secrets versions access <VERSION> --secret=<SECRET_NAME> --project=<PROJECT>` and check that the credential helper is configured in `.bazelrc` like this: `--credential_helper=secretmanager.googleapis.com=%workspace%/tools/credential-helper`.

[adc]: https://cloud.google.com/docs/authentication/provide-credentials-adc
[gcloud-install]: https://cloud.google.com/sdk/docs/install
[google-cloud-auth]: https://cloud.google.com/docs/authentication
[secretmanager]: https://cloud.google.com/secret-manager
[secretmanager-iam]: https://cloud.google.com/secret-manager/docs/access-control
