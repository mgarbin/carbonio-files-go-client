[← Back to README](../README.md)

## Troubleshooting: macOS build fails with a system framework header not found

### Symptom

Building on macOS (observed on **macOS Tahoe / 26**) fails while compiling a
cgo dependency, e.g.:

```
# github.com/wailsapp/wails/v2/pkg/assetserver/webview
.../wails/v2@v2.13.0/pkg/assetserver/webview/responsewriter_darwin.go:9:9: fatal error: 'Foundation/Foundation.h' file not found
#import <Foundation/Foundation.h>
        ^~~~~~~~~~~~~~~~~~~~~~~~~
1 error generated
```

or:

```
# github.com/energye/systray
systray_darwin.m:1:9: fatal error: 'Cocoa/Cocoa.h' file not found
make: *** [build] Error 1
```

The exact framework in the message (`Foundation`, `Cocoa`, `WebKit`, ...) and
which dependency fails first depend only on build order — the underlying
failure is the same.

### Cause

This is **not** a bug in this repository. Several dependencies contain
cgo/Objective-C sources that import Apple system framework headers —
`responsewriter_darwin.go` in Wails (`#import <Foundation/Foundation.h>`,
`<WebKit/WebKit.h>`) and `systray_darwin.m` in `energye/systray`
(`#import <Cocoa/Cocoa.h>`). During `go build`, cgo invokes `clang`, which
needs the macOS SDK sysroot to resolve any of these framework headers
(they live under `.../MacOSX*.sdk/System/Library/Frameworks/`). The build
fails when clang gets no `-isysroot`, or one pointing at an SDK that does not
exist.

After a Tahoe upgrade the Command Line Tools are often stale: cgo looks for
`/Library/Developer/CommandLineTools/SDKs/MacOSX26.sdk`, which the old CLT do
not ship. See [golang/go#75568](https://github.com/golang/go/issues/75568).

> The `webkit2_41` build tag in the `Makefile` is a Linux/GTK tag and is
> irrelevant on macOS — it does not cause this error.

### Diagnose

```bash
xcode-select -p                                  # active developer dir
xcrun --show-sdk-path                            # must print an existing MacOSX*.sdk
ls /Library/Developer/CommandLineTools/SDKs/     # is MacOSX26.sdk present?
go env CGO_ENABLED CC                            # CGO_ENABLED must be 1
```

If `xcrun --show-sdk-path` is empty or points at a missing directory, that
confirms the broken SDK.

### Fix

Try these in order.

1. **Update Xcode / Command Line Tools to the Tahoe build** (the real fix). The
   Xcode update that ships `MacOSX26.sdk` resolves it. You may need macOS
   **26.2+** before the CLT will install.

   ```bash
   sudo rm -rf /Library/Developer/CommandLineTools
   xcode-select --install
   ```

   If full Xcode is installed:

   ```bash
   sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer
   ```

2. **Symlink workaround** (from golang/go#75568) if you cannot update yet —
   point the missing `MacOSX26.sdk` at the installed SDK:

   ```bash
   sudo ln -s /Library/Developer/CommandLineTools/SDKs/MacOSX15.sdk \
              /Library/Developer/CommandLineTools/SDKs/MacOSX26.sdk
   ```

3. **Force the SDK explicitly** for the build:

   ```bash
   export SDKROOT="$(xcrun --show-sdk-path)"
   export CGO_CFLAGS="-isysroot $SDKROOT"
   export CGO_LDFLAGS="-isysroot $SDKROOT"
   make build
   ```

Also check `~/.zshrc` / `~/.bash_profile` for a stale `CPLUS_INCLUDE_PATH` or
`SDKROOT` — a known cause of this exact "framework header not found" symptom —
and clear it.

## Troubleshooting: macOS build prints a `setShowsBaselineSeparator:` deprecation warning

### Symptom

Building on macOS with **SDK 26+** prints:

```
# github.com/wailsapp/wails/v2/internal/frontend/desktop/darwin
WailsContext.m:182:18: warning: 'setShowsBaselineSeparator:' is deprecated: first deprecated in macOS 15.0 - No longer supported [-Wdeprecated-declarations]
```

### Cause

This is a **warning, not an error** — `go build` still completes and emits the
`carbonio-files-client` binary. The call lives in Wails' own vendored
Objective-C (`WailsContext.m`, `wails/v2@v2.13.0`), not in this repository.
`NSToolbar setShowsBaselineSeparator:` was soft-deprecated in macOS 15 but
remains functional; the macOS 26 SDK simply started flagging it via
`-Wdeprecated-declarations`. cgo does not use `-Werror`, so the warning never
aborts the build.

### Fix

The `Makefile` already appends `-Wno-deprecated-declarations` to `CGO_CFLAGS`
for the `build` target, so `make build` is quiet. If you invoke `go build`
directly, silence it the same way:

```bash
CGO_CFLAGS="$CGO_CFLAGS -Wno-deprecated-declarations" \
  go build -tags "webkit2_41,production" -ldflags="-s -w" \
  -o carbonio-files-client ./cmd/carbonio-files-go-client
```

The fix belongs upstream in Wails; nothing in this repository can be changed to
remove the deprecated call itself.
