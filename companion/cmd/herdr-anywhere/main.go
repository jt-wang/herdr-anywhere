// herdr-anywhere lets a paired phone reach herdr-core on this computer: `pair`
// prints a pairing QR code, `serve` bridges each phone WebSocket to its own
// `hc connect`.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/mdp/qrterminal/v3"

	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/lan"
	"herdr-anywhere/companion/internal/server"
	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
)

// A var so a test can build two versions of this program.
var version = "0.1.15"

const defaultPort = "7681"

const usage = `herdr-anywhere: reach herdr-core on this computer from a paired phone

usage:
  herdr-anywhere serve   [--listen 127.0.0.1:7681]... [--lan] [--tailcat] [--web DIR] [--hc PATH] [--state DIR]
  herdr-anywhere start   [--state DIR]
  herdr-anywhere pair    [--name "Phone"] [--ttl 10m] [--url URL] [--json] [--state DIR]
  herdr-anywhere devices [--json] [--state DIR]
  herdr-anywhere revoke  [--state DIR] <device id>
  herdr-anywhere install-dialog [--app-home DIR] [--state DIR]
  herdr-anywhere update [--check]

The state dir defaults to ~/.config/herdr-anywhere ($HERDR_ANYWHERE_STATE overrides).
With serve --tailcat running, pair shows a Tailcat invite the Herdr app scans.
start runs serve --tailcat in the background and returns once it answers.
install-dialog adds an Add phone dialog to the Herdr desktop app, which does all of this without a terminal.
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	return runContext(context.Background(), args, stdout, stderr)
}

// runContext is run with a context that stops serve, for tests.
func runContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "serve":
		err = serve(ctx, rest, stdout, stderr)
	case "start":
		err = start(rest, stdout, stderr)
	case "pair":
		err = pair(rest, stdout, stderr)
	case "install-dialog":
		err = installDialog(rest, stdout, stderr)
	case "devices":
		err = devices(rest, stdout, stderr)
	case "revoke":
		err = revoke(rest, stdout, stderr)
	case "update":
		err = updateCommand(ctx, rest, stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "--version", "version":
		fmt.Fprintln(stdout, "herdr-anywhere", version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", cmd, usage)
		return 2
	}
	var u usageError
	switch {
	case errors.Is(err, errRestart):
		if err := restartSelf(); err != nil {
			fmt.Fprintf(stderr, "herdr-anywhere: restarting: %v\n", err)
			return 1
		}
		return 0 // Windows: the new file runs in a new process (execSelf); elsewhere restartSelf doesn't return
	case errors.Is(err, errReported):
		return 1
	case errors.As(err, &u):
		fmt.Fprintf(stderr, "herdr-anywhere %s: %v\n", cmd, err)
		return 2
	case errors.Is(err, flag.ErrHelp):
		return 0
	case err != nil:
		fmt.Fprintf(stderr, "herdr-anywhere %s: %v\n", cmd, err)
		return 1
	}
	return 0
}

type usageError struct{ error }

func newFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("herdr-anywhere "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs, fs.String("state", "", "state dir (default ~/.config/herdr-anywhere, or $HERDR_ANYWHERE_STATE)")
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return usageError{err}
	}
	return nil
}

func stateDir(flagValue, env, home string) string {
	switch {
	case flagValue != "":
		return flagValue
	case env != "":
		return env
	default:
		return filepath.Join(home, ".config", "herdr-anywhere")
	}
}

func openStore(flagValue string) (*store.Store, error) {
	home, err := os.UserHomeDir()
	if err != nil && flagValue == "" && os.Getenv("HERDR_ANYWHERE_STATE") == "" {
		return nil, err
	}
	return store.Open(stateDir(flagValue, os.Getenv("HERDR_ANYWHERE_STATE"), home))
}

// computerName is the hostname without macOS's ".local".
func computerName() string {
	h, err := os.Hostname()
	if err != nil {
		return "Mac"
	}
	return strings.TrimSuffix(h, ".local")
}

// --- serve -------------------------------------------------------------

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

// listenAddrs turns --listen and --lan into host:port pairs. Wildcard hosts
// are refused: the core can run any command as the user.
func listenAddrs(listen []string, withLAN bool, lanAddrs []netip.Addr) ([]string, error) {
	if len(listen) == 0 {
		listen = []string{"127.0.0.1:" + defaultPort}
	}
	var out []string
	seen := map[string]bool{}
	add := func(a string) {
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	var port string
	for i, l := range listen {
		host, p, err := net.SplitHostPort(l)
		if err != nil {
			return nil, fmt.Errorf("--listen %q: %v", l, err)
		}
		ip, err := netip.ParseAddr(host)
		if host == "" || (err == nil && ip.IsUnspecified()) {
			return nil, fmt.Errorf("--listen %q: refusing to listen on every interface; name an address (127.0.0.1, or use --lan)", l)
		}
		if i == 0 {
			port = p
		}
		add(net.JoinHostPort(host, p))
	}
	if withLAN {
		if len(lanAddrs) == 0 {
			return nil, errors.New("--lan: this computer has no private IPv4 LAN address (Wi-Fi or Ethernet)")
		}
		for _, a := range lanAddrs {
			add(net.JoinHostPort(a.String(), port))
		}
	}
	return out, nil
}

func serve(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("serve", stderr)
	var listen listFlag
	fs.Var(&listen, "listen", "address to listen on (repeatable; default 127.0.0.1:7681)")
	withLAN := fs.Bool("lan", false, "also listen on this computer's private LAN IPv4 address(es)")
	withTailcat := fs.Bool("tailcat", false, "also let paired phones reach this computer through Tailcat, from any network")
	web := fs.String("web", "", "serve the PWA from this directory")
	hcFlag := fs.String("hc", "", "path to herdr-core's hc (default: hc on PATH, else ~/.local/bin/hc; on Windows herdr-core on PATH, else %LOCALAPPDATA%\\Programs\\herdr-core)")
	releases := releaseFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	var lanAddrs []netip.Addr
	if *withLAN {
		if lanAddrs, err = lan.SystemAddresses(); err != nil {
			return err
		}
	}
	addrs, err := listenAddrs(listen, *withLAN, lanAddrs)
	if err != nil {
		return usageError{err}
	}
	logger := log.New(stderr, "", log.LstdFlags)

	home, _ := os.UserHomeDir()
	hcPath, hcVersion, err := hcbin.Resolve(*hcFlag, exec.LookPath, home)
	if err != nil {
		if *hcFlag != "" {
			return err
		}
		logger.Printf("warning: %v; phones can pair but not connect until hc is installed", err)
	} else {
		logger.Printf("using %s (%s)", hcPath, hcVersion)
	}

	restart := make(chan struct{}, 1)
	client, err := releases()
	if err != nil {
		return err
	}
	updates := newUpdates(st.Dir(), client, logger, func() {
		select {
		case restart <- struct{}{}:
		default:
		}
	})
	srv := server.New(server.Config{
		Store: st, HC: hcPath, HCVersion: hcVersion,
		Name: computerName(), Version: version, WebDir: *web, Logger: logger,
		Updates: updates,
	})
	defer srv.Close()
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, ErrorLog: logger}

	var listeners []net.Listener
	for _, a := range addrs {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			for _, l := range listeners {
				l.Close()
			}
			return err
		}
		listeners = append(listeners, ln)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, len(listeners)+1)
	var urls []string
	for _, ln := range listeners {
		urls = append(urls, "http://"+ln.Addr().String())
		fmt.Fprintf(stdout, "listening on http://%s\n", ln.Addr())
		go func() { errc <- httpSrv.Serve(ln) }()
	}
	if err := writeServeURLs(st.Dir(), urls); err != nil {
		httpSrv.Close()
		return err
	}
	defer os.Remove(filepath.Join(st.Dir(), serveURLsFile))
	if *withTailcat {
		closeTailcat, err := serveTailcat(ctx, st, httpSrv, srv.Revoking, urls, errc, logger)
		if err != nil {
			httpSrv.Close()
			return fmt.Errorf("--tailcat: %w", err)
		}
		defer closeTailcat()
		fmt.Fprintln(stdout, "listening on Tailcat; phones pair by scanning `herdr-anywhere pair`")
	}
	fmt.Fprintf(stdout, "state in %s; pair a phone with: herdr-anywhere pair\n", st.Dir())
	go updates.Run(ctx)

	restarting := false
	select {
	case <-ctx.Done():
	case <-restart:
		restarting = true
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Close() // WebSockets are hijacked: Shutdown does not close them
	err = httpSrv.Shutdown(shutdown)
	if restarting {
		return errRestart
	}
	return err
}

// serveTailcat brings up the Tailcat host, serves handler's routes on it as
// on the TCP listeners (so /hc?token=… works unchanged), opens the control
// socket pair talks to, and keeps the host's allowlist in step with the
// device list, once a revoked phone's socket has closed (revoking). The
// returned func stops all of it.
func serveTailcat(ctx context.Context, st *store.Store, httpSrv *http.Server, revoking func() bool, urls []string, errc chan<- error, logger *log.Logger) (func(), error) {
	tc, err := tailcat.Open(st, computerName(), logger)
	if err != nil {
		return nil, err
	}
	if err := tc.Start(ctx); err != nil {
		tc.Close()
		return nil, err
	}
	ctl, err := tailcat.Listen(st.Dir())
	if err != nil {
		tc.Close()
		return nil, err
	}
	go func() { errc <- httpSrv.Serve(tc.Listener()) }()
	ctlSrv := &http.Server{Handler: tailcat.ControlHandler(tc.Pair, urls), ReadHeaderTimeout: 10 * time.Second, ErrorLog: logger}
	go ctlSrv.Serve(ctl)
	watch, stopWatch := context.WithCancel(ctx)
	go tc.Watch(watch, time.Second, revoking)
	return func() {
		stopWatch()
		ctlSrv.Close()
		tc.Close()
	}, nil
}

// --- pair ----------------------------------------------------------------

func pairLink(base, code string) string {
	return strings.TrimRight(base, "/") + "/#pair=" + code
}

func pair(args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("pair", stderr)
	name := fs.String("name", "Phone", "name for the device until it gives its own")
	ttl := fs.Duration("ttl", 10*time.Minute, "how long the code is valid")
	base := fs.String("url", "", "the companion's URL as the phone reaches it (default http://<LAN IPv4>:7681)")
	asJSON := fs.Bool("json", false, "print the Tailcat invite and its QR matrix as JSON, for the Add phone dialog")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	if *ttl <= 0 {
		return usageError{errors.New("--ttl must be positive")}
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	if *asJSON {
		return pairJSON(st, *name, *ttl, stdout)
	}
	code, err := st.CreateCode(*name, *ttl)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	reply, err := tailcat.RequestPair(ctx, st.Dir(), code)
	switch {
	case errors.Is(err, tailcat.ErrNoServe):
	case err != nil:
		return err
	default:
		printTailcatPair(stdout, reply, *base, code, *ttl)
		return nil
	}

	if *base == "" {
		addrs, _ := lan.SystemAddresses()
		if len(addrs) == 0 {
			fmt.Fprintln(stderr, "no LAN address found; the link uses 127.0.0.1 (pass --url for a tailnet name)")
		}
		*base = pairBase(readServeURLs(st.Dir()), addrs)
	}
	link := pairLink(*base, code)
	printQR(stdout, link)
	fmt.Fprintf(stdout, "\nScan with the phone, or open:\n  %s\n\nSingle use; expires in %s (at %s). `herdr-anywhere serve --lan` must be running.\n",
		link, *ttl, time.Now().Add(*ttl).Format("15:04"))
	return nil
}

func printQR(w io.Writer, text string) {
	qrterminal.GenerateWithConfig(text, qrterminal.Config{
		Level: qrterminal.L, Writer: w, HalfBlocks: true, QuietZone: 2,
		BlackChar: qrterminal.BLACK_BLACK, WhiteBlackChar: qrterminal.WHITE_BLACK,
		WhiteChar: qrterminal.WHITE_WHITE, BlackWhiteChar: qrterminal.BLACK_WHITE,
	})
}

// printTailcatPair shows the Tailcat invite as the QR code, and the http link
// for the same code when the phone can reach serve directly: --url, or a
// non-loopback address serve listens on (--lan).
func printTailcatPair(w io.Writer, r tailcat.PairReply, base, code string, ttl time.Duration) {
	printQR(w, r.Invite)
	fmt.Fprintf(w, "\nScan with the Herdr app, or paste this invite into it:\n  %s\n\nSingle use; expires in %s (at %s).\n",
		r.Invite, time.Until(r.Expires).Round(time.Second), r.Expires.Local().Format("15:04"))
	if base == "" {
		base = reachableURL(r.URLs)
	}
	if base != "" {
		fmt.Fprintf(w, "\nOn the same network, or in a browser, you can also open:\n  %s\n(expires in %s, at %s)\n",
			pairLink(base, code), ttl, time.Now().Add(ttl).Format("15:04"))
	}
}

// reachableURL is the first of serve's URLs a phone could reach: not loopback.
func reachableURL(urls []string) string {
	for _, u := range urls {
		host, _, err := net.SplitHostPort(strings.TrimPrefix(u, "http://"))
		if ip, perr := netip.ParseAddr(host); err == nil && perr == nil && !ip.IsLoopback() {
			return u
		}
	}
	return ""
}

// serve records where it listens, so pair's link uses serve's port.
const serveURLsFile = "serve.json"

// writeServeURLs records where this serve listens, and its process.
func writeServeURLs(dir string, urls []string) error {
	b, _ := json.Marshal(map[string]any{"urls": urls, "pid": os.Getpid()})
	return os.WriteFile(filepath.Join(dir, serveURLsFile), b, 0o600)
}

// readServeURLs is what the running serve recorded, or nil when no serve
// answers there (none started, or one that crashed and left the file).
func readServeURLs(dir string) []string {
	b, err := os.ReadFile(filepath.Join(dir, serveURLsFile))
	if err != nil {
		return nil
	}
	var f struct {
		URLs []string `json:"urls"`
	}
	if json.Unmarshal(b, &f) != nil || len(f.URLs) == 0 {
		return nil
	}
	c, err := net.DialTimeout("tcp", strings.TrimPrefix(f.URLs[0], "http://"), 300*time.Millisecond)
	if err != nil {
		return nil
	}
	c.Close()
	return f.URLs
}

// pairBase is the companion's URL for the pairing link: an address serve
// listens on that the phone can reach, else this Mac's LAN address on serve's
// port (or the default port when serve is not running).
func pairBase(urls []string, lanAddrs []netip.Addr) string {
	if u := reachableURL(urls); u != "" {
		return u
	}
	port := defaultPort
	if len(urls) > 0 {
		if _, p, err := net.SplitHostPort(strings.TrimPrefix(urls[0], "http://")); err == nil {
			port = p
		}
	}
	host := "127.0.0.1"
	if len(lanAddrs) > 0 {
		host = lanAddrs[0].String()
	}
	return "http://" + net.JoinHostPort(host, port)
}

// --- devices / revoke -----------------------------------------------------

func devices(args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("devices", stderr)
	asJSON := fs.Bool("json", false, "print the devices as JSON, for the Add phone dialog")
	if err := parse(fs, args); err != nil {
		return err
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	ds, err := st.Devices()
	if err != nil {
		return err
	}
	if *asJSON {
		return devicesJSON(ds, stdout)
	}
	if len(ds) == 0 {
		fmt.Fprintln(stdout, "no paired devices (pair one with: herdr-anywhere pair)")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tCREATED\tLAST SEEN")
	for _, d := range ds {
		seen := "never"
		if !d.LastSeen.IsZero() {
			seen = d.LastSeen.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Created.Local().Format("2006-01-02 15:04"), seen)
	}
	return tw.Flush()
}

func revoke(args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("revoke", stderr)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return usageError{errors.New("usage: herdr-anywhere revoke <device id> (see herdr-anywhere devices)")}
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	id := fs.Arg(0)
	if err := st.Revoke(id); err != nil {
		return fmt.Errorf("%s: %w", id, err)
	}
	// serve --tailcat drops the phone from Tailcat when it notices (tailcat.Service.Watch).
	fmt.Fprintf(stdout, "revoked %s; a running serve closes its connections within a second\n", id)
	return nil
}
