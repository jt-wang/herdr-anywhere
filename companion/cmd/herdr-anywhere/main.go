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
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/mdp/qrterminal/v3"

	"herdr-anywhere/companion/internal/chat"
	"herdr-anywhere/companion/internal/chat/claude"
	"herdr-anywhere/companion/internal/chat/codex"
	"herdr-anywhere/companion/internal/chat/opencode"
	"herdr-anywhere/companion/internal/chat/pi"
	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/lan"
	"herdr-anywhere/companion/internal/server"
	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
)

// A var so a test can build two versions of this program.
var version = "0.3.7"

const defaultPort = "7681"

const usage = `herdr-anywhere: reach herdr-core on this computer from a paired phone

usage:
  herdr-anywhere serve   [--listen 127.0.0.1:7681]... [--tailcat [--lan | --no-lan | --tailcat-only] | --routes tailcat,lan,relay [--lan]] [--relay URL --relay-token-file FILE] [--web DIR] [--hc PATH] [--state DIR]
  herdr-anywhere start   [--lan | --no-lan | --tailcat-only | --routes LIST] [--relay URL --relay-token-file FILE] [--listen ADDR]... [--state DIR]
  herdr-anywhere stop    [--state DIR]
  herdr-anywhere pair    [--name "Phone"] [--ttl 10m] [--url URL] [--json [--window ID [--answer yes|no]]] [--state DIR]
  herdr-anywhere devices [--json] [--state DIR]
  herdr-anywhere revoke  [--state DIR] <device id>
  herdr-anywhere rotate-identity [--yes] [--state DIR]   new keys for this computer; every phone pairs again
  herdr-anywhere install-dialog [--app-home DIR] [--state DIR]
  herdr-anywhere language   this computer's language for the Add phone dialog: en zh-Hans zh-Hant es ko ja ru
  herdr-anywhere update [--check]
  herdr-anywhere track tree|status|add|set|move|file|queue|place|scan|watch|report   (track help for more)
  herdr-anywhere progress on|off|status|agents   the experimental progress page, off by default
  herdr-anywhere chat-setup status|install|remove [agent] [--force]   small files some agents' chats need (Pi)

The state dir defaults to ~/.config/herdr-anywhere ($HERDR_ANYWHERE_STATE overrides).
With serve --tailcat running, pair shows an invite the Herdr app scans, then waits for the phone: it pairs only
once you have checked that the four emoji it shows are the ones pair shows, and answered y.
Phones on this computer's own network reach serve --tailcat directly, end-to-end encrypted; --no-lan turns that
off, and --tailcat-only keeps phones on Tailcat alone (no local network, no relay).
--routes lists the routes exactly, instead of --tailcat: --routes lan reaches phones only on this computer's own
network and sends nothing through Tailcat (away from home they don't connect), --routes relay only through your relay.
start runs serve --tailcat in the background, returns once it answers, and has it start again
whenever you log in (and after a crash); stop stops it and turns that off.
start --lan, --no-lan, --tailcat-only, --routes, --relay and --listen ADDR go to that serve, at each login too; a later start without them takes them out.
With no serve running, pair starts one the way start does.
install-dialog adds an Add phone dialog to the Herdr desktop app, which does all of this without a terminal;
start adds it too, and start, serve and update keep it up to date.
When pair's QR code doesn't fit the terminal, pair opens it as an image (pair-qr.png in the state dir).
The dialog speaks the computer's language ($HERDR_ANYWHERE_LANG overrides it).
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
	case "stop":
		err = stop(rest, stdout, stderr)
	case "pair":
		err = pair(rest, stdout, stderr)
	case "install-dialog":
		err = installDialog(rest, stdout, stderr)
	case "language":
		err = languageCommand(rest, stdout, stderr)
	case "devices":
		err = devices(rest, stdout, stderr)
	case "revoke":
		err = revoke(rest, stdout, stderr)
	case "rotate-identity":
		err = rotateIdentity(rest, stdout, stderr)
	case "update":
		err = updateCommand(ctx, rest, stdout, stderr)
	case "track":
		err = trackCommand(rest, stdout, stderr)
	case "progress":
		err = progressCommand(rest, stdout, stderr)
	case "chat-setup":
		err = chatSetupCommand(rest, stdout, stderr)
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
	var rot rotateRestart
	switch {
	case errors.As(err, &rot):
		// serve has stopped, its host with it: the identity moves aside unread, and serve starts again without it. A
		// phone that paired during the wait is unpaired too: nothing can pair now, and it holds the old host key.
		if err := revokeAll(rot.dir); err != nil {
			fmt.Fprintf(stderr, "herdr-anywhere: unpairing the phones: %v\n", err)
			return 1
		}
		if err := tailcat.Retire(rot.dir, rot.retired); err != nil {
			fmt.Fprintf(stderr, "herdr-anywhere: moving the old identity aside: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "rotate-identity: the old identity is in %s; starting again with a new one\n", rot.retired)
		if err := restartSelf(); err != nil {
			fmt.Fprintf(stderr, "herdr-anywhere: restarting: %v\n", err)
			return 1
		}
		return 0
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
	withLAN := fs.Bool("lan", false, "let paired phones reach this computer on its private LAN IPv4 address(es) on port 7682, end-to-end encrypted, and phones paired before 0.3 on the plain port for up to 3 days (needs --tailcat, or lan in --routes)")
	withTailcat := fs.Bool("tailcat", false, "also let paired phones reach this computer through Tailcat, from any network; phones on this computer's own network reach it directly too, end-to-end encrypted")
	noLAN := fs.Bool("no-lan", false, "with --tailcat: not directly on this computer's local network, only through Tailcat (and --relay)")
	tailcatOnly := fs.Bool("tailcat-only", false, "with --tailcat: through Tailcat alone, as before 0.3: no local network route and no relay")
	routesFlag := fs.String("routes", "", "exactly these routes, end-to-end encrypted, separated by commas: tailcat (from any network), lan (this computer's local network), relay (--relay); without tailcat nothing goes through Tailcat")
	relayURL := fs.String("relay", "", "also register with this tailpocket-relay (wss://…), end-to-end encrypted (needs --relay-token-file, and --tailcat or relay in --routes)")
	relayTokenFile := fs.String("relay-token-file", "", "file holding the relay's token")
	web := fs.String("web", "", "serve the PWA from this directory")
	logTo := fs.String("log", "", "append what serve prints to this file (start's entry on Windows, which has no console)")
	hcFlag := fs.String("hc", "", "path to herdr-core's hc (default: hc or herdr-core on PATH, else in ~/.local/bin, on Windows in %LOCALAPPDATA%\\Programs\\herdr-core)")
	releases := releaseFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	if *logTo != "" {
		f, err := os.OpenFile(*logTo, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		stdout, stderr = f, f
		detachConsole()
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	// A new version of serve (after an update, at login) brings an installed Add phone dialog up to date, also
	// when an older version did the update and knew nothing of this.
	if home, err := herdrAppHome(""); err == nil {
		if _, err := refreshDialog(home); err != nil {
			fmt.Fprintf(stderr, "could not bring the Add phone dialog up to date: %v\n", err)
		}
	}
	routes, err := routeSet(*withTailcat, *routesFlag, *withLAN, *noLAN, *tailcatOnly, *relayURL != "")
	if err != nil {
		return usageError{err}
	}
	// secure: TailPocket's end-to-end encrypted host runs, on the routes --tailcat or --routes name.
	secure := *withTailcat || routes != nil
	// Before anything listens: a second serve would start this computer's TailPocket host a second time.
	if secure && tailcat.ServeRunning(st.Dir()) {
		err := fmt.Errorf("Companion is already running on %s (herdr-anywhere stop stops it)", st.Dir())
		if atLogin().StartedThis() {
			// Not a failure to the service manager, which would otherwise start it again every few seconds for as
			// long as the other one runs.
			fmt.Fprintf(stderr, "herdr-anywhere serve: %v\n", err)
			return nil
		}
		return err
	}
	relays, err := relayOption(*relayURL, *relayTokenFile)
	if err != nil {
		return usageError{err}
	}
	var lanAddrs []netip.Addr
	if *withLAN {
		if lanAddrs, err = lan.SystemAddresses(); err != nil {
			return err
		}
		if len(lanAddrs) == 0 {
			return usageError{errors.New("--lan: this computer has no private IPv4 LAN address (Wi-Fi or Ethernet)")}
		}
	}
	addrs, err := listenAddrs(listen, false, nil)
	if err != nil {
		return usageError{err}
	}
	for _, a := range addrs {
		host, _, _ := net.SplitHostPort(a)
		if ip, err := netip.ParseAddr(host); host != "localhost" && (err != nil || !ip.IsLoopback()) {
			fmt.Fprintf(stderr, "warning: --listen %s is plain HTTP: anyone who can reach it sees the traffic and device tokens. Use it only on your own VPN; --lan is end-to-end encrypted\n", a)
		}
	}
	plain, err := openPlainLAN(st, *withLAN, listen, lanAddrs, addrs, time.Now())
	if err != nil {
		return err
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
	rotating := make(chan string, 1) // rotate-identity: the name the old identity goes to once serve stopped
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
	setups := chatSetups(hcPath)
	srv := server.New(server.Config{
		Store: st, HC: hcPath, HCVersion: hcVersion,
		Name: computerName(), Version: version, WebDir: *web, Logger: logger,
		Updates: updates, PlainUntil: plain.Until,
		// The chat view's readers, by the app name each agent reports to herdr-core.
		// Ctx: OpenCode 2 says which session it shows only while it works; from its first chat the Service keeps
		// what it said, until serve ends.
		Chat:       &chat.Service{HC: hcPath, Log: logger, Readers: chatReaders(setups), Ctx: ctx},
		ChatSetups: setups,
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
	if err := plain.serve(ctx, srv.Handler(), stdout, logger); err != nil {
		httpSrv.Close()
		return err
	}
	defer plain.close()
	if secure {
		opts := tailcat.Options{Routes: routes, Relays: relays, NoLAN: *noLAN, TailcatOnly: *tailcatOnly}
		for _, a := range lanAddrs {
			opts.Listen = append(opts.Listen, net.JoinHostPort(a.String(), strconv.Itoa(secureLANPort)))
		}
		closeTailcat, err := serveTailcat(ctx, st, httpSrv, srv.Revoking, urls, errc, logger, opts, rotateHandler(st, rotating, logger))
		if err != nil {
			httpSrv.Close()
			return err
		}
		defer closeTailcat()
		if tailcatOn(routes) {
			fmt.Fprintln(stdout, "listening on Tailcat; phones pair by scanning `herdr-anywhere pair`")
		} else {
			fmt.Fprintf(stdout, "listening for phones on %s, not through Tailcat; phones pair by scanning `herdr-anywhere pair`\n", routeWords(routes))
		}
	}
	fmt.Fprintf(stdout, "state in %s; pair a phone with: herdr-anywhere pair\n", st.Dir())
	if isTerminal(stdout) {
		fmt.Fprintln(stdout, foregroundTip(serveOpts{routes: routes, lan: *withLAN, noLAN: *noLAN, tailcatOnly: *tailcatOnly, listen: listen, relay: *relayURL, relayTokenFile: absPath(*relayTokenFile)}))
	}
	go updates.Run(ctx)
	if hcPath != "" {
		go progressLoop(ctx, hcPath, st.Dir(), stderr)
	}

	restarting, retired := false, ""
	select {
	case <-ctx.Done():
	case <-restart:
		restarting = true
	case retired = <-rotating:
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Close() // WebSockets are hijacked: Shutdown does not close them
	err = httpSrv.Shutdown(shutdown)
	if retired != "" {
		// The deferred closeTailcat stops the host before serve returns; main moves the identity aside after that,
		// as the host saves the identity it holds until it stops.
		return rotateRestart{dir: st.Dir(), retired: retired}
	}
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
func serveTailcat(ctx context.Context, st *store.Store, httpSrv *http.Server, revoking func() bool, urls []string, errc chan<- error, logger *log.Logger, opts tailcat.Options, rotate http.HandlerFunc) (func(), error) {
	tc, err := tailcat.Open(st, computerName(), logger, opts)
	if err != nil {
		return nil, err
	}
	if err := tc.Start(ctx); err != nil {
		tc.Close()
		return nil, err
	}
	for _, a := range opts.Listen {
		logger.Printf("listening on %s for phones on this network, end-to-end encrypted", a)
	}
	switch {
	case opts.Routes != nil && !tailcatOn(opts.Routes):
		logger.Printf("routes: %s; not through Tailcat (--routes %s)", routeWords(opts.Routes), strings.Join(opts.Routes, ","))
	case opts.Routes != nil:
		logger.Printf("routes: %s (--routes %s)", routeWords(opts.Routes), strings.Join(opts.Routes, ","))
	case opts.TailcatOnly:
		logger.Printf("Tailcat only (--tailcat-only): not on the local network, no relay")
	case opts.NoLAN:
		logger.Printf("not on the local network (--no-lan)")
	case len(opts.Listen) == 0 && opts.Routes == nil:
		logger.Printf("listening on this computer's private network addresses for phones on the same network, end-to-end encrypted (--no-lan turns it off)")
	}
	for _, r := range opts.Relays {
		logger.Printf("registering with relay %s, end-to-end encrypted", r.URL)
	}
	if n := tc.Unsynced(); n > 0 && tailcatOn(opts.Routes) && !opts.TailcatOnly && (!opts.NoLAN || len(opts.Relays) > 0) {
		logger.Printf("%d phone(s) paired before 0.3 reach this computer only through Tailcat until they open Herdr Anywhere 0.3 or later: ask their owners to open the new app soon", n)
	}
	ctl, err := tailcat.Listen(st.Dir())
	if err != nil {
		tc.Close()
		return nil, err
	}
	go func() { errc <- httpSrv.Serve(tc.Listener()) }()
	control := http.NewServeMux()
	control.Handle("/", tailcat.ControlHandler(removeImageAfter(st.Dir(), tc.Pair), tc, urls))
	control.HandleFunc("POST /rotate", rotate)
	ctlSrv := &http.Server{Handler: control, ReadHeaderTimeout: 10 * time.Second, ErrorLog: logger}
	go ctlSrv.Serve(ctl)
	watch, stopWatch := context.WithCancel(ctx)
	watched := make(chan struct{})
	go func() { tc.Watch(watch, time.Second, revoking); close(watched) }()
	return func() {
		stopWatch()
		ctlSrv.Close()
		<-watched // a revoke Watch is making saves tailcat.json: done before the host stops (rotate-identity moves it next)
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
	windowID := fs.String("window", "", "with --json: print what the phone pairing through this window (its \"id\") is doing")
	answer := fs.String("answer", "", "with --window: yes lets the phone asking pair, no ends the window")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	if *windowID != "" || *answer != "" {
		if !*asJSON || *windowID == "" || (*answer != "" && *answer != "yes" && *answer != "no") {
			return usageError{errors.New("--window needs --json; --answer (yes or no) needs --window")}
		}
		st, err := openStore(*state)
		if err != nil {
			return err
		}
		return windowJSON(st, *windowID, *answer, stdout)
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
	if errors.Is(err, tailcat.ErrNoServe) && *base == "" && readServeURLs(st.Dir()) == nil {
		reply, err = startForPair(ctx, st, code, stdout, stderr)
	}
	switch {
	case errors.Is(err, tailcat.ErrNoServe):
	case err != nil:
		return err
	default:
		printTailcatPair(stdout, reply, *base, code, *ttl, st.Dir())
		if reply.ID == "" { // a serve from before the emoji check
			return nil
		}
		return followPair(st.Dir(), reply, pairInput, stdout)
	}

	urlGiven := *base != ""
	if *base == "" {
		addrs, _ := lan.SystemAddresses()
		if len(addrs) == 0 {
			fmt.Fprintln(stderr, "no LAN address found; the link uses 127.0.0.1 (pass --url for a tailnet name)")
		}
		*base = pairBase(readServeURLs(st.Dir()), addrs)
	}
	link := pairLink(*base, code)
	rest := fmt.Sprintf("\nScan with the phone, or open:\n  %s\n\nSingle use; expires in %s (at %s).\n%s\n",
		link, *ttl, time.Now().Add(*ttl).Format("15:04"), pairHint(readServeURLs(st.Dir()), urlGiven))
	showQR(stdout, link, st.Dir(), strings.Count(rest, "\n")+1)
	fmt.Fprint(stdout, rest)
	return nil
}

// pairHint says what the phone needs for the link, from how the running serve
// listens (urls, nil when none answers): where it listens, a serve the phone
// cannot reach, or the ways to start one. urlGiven is --url: the user named
// how the phone reaches serve (a proxy, a tailnet name), so loopback is fine.
func pairHint(urls []string, urlGiven bool) string {
	var reach []string
	for _, u := range urls {
		if reachableURL([]string{u}) != "" {
			reach = append(reach, u)
		}
	}
	switch {
	case len(urls) == 0:
		return "No `herdr-anywhere serve` is running. Start one the phone can reach:\n" + serveWays
	case len(reach) > 0:
		return "serve is listening on " + strings.Join(reach, ", ") + "."
	case urlGiven:
		return "serve is listening on " + strings.Join(urls, ", ") + "."
	default:
		return "serve listens only on 127.0.0.1, which the phone cannot reach. Stop it (`herdr-anywhere stop`), then start it with one of:\n" + serveWays
	}
}

// start runs serve --tailcat and keeps it running, also after a restart; serve itself stops with its terminal.
const serveWays = `  herdr-anywhere start                              from any network, end-to-end encrypted; also starts at login
  herdr-anywhere start --lan                        also directly on the same Wi-Fi or Ethernet, end-to-end encrypted
  herdr-anywhere start --listen <VPN address>:7681  also over your own VPN (a Tailscale 100.x address, say)`

// startForPair: with no serve at all, a link pair printed would open nothing (after a restart, the phone got
// a LAN link no serve answered). So pair starts Companion as start does, and shows its Tailcat invite.
func startForPair(ctx context.Context, st *store.Store, code string, stdout, stderr io.Writer) (tailcat.PairReply, error) {
	fmt.Fprintln(stdout, "Companion wasn't running, so it is starting in the background.")
	if err := startCompanion(st, serveOpts{}, stdout, stderr); err != nil {
		return tailcat.PairReply{}, fmt.Errorf("Companion isn't running, and starting it failed: %w", err)
	}
	return tailcat.RequestPair(ctx, st.Dir(), code)
}

func printQR(w io.Writer, text string) {
	qrterminal.GenerateWithConfig(text, qrterminal.Config{
		Level: qrterminal.L, Writer: w, HalfBlocks: true, QuietZone: qrQuietZone,
		BlackChar: qrterminal.BLACK_BLACK, WhiteBlackChar: qrterminal.WHITE_BLACK,
		WhiteChar: qrterminal.WHITE_WHITE, BlackWhiteChar: qrterminal.BLACK_WHITE,
	})
}

// printTailcatPair shows the Tailcat invite and, when the phone can reach serve directly (--url, or a non-loopback
// address serve listens on: --lan), the http link for the same code; then the invite's QR code, last, so the lines
// after it don't push its top off the screen. dir is where an image of the code goes when the terminal is too small.
func printTailcatPair(w io.Writer, r tailcat.PairReply, base, code string, ttl time.Duration, dir string) {
	invite := r.Invite
	fmt.Fprintf(w, "Scan this QR code with the Herdr Anywhere app, or paste the invite into it:\n  %s\n", invite)
	if base == "" {
		base = reachableURL(r.URLs)
	}
	if base != "" {
		fmt.Fprintf(w, "\nOn the same network, or in a browser, you can also open:\n  %s\n(expires in %s, at %s)\n",
			pairLink(base, code), ttl, time.Now().Add(ttl).Format("15:04"))
	}
	fmt.Fprintln(w)
	showQR(w, invite, dir, 2) // the line below, and the shell's prompt
	fmt.Fprintf(w, "Single use; expires in %s (at %s).\n", time.Until(r.Expires).Round(time.Second), r.Expires.Local().Format("15:04"))
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

// chatReaders: the chat view's readers, by the app name each agent reports to herdr-core in OSC 7501.
// chatReaders: setups are chatSetups' (Pi's reader asks its setup whether the file is there).
func chatReaders(setups map[string]chat.Setup) map[string]chat.Reader {
	piSetup, _ := setups["pi"].(*pi.Setup)
	return map[string]chat.Reader{
		"claude-code": &claude.Reader{Dir: chat.ClaudeDir()},
		"codex":       &codex.Reader{Dir: codex.Dir()},
		"pi":          &pi.Reader{Setup: piSetup},
		"opencode":    &opencode.Reader{},
	}
}
