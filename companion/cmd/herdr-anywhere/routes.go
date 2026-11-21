package main

// The routes a phone reaches serve on, each end-to-end encrypted by TailPocket: Tailcat (from any network), this
// computer's local network, and a tailpocket-relay. Without --routes, serve --tailcat runs Tailcat, the local network
// unless --no-lan, and the relay --relay names, as before 0.3.6. --routes lists them exactly (TailPocket v0.3.2's
// connecthost.Config.Routes): --routes lan runs no Tailcat node at all and sends nothing to Tailcat's servers.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jt-wang/tailpocket/connecthost"
)

// routeNames are the routes --routes takes, in the order serve lists them.
var routeNames = []string{connecthost.RouteTailcat, connecthost.RouteLAN, connecthost.RouteRelay}

// routeSet checks serve's and start's route flags against each other. It returns the routes --routes lists, in
// routeNames' order, or nil without --routes. withTailcat is serve --tailcat; start passes true without --routes,
// as it runs serve --tailcat.
func routeSet(withTailcat bool, routes string, withLAN, noLAN, tailcatOnly, relay bool) ([]string, error) {
	if routes == "" {
		switch {
		case (noLAN || tailcatOnly) && !withTailcat:
			return nil, errors.New("--no-lan and --tailcat-only need --tailcat")
		case withLAN && !withTailcat:
			return nil, errors.New("--lan needs --tailcat or --routes: for the local network without Tailcat, use --routes lan")
		case relay && !withTailcat:
			return nil, errors.New("--relay needs --tailcat or --routes: for the relay without Tailcat, use --routes relay (or --routes lan,relay)")
		case tailcatOnly && (withLAN || relay):
			return nil, errors.New("--tailcat-only takes no --lan and no --relay")
		case noLAN && withLAN:
			return nil, errors.New("--no-lan and --lan contradict each other")
		}
		return nil, nil
	}
	if noLAN || tailcatOnly {
		return nil, errors.New("--routes replaces --no-lan and --tailcat-only: list the routes in --routes alone (--routes tailcat is Tailcat only)")
	}
	set := map[string]bool{}
	for _, r := range strings.Split(routes, ",") {
		r = strings.TrimSpace(r)
		switch {
		case r == "":
			return nil, fmt.Errorf("--routes %q has an empty item: list one or more of tailcat, lan, relay, separated by commas", routes)
		case r != connecthost.RouteTailcat && r != connecthost.RouteLAN && r != connecthost.RouteRelay:
			return nil, fmt.Errorf("--routes: unknown route %q; use tailcat, lan or relay", r)
		case set[r]:
			return nil, fmt.Errorf("--routes: %s is listed twice", r)
		}
		set[r] = true
	}
	var out []string
	for _, r := range routeNames {
		if set[r] {
			out = append(out, r)
		}
	}
	list := strings.Join(out, ",")
	switch {
	case withTailcat && !set[connecthost.RouteTailcat]:
		return nil, fmt.Errorf("--tailcat contradicts --routes %s, which leaves Tailcat out: drop --tailcat", list)
	case withLAN && !set[connecthost.RouteLAN]:
		return nil, fmt.Errorf("--lan contradicts --routes %s, which leaves the local network out: add lan to --routes", list)
	case relay && !set[connecthost.RouteRelay]:
		return nil, fmt.Errorf("--relay is given but --routes %s leaves the relay out: add relay to --routes", list)
	case set[connecthost.RouteRelay] && !relay:
		return nil, fmt.Errorf("--routes %s has relay: name it with --relay wss://… and --relay-token-file FILE", list)
	}
	return out, nil
}

// tailcatOn: routes run Tailcat, as they do without --routes.
func tailcatOn(routes []string) bool {
	for _, r := range routes {
		if r == connecthost.RouteTailcat {
			return true
		}
	}
	return routes == nil
}

// routeWords says routes as serve's log does: "the local network and the relay".
func routeWords(routes []string) string {
	words := map[string]string{connecthost.RouteTailcat: "Tailcat", connecthost.RouteLAN: "the local network", connecthost.RouteRelay: "the relay"}
	var w []string
	for _, r := range routes {
		w = append(w, words[r])
	}
	switch len(w) {
	case 0:
		return ""
	case 1:
		return w[0]
	}
	return strings.Join(w[:len(w)-1], ", ") + " and " + w[len(w)-1]
}

// absPath is p made absolute, so serve finds it at each login from wherever start ran; "" stays "".
func absPath(p string) string {
	if p == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// shellQuote is s as one word for a shell, for a command shown to the person.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:@=+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
