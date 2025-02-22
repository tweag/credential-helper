# Building a Custom Credential Helper Binary in Your Bazel Repository

The default credential helper (`@tweag-credential-helper//:tweag-credential-helper`) supports popular services out of the box. If you need additional providers, consider adding support upstream. However, certain projects require custom helpers—either for unique requirements or because the code cannot be released as open source. In such cases, you can define your own `credential_helper` and `installer` targets in your Bazel workspace. A full example is available under [examples/customized][example].

## Implementing a custom helper for authentication to a service

Implement a `go_library` with a struct that implements the `api.Helper` interface. The interface requires two methods:

```go
Resolver(context.Context) (Resolver, error)
CacheKey(GetCredentialsRequest) string
```

`CacheKey` determines the cache key under which a request is stored. Because some services can share authentication headers across multiple URIs, let your helper return an accurate and efficient cache key.

`Resolver` returns a new `api.Resolver`, which you must also implement for your custom provider:

```go
Get(context.Context, GetCredentialsRequest) (GetCredentialsResponse, error)
```

The `Get` method receives a request containing a URI and returns authentication headers (and optionally an expiration time).

You can find the built-in default implementations under [/authenticate][authenticate]. You can also look at an [example of a custom helper that uses parts of the URL path as an authentication header][example-authenticate].

## Implementing a helper factory

The credential helper supports multiple authentication providers in a single helper binary. The factory function determines which `api.Helper` to use based on the request URI. You can find the default implementation in [github.com/tweag/credential-helper/helperfactory/fallback.FallbackHelperFactory][fallback-helper-factory].

## Registering your helper

Custom helpers need to be registered at program startup to work and be recognized correctly. For this, the `registry.Register` function can be called to add a globally known register (with a unique name) to the registry.
Simply add an `init` function to the package that implements `api.Helper`:

```go
func init() {
	registry.Register("foo", FooHelper{})
}
```

## (Optional) Replace the default in-memory cache

By default, the agent process uses a simple in-memory key-value store to cache credentials. You can provide a custom implementation to persist credentials (on disk, in a database, in a shared key-value store, using a (hardware backed) secure storage, etc.), implement more selective caching (decide what to keep), or perform other custom logic.

Implement a `go_library` with a struct that implements the `api.Cache` interface:

```go
Retrieve(context.Context, string) (GetCredentialsResponse, error)
Store(context.Context, CachableGetCredentialsResponse) error
Prune(context.Context) error
```

`Retrieve` takes a cache key and returns a cached response (or the special error `api.CacheMiss`). `Store` receives a cachable response (including a cache key) and caches it. `Prune` is called by the agent on a schedule to evict expired credentials.

You can find the default implementation in [github.com/tweag/credential-helper/cache.MemCache][memcache] and review an [example of a custom cache implementation that uses SQLite to persist credentials][example-sqlite].

## Putting it all together

Add a `BUILD.bazel` file containing the following template and replace values as needed:

```starlark
load("@tweag-credential-helper//bzl:defs.bzl", "credential_helper", "installer")

# This is an example for creating your own, custom credential helper
credential_helper(
    name = "custom_credential_helper",
    # Set `cache` to a `go_library` that implements `api.Cache` and a
    # function of type api.NewCache to construct it.
    cache = "//helper/cache",
    cache_type_name = "NewCustomCache",
    # Set `helperfactory` to a `go_library` that implements `api.HelperFactory`.
    helperfactory = "//helper/helperfactory",
    helperfactory_type_name = "CustomHelperFactory",
    pure = "on",
    visibility = ["//visibility:public"],
)

# You can invoke the installer using:
#   bazel run //:custom_installer
installer(
    name = "custom_installer",
    credential_helper = ":custom_credential_helper",
)
```

Assuming the Bazel package is in the root of your workspace, you can install your custom helper with this command:

```
bazel run //:custom_installer
```

[example]: /examples/customized
[example-authenticate]: /examples/customized/helper/authenticate
[example-sqlite]: /examples/customized/helper/cache/sqlitecache.go
[authenticate]: /authenticate
[fallback-helper-factory]: /helperfactory/fallback/fallback_factory.go
[memcache]: /cache/memcache.go
