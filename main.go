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
	"path"
	"path/filepath"

	"github.com/bobg/errors"
	"github.com/bobg/go-generics/v4/slices"
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

	var (
		all      bool
		emitCmd  bool
		emitJSON bool
		showErrs bool
		pre      bool
		qps      float64
	)

	flag.BoolVar(&all, "all", false, "show all files")
	flag.BoolVar(&emitCmd, "cmd", false, "emit output as shell commands")
	flag.BoolVar(&emitJSON, "json", false, "emit output as JSON")
	flag.BoolVar(&pre, "pre", true, "include prerelease versions (default true, use -pre=false to disable)")
	flag.BoolVar(&showErrs, "errs", true, "show errors (default true, use -errs=false to disable)")
	flag.Float64Var(&qps, "rate", 2, "max queries per second to the proxy")
	flag.StringVar(&goproxy, "proxy", goproxy, "Go module proxy URL")
	flag.Parse()

	if all && emitCmd {
		return fmt.Errorf("cannot specify both -all and -cmd")
	}
	if emitCmd && emitJSON {
		return fmt.Errorf("cannot specify both -cmd and -json")
	}

	var (
		limiter = rate.NewLimiter(rate.Limit(qps), 1)
		lt      = mid.LimitedTransport{L: limiter}
		hc      = &http.Client{Transport: lt}
		ctx     = context.Background()
		c       = controller{
			all:      all,
			emitCmd:  emitCmd,
			emitJSON: emitJSON,
			pre:      pre,
			showErrs: showErrs,
			client:   goproxyclient.New(goproxy, hc),
		}
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
	client                                goproxyclient.Client
	all, emitCmd, emitJSON, pre, showErrs bool
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
	Command     string `json:"command"`
	Error       string `json:"error,omitempty"`
}

func (c controller) doFile(ctx context.Context, file string) (err error) {
	o := output{
		File: file,
	}

	defer func() {
		if !c.showErrs && o.Error != "" {
			return
		}
		if !c.emitJSON && o.Error != "" {
			fmt.Fprintf(os.Stderr, "%s: %s\n", file, o.Error)
			return
		}
		if !c.all || c.emitCmd {
			if !semver.IsValid(o.Installed) || !semver.IsValid(o.Available) {
				return
			}
			if semver.Compare(o.Installed, o.Available) >= 0 {
				return
			}
		}
		if c.emitJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			err = enc.Encode(o)
			return
		}
		if c.emitCmd {
			fmt.Println(o.Command)
			return
		}
		fmt.Printf("%s:", file)
		if o.MainPackage != "" {
			fmt.Printf(" package=%s", o.MainPackage)
		}
		if o.Installed != "" {
			fmt.Printf(" installed=%s", o.Installed)
		}
		if o.Available != "" {
			fmt.Printf(" available=%s", o.Available)
		}
		fmt.Print("\n")
	}()

	info, err := buildinfo.ReadFile(file)
	if err != nil {
		err = errors.Wrapf(err, "reading %s", file)
		o.Error = err.Error()
		return nil
	}

	o.Installed = info.Main.Version
	o.MainModule = info.Main.Path
	o.MainPackage = info.Path

	// xxx check info.GoVersion, is it out of date?

	versions, err := c.client.List(ctx, info.Main.Path)
	if err != nil {
		err = errors.Wrapf(err, "listing versions for %s", info.Main.Path)
		o.Error = err.Error()
		return nil
	}

	if !c.pre {
		versions = slices.Filter(versions, func(v string) bool {
			return semver.Prerelease(v) == ""
		})
	}

	if len(versions) > 0 {
		semver.Sort(versions)
		o.Available = versions[len(versions)-1]
		o.Command = commandFor(o.MainPackage, o.Available, file)
	}

	return nil
}

func commandFor(pkg, ver, dest string) string {
	destdir, destfile := filepath.Split(dest)
	destdir = filepath.Clean(destdir)
	pkgbase := path.Base(pkg)
	gobin := os.Getenv("GOBIN")
	if gobin == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			home, _ := os.UserHomeDir()
			gopath = filepath.Join(home, "go")
		}
		gobin = filepath.Join(gopath, "bin")
	}

	if destdir == gobin && destfile == pkgbase {
		return fmt.Sprintf("go install %s@%s", pkg, ver)
	}

	if destfile == pkgbase {
		return fmt.Sprintf("GOBIN=%s go install %s@%s", destdir, pkg, ver)
	}

	tmpdir := os.TempDir()
	tmpfile := filepath.Join(tmpdir, pkgbase)
	return fmt.Sprintf("GOBIN=%s go install %s@%s && mv %s %s", tmpdir, pkg, ver, tmpfile, dest)
}
