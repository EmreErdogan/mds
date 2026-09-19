package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

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
  mds [flags] -             Render markdown from standard input
  mds serve [flags] <path>  Same as above; use when the path is named like a
                            command (e.g. a directory called "config")
  mds config [directory]    Show effective settings and where they come from
                            (defaults to the current directory)
  mds config init           Write a commented global config file
                            (--local writes ./.mds.toml, --force overwrites)
  mds update                Update mds to the latest release
  mds version               Print the version

Server:
  -p, --port <n>        Port to listen on (default 8080)
      --host <addr>     Address to bind (default 0.0.0.0)
      --open            Open the page in a browser after starting
      --no-reload       Disable live reload
      --no-toc          Hide the table of contents on rendered pages
      --theme <name>    auto (follow the system), light or dark

Files (directory mode):
  -t, --types <list>    Only serve these file types, e.g. "md,txt,png".
                        Default: all files.
  -x, --exclude <list>  Glob patterns for names to skip, e.g. "node_modules,*.log".
                        Default: ".git". Excluded names are never served.
      --hidden          Also serve dot-prefixed files and directories
      --index           Render README.md / index.md under directory listings
      --no-index        Disable --index

  -h, --help            Show this help

Settings are resolved in this order, later wins:
  defaults < ~/.config/mds/config.toml < <dir>/.mds.toml < environment < flags
Environment: MDS_HOST, MDS_PORT, MDS_TYPES, MDS_EXCLUDE, MDS_HIDDEN,
             MDS_RELOAD, MDS_INDEX, MDS_OPEN, MDS_TOC, MDS_THEME

Examples:
  mds README.md
  mds ./docs
  git show HEAD~1:README.md | mds -
  mds -p 3000 -t md,png ./notes
  mds -x node_modules,dist ./project
  MDS_HOST=127.0.0.1 mds ./notes   # local only
`)
}

// commands lists the subcommand names, used for collision hints.
var commands = []string{"serve", "config", "update", "upgrade", "version", "help"}

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		warnCollision(args[0])
		switch args[0] {
		case "serve":
			args = args[1:]
		case "version", "-v", "--version":
			fmt.Println("mds " + version)
			return
		case "update", "upgrade":
			if err := update.Run(version, args[1:]); err != nil {
				fatal(err)
			}
			return
		case "config":
			var err error
			if len(args) > 1 && args[1] == "init" {
				err = initConfig(args[2:])
			} else {
				err = showConfig(args[1:])
			}
			if err != nil {
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
		port                                   int
		host, types, exclude, theme            string
		index, noIndex, noReload, hidden, open bool
		noTOC                                  bool
	)
	fs.BoolVar(&noTOC, "no-toc", false, "")
	fs.StringVar(&theme, "theme", "", "")
	fs.IntVar(&port, "port", 0, "")
	fs.IntVar(&port, "p", 0, "")
	fs.StringVar(&host, "host", "", "")
	fs.StringVar(&types, "types", "", "")
	fs.StringVar(&types, "t", "", "")
	fs.StringVar(&exclude, "exclude", "", "")
	fs.StringVar(&exclude, "x", "", "")
	fs.BoolVar(&hidden, "hidden", false, "")
	fs.BoolVar(&open, "open", false, "")
	fs.BoolVar(&index, "index", false, "")
	fs.BoolVar(&noIndex, "no-index", false, "")
	fs.BoolVar(&noReload, "no-reload", false, "")

	// Allow flags before and after the positional argument.
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

	var (
		target, root, indexFile string
		content                 []byte
	)
	if positional[0] == "-" {
		if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			fatal(fmt.Errorf("nothing to read: stdin is a terminal (try: cat file.md | mds -)"))
		}
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal(err)
		}
		content = data
		target = "stdin"
		root, _ = os.Getwd()
	} else {
		var err error
		target, err = filepath.Abs(positional[0])
		if err != nil {
			fatal(err)
		}
		info, err := os.Stat(target)
		if err != nil {
			fatal(err)
		}
		root, indexFile = target, ""
		if !info.IsDir() {
			root, indexFile = filepath.Dir(target), target
		}
	}

	cfg, src, err := config.Load(root)
	if err != nil {
		fatal(err)
	}
	// Flags win over everything else, but only when given explicitly.
	fs.Visit(func(f *flag.Flag) {
		flagSrc := "--" + f.Name
		if len(f.Name) == 1 {
			flagSrc = "-" + f.Name
		}
		switch f.Name {
		case "port", "p":
			cfg.Port, src["port"] = port, flagSrc
		case "host":
			cfg.Host, src["host"] = host, flagSrc
		case "types", "t":
			cfg.Types, src["types"] = config.SplitExts(types), flagSrc
		case "exclude", "x":
			cfg.Exclude, src["exclude"] = config.SplitList(exclude), flagSrc
		case "hidden":
			cfg.Hidden, src["hidden"] = true, flagSrc
		case "open":
			cfg.Open, src["open"] = true, flagSrc
		case "index":
			cfg.Index, src["index"] = true, flagSrc
		case "no-index":
			cfg.Index, src["index"] = false, flagSrc
		case "no-reload":
			cfg.Reload, src["reload"] = false, flagSrc
		case "no-toc":
			cfg.TOC, src["toc"] = false, flagSrc
		case "theme":
			if err := config.CheckTheme(theme); err != nil {
				fatal(err)
			}
			cfg.Theme, src["theme"] = theme, flagSrc
		}
	})

	handler, err := server.New(server.Options{
		Root:     root,
		Index:    indexFile,
		Content:  content,
		Types:    cfg.Types,
		Exclude:  cfg.Exclude,
		Hidden:   cfg.Hidden,
		Reload:   cfg.Reload,
		DirIndex: cfg.Index,
		TOC:      cfg.TOC,
		Theme:    cfg.Theme,
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
	if active := activeSources(src); len(active) > 0 {
		fmt.Printf("config: %s\n", strings.Join(active, ", "))
	}
	fmt.Println("listening on:")
	for _, u := range listenURLs(ln.Addr().(*net.TCPAddr)) {
		fmt.Println("  " + u)
	}
	if cfg.Open {
		go openBrowser(localURL(ln.Addr().(*net.TCPAddr)))
	}
	if err := http.Serve(ln, handler); err != nil {
		fatal(err)
	}
}

// localURL is the address a browser on this machine should use: loopback when
// the server listens on all interfaces, otherwise the bound address.
func localURL(addr *net.TCPAddr) string {
	ip := addr.IP.String()
	if addr.IP.IsUnspecified() {
		ip = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(ip, strconv.Itoa(addr.Port)) + "/"
}

// openBrowser opens url with the platform's default opener. Failure (no
// display over SSH, no opener installed) is reported but never fatal.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			fmt.Fprintln(os.Stderr, "note: could not open a browser (no display)")
			return
		}
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "note: could not open a browser: %v\n", err)
		return
	}
	go cmd.Wait()
}

// warnCollision prints a hint when a subcommand name also exists as a path in
// the current directory, since the subcommand always wins.
func warnCollision(arg string) {
	if !slices.Contains(commands, arg) {
		return
	}
	if _, err := os.Stat(arg); err != nil {
		return
	}
	fmt.Fprintf(os.Stderr, "note: running the %q command; to serve ./%s run: mds ./%s\n", arg, arg, arg)
}

// activeSources returns the non-default configuration sources in precedence
// order, e.g. ["~/.config/mds/config.toml", ".mds.toml", "MDS_HOST", "--port"].
// Each source appears once, in a short display form.
func activeSources(src config.Sources) []string {
	rank := map[string]int{}
	for _, k := range config.Keys {
		s := src[k]
		var name string
		var r int
		switch {
		case s == "default":
			continue
		case strings.HasPrefix(s, "global "):
			name, r = shortenHome(config.GlobalPath()), 1
		case strings.HasPrefix(s, "local "):
			name, r = config.LocalName, 2
		case strings.HasPrefix(s, "env "):
			name, r = strings.TrimPrefix(s, "env "), 3
		default:
			name, r = s, 4
		}
		rank[name] = r
	}
	names := make([]string, 0, len(rank))
	for n := range rank {
		names = append(names, n)
	}
	sort.SliceStable(names, func(i, j int) bool {
		if rank[names[i]] != rank[names[j]] {
			return rank[names[i]] < rank[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

func shortenHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// initConfig implements "mds config init [--local] [--force]".
func initConfig(args []string) error {
	fs := flag.NewFlagSet("mds config init", flag.ExitOnError)
	local := fs.Bool("local", false, "write .mds.toml in the current directory")
	force := fs.Bool("force", false, "overwrite an existing file")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: mds config init [--local] [--force]")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	path := config.GlobalPath()
	if *local {
		path = config.LocalPath(".")
	}
	if err := config.Init(path, *force); err != nil {
		return err
	}
	fmt.Println("wrote", path)
	return nil
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
