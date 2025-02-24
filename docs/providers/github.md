# GitHub Authentication

This document explains how to setup your system for authenticating to GitHub using the credential helper.
The credential helper can be used to download any assets GitHub hosts, including:

- [the git protocol via https][git-http]
- raw code files (`raw.githubusercontent.com/<org>/<repo>/<commit>/<file>`)
- patches (`github.com/<org>/<repo>/<commit>.patch`)
- source tarballs (`github.com/<org>/<repo>/archive/refs/tags/v1.2.3.tar.gz`)
- release assets (`github.com/<org>/<repo>/releases/download/v1.2.3/<file>`)
- container images from `ghcr.io` ([doc][doc-oci])
- ... and more.

With credentials, you are also less likely to be blocked by GitHub rate limits, even when accessing public repositories.

## Authentication Methods

### Option 1: Using the GitHub CLI as a regular user (Recommended)

With this setup, credentials are stored in a local file (`hosts.yml`) or in the user's keyring.

1. Install the [GitHub CLI (`gh`)][gh-install]
2. Login via `gh auth login`
3. Follow the browser prompts to authenticate

### Option 2: Authentication using a GitHub App, GitHub Actions Token or Personal Access Token (PAT)

1. Setup your authentication method of choice
2. Set the required environment variable (`GH_TOKEN` or `GITHUB_TOKEN`) when running Bazel (or other tools that access credential helpers)
3. Alternatively, add the secret to the system keyring under the `gh:github.com` key.

## Configuration

Add to your `.bazelrc`:

```
common --credential_helper=github.com=%workspace%/tools/credential-helper
common --credential_helper=raw.githubusercontent.com=%workspace%/tools/credential-helper
```

The configuration in `.tweag-credential-helper.json` supports the following values:

- `.urls[].helper`: `"github"` (name of the helper)
- `.urls[].config.read_config_file`: Boolean (default: `true`). If set, allows the helper to search the GitHub config file (`~/.config/gh/hosts.yml`) for tokens
- `.urls[].config.lookup_chain`: The [lookup chain][lookup_chain] used to find the `default` secret. Defaults to:
    ```json
    [
        {
            "source": "env",
            "name": "GH_TOKEN",
            "binding": "default"
        },
        {
            "source": "env",
            "name": "GITHUB_TOKEN",
            "binding": "default"
        },
        {
            "source": "keyring",
            "name": "gh:github.com",
            "binding": "default"
        }
    ]
    ```

## Troubleshooting

### HTTP 401 or 403 error codes

When using a regular user account with the GitHub CLI, validate that the token did not expire: `gh auth status`.
Otherwise, ensure that your token is still valid and has the required permissions for the resource you are trying to access.
Personal access tokens and automatic GitHub Actions tokens are limited in what resources they can access.
If possible, switch to a GitHub CLI token (regular user) or a GitHub App (CI or automated system) instead, since they have access to more resources.

[gh-install]: https://github.com/cli/cli#installation
[git-http]: https://git-scm.com/book/ms/v2/Git-on-the-Server-The-Protocols#_the_http_protocols
[doc-oci]: /docs/providers/oci.md
