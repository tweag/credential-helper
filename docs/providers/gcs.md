# Google Cloud Storage (GCS) Authentication

This document explains how to setup your system for authenticating to Google Cloud Storage (GCS) using the credential helper to download objects, or use a bucket as a HTTP/1.1 remote cache.

## IAM Setup

In order to access data from a bucket, you need a Google Cloud user- or service account with read access to the objects you want to access (`storage.objects.get`). No other permissions are needed to download objects.
Refer to [Google's documentation][gcs-iam] for more information.


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
common --credential_helper=storage.googleapis.com=%workspace%/tools/credential-helper
```

Additionally, you can configure a GCS bucket to be a HTTP/1.1 remote cache:

```
build --remote_cache=https://storage.googleapis.com/my_bucket
```

## Troubleshooting

### HTTP 401 or 403 error codes

```
ERROR: Target parsing failed due to unexpected exception: java.io.IOException: Error downloading [https://storage.googleapis.com/...] to ...: GET returned 403 Forbidden
```

First, verify your credentials are valid: `gcloud auth application-default print-access-token`.
Then ensure the user you are logged in as has access to the bucket using `gsutil cp gs://<BUCKET_NAME>/<OBJECT> ./<OUTPUT_FILENAME>` and check if the credential helper is configured in `.bazelrc` like this: `--credential_helper=storage.googleapis.com=%workspace%/tools/credential-helper`.

[adc]: https://cloud.google.com/docs/authentication/provide-credentials-adc
[api-explorer-objects-get]: https://cloud.google.com/storage/docs/json_api/v1/objects/get
[gcloud-install]: https://cloud.google.com/sdk/docs/install
[gcs-iam]: https://cloud.google.com/storage/docs/access-control/iam-permissions
[google-cloud-auth]: https://cloud.google.com/docs/authentication
