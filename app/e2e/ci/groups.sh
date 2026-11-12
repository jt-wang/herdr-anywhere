#!/bin/sh
# Which of devices.yml's suite groups a run takes. Empty: all of them (a push always). A dispatch can name some,
# separated by commas or spaces; the rest are skipped. A name that is not a group fails the run, so a typo can't
# end green with nothing tested. ios_main_repeat or ios_b_repeat runs copies of that one iOS group and nothing else,
# so it can't be combined with a list.
# Usage: app/e2e/ci/groups.sh GROUPS MAIN_REPEAT B_REPEAT    prints ios=, android= (JSON lists of matrix keys),
#          update=, remote=, user_path= (true or false), one per line, for $GITHUB_OUTPUT
#        app/e2e/ci/groups.sh --self-test
# The groups, as devices.yml's jobs and matrix keys:
#   ios-main (ios, key a), ios-b (ios, key b), ios-onerror (ios, key onerror: an error the app's JS doesn't catch),
#   ios-title (ios, key title: the new-tab title on a 375 pt iPhone SE),
#   android-<key> (android: main, panes, tunnel, notify, progress, picture, title), android-update, remote-picture,
#   user-path.
# ios-onerror-27 (ios, key onerror27: the same built with Xcode 27.0 on iOS 27.0) runs only when named: the main suite
# has not yet passed on that runner image. So do ios-title and android-title, and every group added from now on: a
# push runs only the groups above, so it starts no more macOS jobs than before.
android_keys="main panes tunnel notify progress picture title"
android_default="main panes tunnel notify progress picture"
ios_keys="a b onerror title onerror27"
all="ios-main ios-b ios-onerror $(for k in $android_default; do printf 'android-%s ' "$k"; done)android-update remote-picture user-path"
named_only="ios-onerror-27 ios-title android-title"

json() { printf '['; sep=; for x in "$@"; do printf '%s"%s"' "$sep" "$x"; sep=,; done; printf ']\n'; }

plan() {
  list=$(printf '%s\n' "$1" | tr ', ' '\n\n' | grep -v '^$')
  main=${2:-0} b=${3:-0}
  if [ "$main" != 0 ] || [ "$b" != 0 ]; then
    [ -z "$list" ] || { echo "groups.sh: a run of copies (ios_main_repeat, ios_b_repeat) takes no groups: $1" >&2; return 2; }
    [ "$main" != 0 ] && ios=$(json a) || ios=$(json b)
    printf 'ios=%s\nandroid=[]\nupdate=false\nremote=false\nuser_path=false\n' "$ios"
    return 0
  fi
  [ -n "$list" ] || list=$(printf '%s\n' $all)
  for g in $list; do
    case " $all $named_only " in *" $g "*) ;; *) echo "groups.sh: no group named $g; the groups: $all $named_only" >&2; return 2 ;; esac
  done
  has() { printf '%s\n' "$list" | grep -qx "$1"; }
  set --
  has ios-main && set -- "$@" a
  has ios-b && set -- "$@" b
  has ios-onerror && set -- "$@" onerror
  has ios-title && set -- "$@" title
  has ios-onerror-27 && set -- "$@" onerror27
  echo "ios=$(json "$@")"
  set --
  for k in $android_keys; do has "android-$k" && set -- "$@" "$k"; done
  echo "android=$(json "$@")"
  has android-update && echo update=true || echo update=false
  has remote-picture && echo remote=true || echo remote=false
  has user-path && echo user_path=true || echo user_path=false
}

if [ "${1:-}" = --self-test ]; then
  fail=0
  check() { # what it prints for the arguments after the first, or the failure it must give
    want=$1; shift
    got=$(plan "$@" 2>&1 | tr '\n' ' ')
    case "$got" in *"$want"*) ;; *) echo "FAIL: groups.sh self-test: for \"$*\" it gave: $got (wanted: $want)"; fail=1 ;; esac
  }
  check 'ios=["a","b","onerror"] android=["main","panes","tunnel","notify","progress","picture"] update=true remote=true user_path=true' "" 0 0
  check 'ios=["title"] android=["title"] update=false remote=false user_path=false' "ios-title android-title" 0 0
  check 'ios=["onerror","onerror27"] android=[] update=false remote=false user_path=false' "ios-onerror ios-onerror-27" 0 0
  check 'ios=["onerror27"] android=[]' ios-onerror-27 0 0
  check 'ios=["b"] android=[] update=false remote=false user_path=false' ios-b 0 0
  check 'ios=["a"] android=["tunnel","picture"] update=false remote=false user_path=true' "android-picture, ios-main,android-tunnel user-path" 0 0
  check 'ios=[] android=[] update=true remote=true user_path=false' "android-update remote-picture" 0 0
  check 'no group named ios-mian' ios-mian 0 0
  check 'no group named android' android 0 0
  check 'ios=["a"] android=[] update=false remote=false user_path=false' "" 3 0
  check 'ios=["b"] android=[]' "" 0 2
  check 'takes no groups' ios-b 2 0
  plan ios-mian 0 0 >/dev/null 2>&1 && { echo "FAIL: groups.sh self-test: an unknown group exits 0"; fail=1; }
  # devices.yml runs each Android key (its job's name map and its case) and no other.
  yml="$(dirname "$0")/../../../.github/workflows/devices.yml"
  if [ -f "$yml" ]; then
    named=$(grep "^    name: Android \${{ fromJSON" "$yml" | grep -o '"[a-z]*":"' | tr -d '":' | tr '\n' ' ')
    [ "$named" = "$android_keys " ] || { echo "FAIL: groups.sh self-test: devices.yml names the Android keys $named, here: $android_keys"; fail=1; }
    for k in $android_keys; do
      grep -qE "^            $k\)\$" "$yml" || { echo "FAIL: groups.sh self-test: devices.yml's Android suites have no case for $k"; fail=1; }
    done
    # And each iOS key, in the iOS job's case and its name map.
    for k in $ios_keys; do
      grep -qE "^            $k\)\$" "$yml" || { echo "FAIL: groups.sh self-test: devices.yml's iOS suites have no case for $k"; fail=1; }
      grep "^    name: iOS \${{ fromJSON" "$yml" | grep -q "\"$k\":\"" || { echo "FAIL: groups.sh self-test: devices.yml's iOS job has no name for $k"; fail=1; }
    done
  fi
  [ "$fail" = 0 ] && echo "PASS: groups.sh self-test" || exit 1
else
  plan "$@"
fi
