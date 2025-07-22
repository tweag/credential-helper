# Google Artifact Registry (GAR) Authentication

This document explains how to setup your system for authenticating to Google Artifact Registry
using the credential helper to download various packages from the registry (e.g PyPi, Maven)

## IAM Setup

In order to access data from a bucket, you need a Google Cloud user- or service account with read
access to the repositories you want to access. No other permissions are needed to download artifacts.
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

Add the following to your `.bazelrc`:

```
common --credential_helper=*.pkg.dev=%workspace%/tools/credential-helper
```

## Limitations

### Usage with rules_python

Currently it's not possible to populate the `requirements.txt` file using
[compile_pip_requirements][compile-pip-requirements]
from [rules_python][rules_python] because it doesn't use Bazel's downloader but pip directly.

As a workaround you'll need to manually update the requirements.txt by using the native
authentication pip mechanism

```bash
uv pip compile --output-file=python/requirements.txt --emit-index-url --generate-hashes --no-strip-extras python/requirements.in
```

Also note that in order to use the artifact registry, you'll need to provide the repository URL
through the `experimental_index_url` attribute which will bypass pip and instead use the credential
helper.

The example below is taken from `examples/full/MODULE.bazel`.
```
pip.parse(
    experimental_index_url = "https://oauth2accesstoken@europe-west8-python.pkg.dev/git-credential-helper-dev/python-test-repository/simple",
    hub_name = "pypi",
    python_version = "3.13",
    quiet = False,
    requirements_lock = "//:python/requirements.txt",
)
```

[adc]: https://cloud.google.com/docs/authentication/provide-credentials-adc
[gcloud-install]: https://cloud.google.com/sdk/docs/install
[google-cloud-auth]: https://cloud.google.com/docs/authentication
[rules_python]: https://github.com/bazel-contrib/rules_python
[compile-pip-requirements]: https://rules-python.readthedocs.io/en/0.32.1/api/pip.html#compile-pip-requirements
