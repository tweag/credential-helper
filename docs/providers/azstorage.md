# Azure Blob Storage Authentication

This document explains how to setup your system for authenticating to Azure Blob Storage using the
credential helper to download objects.

## IAM Setup

To access blob data, a user must have the following role assignments:
- A data access role, such as Storage Blob Data Reader or Storage Blob Data Contributor
- The Azure Resource Manager Reader role, at a minimum.

The following example assigns the Storage Blob Data Reader role to a user by specifying the object ID.
In this example, the role assignment is scoped to the level of the storage account.

```
az role assignment create \
   --role "Storage Blob Data Reader" \
	--assignee-object-id "aaaaaaaa-0000-1111-2222-bbbbbbbbbbbb" \
	--assignee-principal-type "User" \
	--scope "/subscriptions/<subscription-id>/resourceGroups/<resource-group-name>/providers/Microsoft.Storage/storageAccounts/<storage-account-name>"
```

Refer to Azure's [documentation][azure-rbac-docs] for more information about RBAC.

## Authentication Methods

There are several methods that you can use to authenticate with Azure.

You can refer to Azure's [documentation][azure-auth-docs] for the details.

## Configuration

Add to your `.bazelrc`:

```
common --credential_helper=*.blob.core.windows.net=%workspace%/tools/credential-helper
```

It's possible to configure the [x-ms-version][storage-versioning] header through the
`AZURE_HEADER_X_MS_VERSION` environment variable. If not provided it will default to `2025-07-05`.

[azure-rbac-docs]: https://learn.microsoft.com/en-us/azure/role-based-access-control/
[azure-auth-docs]: https://learn.microsoft.com/en-us/cli/azure/authenticate-azure-cli?view=azure-cli-latest
[storage-versioning]: https://learn.microsoft.com/en-us/rest/api/storageservices/versioning-for-the-azure-storage-services
