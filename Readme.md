# Goup

This is goup, a command that can inspect Go binaries to determine which ones can be upgraded.

It works by reading build-time info from each binary
(using [buildinfo.ReadFile](https://pkg.go.dev/debug/buildinfo#ReadFile))
and then querying a [Go module proxy](https://proxy.golang.org/)
to compare the build-time version info with the list of available versions.

## Installation

```sh
go install github.com/bobg/goup@latest
```

## Usage

```sh
goup [FLAGS] ARG ...
```

Each ARG is the path to a Go binary or a directory of Go binaries (such as `~/go/bin`).

The flags and their meanings are:

| flag       | meaning                                                                                                  |
|------------|----------------------------------------------------------------------------------------------------------|
| -all       | Show info for all binaries (not just those that can be upgraded).                                        |
| -cmd       | Show output as shell commands for performing upgrades.                                                   |
| -json      | Show output as JSON objects.                                                                             |
| -errs      | Show files that encountered errors (default true; disable with -errs=false).                             |
| -qps RATE  | Max queries per second to the Go module proxy.                                                           |
| -pre       | Include prerelease versions in the output (default true; disable with -pre=false).                       |
| -proxy URL | URL of the Go module proxy, or a sequence of proxies (default is $GOPROXY, or https://proxy.golang.org). |

Normal output is a line like this:

```
/Users/bobg/go/bin/decouple: package=github.com/bobg/decouple/cmd/decouple installed=v0.4.5 available=v0.5.0
```

Selecting `-cmd` changes that to:

```
go install -o /Users/bobg/go/bin/decouple github.com/bobg/decouple/cmd/decouple@v0.5.0
```

Selecting `-json` changes it to:

```
{
  "file": "/Users/bobg/go/bin/decouple",
  "installed": "v0.4.5",
  "available": "v0.5.0",
  "main_module": "github.com/bobg/decouple",
  "main_package": "github.com/bobg/decouple/cmd/decouple",
  "command": "go install -o /Users/bobg/go/bin/decouple github.com/bobg/decouple/cmd/decouple@v0.5.0"
}
```
