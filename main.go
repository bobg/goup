// Command goup inspects one or more Go binaries and reports which ones can be updated.
package main

import (
	"context"
	"debug/buildinfo"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bobg/errors"
	"github.com/bobg/goproxyclient"
	"github.com/bobg/mid"
	"golang.org/x/mod/semver"
	"golang.org/x/time/rate"
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
		upgradeable bool
		qps         float64
	)

	flag.StringVar(&goproxy, "proxy", goproxy, "Go module proxy URL")
	flag.BoolVar(&upgradeable, "u", false, "show only upgradeable files")
	flag.Float64Var(&qps, "rate", 2, "max queries per second to the proxy")
	flag.Parse()

	var (
		limiter = rate.NewLimiter(rate.Limit(qps), 1)
		lt      = mid.LimitedTransport{L: limiter}
		hc      = &http.Client{Transport: lt}
		c       = controller{client: goproxyclient.New(goproxy, hc), upgradeable: upgradeable}
		ctx     = context.Background()
	)
	for _, arg := range flag.Args() {
		info, err := os.Stat(arg)
		if err != nil {
			return errors.Wrapf(err, "statting %s", arg)
		}
		if info.IsDir() {
			if err := c.doDir(ctx, arg); err != nil {
				return errors.Wrapf(err, "processing %s", arg)
			}
			continue
		}
		if err := c.doFile(ctx, arg); err != nil {
			return errors.Wrapf(err, "processing %s", arg)
		}
	}
	return nil
}

type controller struct {
	client      *goproxyclient.Client
	upgradeable bool
}

func (c controller) doDir(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return errors.Wrapf(err, "reading %s", dir)
	}
	for _, entry := range entries {
		if err := c.doFile(ctx, filepath.Join(dir, entry.Name())); err != nil {
			return errors.Wrapf(err, "processing %s/%s", dir, entry.Name())
		}
	}
	return nil
}

type output struct {
	File        string `json:"file"`
	Installed   string `json:"installed"`
	Available   string `json:"available"`
	MainModule  string `json:"main_module"`
	MainPackage string `json:"main_package"`
}

func (c controller) doFile(ctx context.Context, file string) error {
	info, err := buildinfo.ReadFile(file)
	if err != nil {
		return errors.Wrapf(err, "reading %s", file)
	}

	if c.upgradeable && !semver.IsValid(info.Main.Version) {
		return nil
	}

	// xxx check info.GoVersion, is it out of date?

	versions, err := c.client.List(ctx, info.Main.Path)
	if err != nil {
		return errors.Wrapf(err, "listing versions for %s", info.Main.Path)
	}

	o := output{
		File:        file,
		Installed:   info.Main.Version,
		MainModule:  info.Main.Path,
		MainPackage: info.Path,
	}

	if len(versions) > 0 {
		semver.Sort(versions)
		o.Available = versions[len(versions)-1]
	}

	if c.upgradeable && semver.Compare(o.Installed, o.Available) >= 0 {
		return nil
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	err = enc.Encode(o)
	return errors.Wrap(err, "encoding output")
}
