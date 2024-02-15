# Go Binary Updater

A simple way to self update Go applications from hosted prebuilt binaries.

## Features

- Supports downloading and extracting binaries from archives (.tar.gz or .zip)
- Supports downloading binaries
- Supports downloading updates from files hosted on GitHub releases.

## How it works

Updater expects the following files to be hosted together under the same base url.

- A manifest json file.
- The prebuilt binaries or archives (.tar.gz or .zip) containing the prebuilt binaries.

The manifest file describes the following

- App version
- os and architecture specific file names for archives/binaries.
- Binary name

Updater downloads the manifest to check the available binaries/archives and installs the one appropriate for the current os and architecture. It then replaces the currently running binary with the newly downloaded binary.

## Example

Here is a simple example where prebuilt binaries are hosted on GitHub releases within archives.

**Files hosted on GitHub releases:**

- scf_Darwin_x86_64.tar.gz
- scf_Linux_x86_64.tar.gz
- scf_Windows_x86_64.zip
- updater.config.json

**updater.config.json**

```json
{
  "version": "1.0.0",
  "archive": "scf_{{.Os}}_{{.Arch}}{{.ArchiveExt}}",
  "binary": "scf{{.Ext}}",
  "os": {
    "linux": "Linux",
    "darwin": "Darwin",
    "windows": "Windows"
  },
  "arch": {
    "Windows": {
      "amd64": "x86_64"
    },
    "Linux": {
      "amd64": "x86_64"
    },
    "Darwin": {
      "amd64": "x86_64"
    }
  }
}
```

View the full [manifest reference](#updater-manifest-type) for more details.

**Example updater code**


```go
package main

import (
  "fmt"

  "github.com/dworthen/updater"
)

func main() {
  pkgUpdater := updater.New(&updater.UpdaterConfig{
    CurrentVersion: "0.0.1",
    BaseUrl: "https://github.com/dworthen/scf/releases/latest/download",
    UpdaterConfig: "updater.config.json"
  })

  // Check for updates.
  isUpdate, newVersion, err := pkgUpdater.CheckForAvailableUpdate()
  if err != nil {
    fmt.Println(err)
    os.Exit(1)
  }

  if isUpdate {
    fmt.Printf("New update available. Updating to %v", newVersion)

    // Perform update
    err = pkgUpdater.Update()
    if err != nil {
      var notSupportedError *updater.NotSupportedError
      if errors.As(err, &notSupportedError) {
        fmt.Printf(
          "Self updating is not supported for %s. Please reinstall.",
          notSupportedError.Platform
        )
      } else {
        fmt.Println(err)
      }
        os.Exit(1)
    }

    fmt.Println("Updated!")
  }
}
```

You can view the hosted files for this sample [here](https://github.com/dworthen/scf/releases/latest) along with the usage of updater [here](https://github.com/dworthen/scf/blob/main/internal/versioninfo/version.go).

## Reference

### Updater Config

- `CurrentVersion`: The current version of the application. This is used in `CheckForAvailableUpdate`. The method checks that the `CurrentVersion` and hosted manifest `version` differ to determine that there is an update available. Updater only checks that these values differ and does not try to parse them as semantic versions or determine if the hosted version is greater than the current version. The idea is that the location provided by `BaseUrl` is where the latest, ready-to-go, binaries are stored.
- `UpdaterConfig`: Name of the updater manifest file hosted at the `BaseUrl`.
- `BaesUrl`: Url where all the files are hosted. Updater will first download the `UpdaterConfig` file from this location and then use the values within the manifest to download the appropriate archive/binary from the same `BaseUrl` location. Updater expects the manifest to be hosted along side the binaries/archives.

### Updater Manifest Type

- `version` (string) [Required]: The version of
- `archive` ([text/template string](https://pkg.go.dev/text/template@go1.22.0)) [Optional]: Describes the archive names where the binaries are stored. If not provided, updater will download the direct binaries as specified by the `binary` key.
- `binary` ([text/template string](https://pkg.go.dev/text/template@go1.22.0)) [Required]: The name of the binary. If the `archive` key is provided, updater will extract the binary from the archive. This should be the name of the binary file only, not the path. For example, if the archive contains a directory that then contains the binary, only provide the binary name, updater will search through all directories for the binary. If multiple directories exist within the archive that contain the binary, updater will use the first found binary that matches the name. If the `archive` key is not provided then updater will try to download the binary directly from the `BaseUrl`.
- `os` (map[string]string) [Required]: A mapping of os names as returned by `runtime.GOOS` to the value that is used by the hosted archive/binary file names. This value is required even if using the default values, e.g., `windows` = `windows`.
- `arch` (map[string]map[string]string) [Required]: A mapping of architectures as returned by `runtime.GOARCH` to what is used by the hosted archive/binary file names. This map is scoped by os so it is possible to map the term `amd64` to `x86_64` for linux builds but leave as is for windows. This value is required even if using the default values, e.g., `linux.amd64` = `amd64`.

> [!NOTE]
> `os` and `arch` mappings are required even if using the default values. Updater uses these maps to determine which platforms are supported for updating, that way avoiding random requests to the `BaseUrl`. For example, if a user is running the application on `freebsd` but prebuilt binaries are only available for `darwin`, `linux` and `windows` then when updater is unable to find the `freebsd` key in the `os` map it will assume that self-updating is not supported on that platform and report that in ERROR and avoid making a request to the `BaseUrl` for the `freebsd` based binary.
>
> Run `go tool dist list` to view the full list of possible `os`/`arch` combinations.

The `archive` and `binary` template strings have access to the following variables:

- `OS`: The operating system as defined the `os` mapping.
- `Arch`: The architecture as defined by the `arch` mapping. In the above example, `Arch` is set to `x86_64` instead of `amd64` on all systems due to the `arch` mapping.
- `ArchiveExt`: `.zip` on Windows and `.tar.gz` on other platforms.
- `Ext`: The binary extension. `.exe` on Windows and the empty string on other platforms.
