package main

import (
	"strings"
	"testing"
)

// The route flags a person may combine, and what serve and start say about the ones that contradict each other.
func TestRouteSet(t *testing.T) {
	type flags struct {
		tailcat                     bool
		routes                      string
		lan, noLAN, tailcatOnly, rl bool
	}
	ok := []struct {
		f    flags
		want string
	}{
		{flags{tailcat: true}, ""},
		{flags{tailcat: true, lan: true, rl: true}, ""},
		{flags{routes: "lan"}, "lan"},
		{flags{routes: "relay,lan", rl: true}, "lan,relay"},
		{flags{routes: " relay ", rl: true}, "relay"},
		{flags{tailcat: true, routes: "lan,tailcat"}, "tailcat,lan"},
		{flags{routes: "lan", lan: true}, "lan"},
	}
	for _, c := range ok {
		got, err := routeSet(c.f.tailcat, c.f.routes, c.f.lan, c.f.noLAN, c.f.tailcatOnly, c.f.rl)
		if err != nil || strings.Join(got, ",") != c.want || (c.want == "") != (got == nil) {
			t.Errorf("%+v: %v, %v; want %q", c.f, got, err, c.want)
		}
	}
	bad := []struct {
		f    flags
		want string
	}{
		{flags{lan: true}, "use --routes lan"},
		{flags{rl: true}, "use --routes relay"},
		{flags{noLAN: true}, "need --tailcat"},
		{flags{tailcat: true, tailcatOnly: true, rl: true}, "--tailcat-only takes no --lan and no --relay"},
		{flags{routes: "lan", noLAN: true}, "--routes replaces --no-lan and --tailcat-only"},
		{flags{routes: ""}, ""}, // no flags at all is serve without a TailPocket host: fine
		{flags{routes: "lan,"}, "empty item"},
		{flags{routes: "wifi"}, `unknown route "wifi"`},
		{flags{routes: "lan,lan"}, "lan is listed twice"},
		{flags{tailcat: true, routes: "lan"}, "--tailcat contradicts --routes lan"},
		{flags{routes: "relay", lan: true, rl: true}, "add lan to --routes"},
		{flags{routes: "lan", rl: true}, "add relay to --routes"},
		{flags{routes: "lan,relay"}, "name it with --relay"},
	}
	for _, c := range bad {
		_, err := routeSet(c.f.tailcat, c.f.routes, c.f.lan, c.f.noLAN, c.f.tailcatOnly, c.f.rl)
		if c.want == "" {
			if err != nil {
				t.Errorf("%+v: %v", c.f, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: error %v, want one saying %q", c.f, err, c.want)
		}
	}
}

// serve says what it refuses before it starts anything, as a usage error (exit 2).
func TestServeRefusesContradictingRoutes(t *testing.T) {
	dir := shortStateDir(t)
	for _, args := range [][]string{
		{"serve", "--lan", "--state", dir},
		{"serve", "--routes", "lan,tailscale", "--state", dir},
		{"serve", "--tailcat", "--routes", "lan", "--state", dir},
		{"start", "--routes", "relay", "--state", dir},
		{"start", "--routes", "lan", "--tailcat-only", "--state", dir},
	} {
		_, errs, code := runCLI(t, args...)
		if code != 2 || !strings.Contains(errs, "--") {
			t.Errorf("%v: exit %d: %s", args, code, errs)
		}
	}
}
