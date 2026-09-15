package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/EmreErdogan/mds/internal/config"
	"github.com/EmreErdogan/mds/internal/server"
	"github.com/EmreErdogan/mds/internal/update"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func printUsage() {
	fmt.Fprint(os.Stderr, `mds - serve markdown files as HTML

Usage:
  mds [flags] <file.md | directory>
  mds config [directory]  Show effective settings and where they come from
  mds update              Update mds to the latest release
  mds version             Print the version

Flags:
  -p, --port <n>        Port to listen on (default 8080)
      --host <addr>     Address to bind (default 0.0.0.0)
  -e, --ext <list>      Comma-separated file extensions to serve in directory
                        mode, e.g. "md,txt,png". Default: all files.
      --index           Render README.md / index.md under directory listings
      --no-index        Disable --index
      --no-reload       Disable live reload
  -h, --help            Show this help

Settings are resolved in this order, later wins:
  defaults < ~/.config/mds/config.toml < <dir>/.mds.toml < environment < flags
Environment: MDS_HOST, MDS_PORT, MDS_EXT, MDS_RELOAD, MDS_INDEX

Examples:
  mds README.md
  mds ./docs
  mds -p 3000 -e md,png ./notes
  MDS_HOST=127.0.0.1 mds ./notes   # local only
`)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println("mds " + version)
			return
		case "update", "upgrade":
			if err := update.Run(version, os.Args[2:]); err != nil {
				fatal(err)
			}
			return
		case "config":
			if err := showConfig(os.Args[2:]); err != nil {
				fatal(err)
			}
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	fs := flag.NewFlagSet("mds", flag.ExitOnError)
	fs.Usage = printUsage
	var (
		port                     int
		host, ext                string
		index, noIndex, noReload bool
	)
	fs.IntVar(&port, "port", 0, "")
	fs.IntVar(&port, "p", 0, "")
	fs.StringVar(&host, "host", "", "")
	fs.StringVar(&ext, "ext", "", "")
	fs.StringVar(&ext, "e", "", "")
	fs.BoolVar(&index, "index", false, "")
	fs.BoolVar(&noIndex, "no-index", false, "")
	fs.BoolVar(&noReload, "no-reload", false, "")

	// Allow flags before and after the positional argument.
	args := os.Args[1:]
	var positional []string
	for {
		_ = fs.Parse(args)
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(positional) != 1 {
		printUsage()
		os.Exit(2)
	}

	target, err := filepath.Abs(positional[0])
	if err != nil {
		fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		fatal(err)
	}
	root, indexFile := target, ""
	if !info.IsDir() {
		root, indexFile = filepath.Dir(target), target
	}

	cfg, _, err := config.Load(root)
	if err != nil {
		fatal(err)
	}
	// Flags win over everything else, but only when given explicitly.
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "port", "p":
			cfg.Port = port
		case "host":
			cfg.Host = host
		case "ext", "e":
			cfg.Ext = config.SplitExts(ext)
		case "index":
			cfg.Index = true
		case "no-index":
			cfg.Index = false
		case "no-reload":
			cfg.Reload = false
		}
	})

	handler, err := server.New(server.Options{
		Root:     root,
		Index:    indexFile,
		Exts:     cfg.Ext,
		Reload:   cfg.Reload,
		DirIndex: cfg.Index,
	})
	if err != nil {
		fatal(err)
	}
	defer handler.Close()

	ln, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		fatal(err)
	}
	fmt.Printf("mds %s serving %s\n", version, target)
	fmt.Println("listening on:")
	for _, u := range listenURLs(ln.Addr().(*net.TCPAddr)) {
		fmt.Println("  " + u)
	}
	if err := http.Serve(ln, handler); err != nil {
		fatal(err)
	}
}

// showConfig implements "mds config [directory]".
func showConfig(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	cfg, src, err := config.Load(dir)
	if err != nil {
		return err
	}
	fmt.Printf("global config: %s%s\n", config.GlobalPath(), exists(config.GlobalPath()))
	fmt.Printf("local config:  %s%s\n\n", config.LocalPath(dir), exists(config.LocalPath(dir)))
	for _, k := range config.Keys {
		fmt.Printf("%-8s %-12s %s\n", k, config.Format(cfg, k), src[k])
	}
	return nil
}

func exists(path string) string {
	if _, err := os.Stat(path); err != nil {
		return "  (not found)"
	}
	return ""
}

// listenURLs returns the URLs the server is reachable at. For an unspecified
// bind address every IPv4 interface address is listed, annotated with the
// interface name.
func listenURLs(addr *net.TCPAddr) []string {
	port := strconv.Itoa(addr.Port)
	if !addr.IP.IsUnspecified() {
		return []string{"http://" + net.JoinHostPort(addr.IP.String(), port) + "/"}
	}
	var urls []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"http://localhost:" + port + "/"}
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			u := "http://" + net.JoinHostPort(ipn.IP.String(), port) + "/"
			if !ipn.IP.IsLoopback() {
				u += "  (" + iface.Name + ")"
			}
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		urls = []string{"http://localhost:" + port + "/"}
	}
	return urls
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
