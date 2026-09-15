package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/EmreErdogan/mds/internal/server"
	"github.com/EmreErdogan/mds/internal/update"
)

// version is set at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func printUsage() {
	fmt.Fprint(os.Stderr, `mds - serve markdown files as HTML

Usage:
  mds [flags] <file.md | directory>
  mds update            Update mds to the latest release
  mds version           Print the version

Flags:
  -p, --port <n>        Port to listen on (default 8080, env MDS_PORT)
      --host <addr>     Address to bind (default 0.0.0.0, env MDS_HOST)
  -e, --ext <list>      Comma-separated file extensions to serve in directory
                        mode, e.g. "md,txt,png". Default: all files.
  -h, --help            Show this help

Examples:
  mds README.md
  mds ./docs
  mds -p 3000 -e md,png ./notes
  MDS_HOST=127.0.0.1 mds ./notes   # local only
`)
}

const (
	defaultHost = "0.0.0.0"
	defaultPort = 8080
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println("mds " + version)
			return
		case "update", "upgrade":
			if err := update.Run(version, os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "error:", err)
				os.Exit(1)
			}
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	fs := flag.NewFlagSet("mds", flag.ExitOnError)
	fs.Usage = printUsage
	// Precedence: flag > environment variable > default.
	host := defaultHost
	if v := os.Getenv("MDS_HOST"); v != "" {
		host = v
	}
	port := defaultPort
	if v := os.Getenv("MDS_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 65535 {
			fatal(fmt.Errorf("invalid MDS_PORT %q", v))
		}
		port = n
	}
	var ext string
	fs.IntVar(&port, "port", port, "port to listen on")
	fs.IntVar(&port, "p", port, "port to listen on")
	fs.StringVar(&host, "host", host, "address to bind")
	fs.StringVar(&ext, "ext", "", "comma-separated extensions to serve")
	fs.StringVar(&ext, "e", "", "comma-separated extensions to serve")

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

	opts := server.Options{Exts: splitExts(ext)}
	if info.IsDir() {
		opts.Root = target
	} else {
		opts.Root = filepath.Dir(target)
		opts.Index = target
	}
	handler, err := server.New(opts)
	if err != nil {
		fatal(err)
	}

	addr := net.JoinHostPort(host, fmt.Sprint(port))
	ln, err := net.Listen("tcp", addr)
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

func splitExts(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
