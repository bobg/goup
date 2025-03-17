// Command goup inspects one or more Go binaries and reports which ones can be updated.
package main

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bobg/errors"
	"github.com/bobg/goproxyclient"
	"golang.org/x/mod/semver"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	goproxy := os.Getenv("GOPROXY")
	if goproxy == "" {
		goproxy = "https://proxy.golang.org"
	}
	parts := strings.Split(goproxy, ",")
	if len(parts) > 1 {
		goproxy = parts[0]
	}

	var (
		client = goproxyclient.New(goproxy, nil)
		ctx    = context.Background()
	)
	for _, arg := range os.Args[1:] {
		info, err := os.Stat(arg)
		if err != nil {
			return errors.Wrapf(err, "statting %s", arg)
		}
		if info.IsDir() {
			if err := doDir(ctx, client, arg); err != nil {
				return errors.Wrapf(err, "processing %s", arg)
			}
			continue
		}
		if err := doFile(ctx, client, arg); err != nil {
			return errors.Wrapf(err, "processing %s", arg)
		}
	}
	return nil
}

func doDir(ctx context.Context, client *goproxyclient.Client, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Wrapf(err, "reading %s", dir)
	}
	for _, entry := range entries {
		if err := doFile(ctx, client, filepath.Join(dir, entry.Name())); err != nil {
			return errors.Wrapf(err, "processing %s/%s", dir, entry.Name())
		}
	}
	return nil
}

func doFile(ctx context.Context, client *goproxyclient.Client, file string) error {
	info, err := buildinfo.ReadFile(file)
	if err != nil {
		return errors.Wrapf(err, "reading %s", file)
	}

	// xxx check info.GoVersion, is it out of date?

	versions, err := client.List(ctx, info.Main.Path)
	if err != nil {
		return errors.Wrapf(err, "listing versions for %s", info.Main.Path)
	}

	if len(versions) == 0 {
		fmt.Printf("%s: no versions found for %s\n", file, info.Main.Path)
		return nil
	}

	semver.Sort(versions)
	if semver.Compare(info.Main.Version, versions[len(versions)-1]) < 0 {
		fmt.Printf("%s: at version %s, can be updated with go install %s@%s\n", file, info.Main.Version, info.Main.Path, versions[len(versions)-1])
	} else {
		fmt.Printf("%s: at version %s@%s, no update needed\n", file, info.Main.Path, info.Main.Version)
	}

	return nil
}
