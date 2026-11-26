#!/bin/sh
# Which of devices.yml's suite groups a run takes. Empty: all of them (a push always). A dispatch can name some,
# separated by commas or spaces; the rest are skipped. A name that is not a group fails the run, so a typo can't
# end green with nothing tested. ios_main_repeat, ios_b_repeat or ios27_chat_repeat runs copies of that one iOS group
# and nothing else, so it can't be combined with a list.
# Usage: app/e2e/ci/groups.sh GROUPS MAIN_REPEAT B_REPEAT [CHAT27_REPEAT]    prints ios=, android= (JSON lists of matrix keys),
#          update=, remote=, user_path= (true or false), one per line, for $GITHUB_OUTPUT
#        app/e2e/ci/groups.sh --self-test
# The groups, as devices.yml's jobs and matrix keys:
#   ios-main (ios, key a), ios-b (ios, key b), ios-onerror (ios, key onerror: an error the app's JS doesn't catch),
#   android-<key> (android: main, panes, tunnel, notify, progress, picture), android-update, remote-picture, user-path.
# Run only when a dispatch names them: ios-onerror-27 (ios, key onerror27: the same built with Xcode 27.0 on iOS 27.0),
# as the main suite has not yet passed on that runner image; ios-ask and android-ask (ios and android, key ask: the
# decision card in the chat), so a push doesn't start more macOS jobs, and a change to the card names them; so does
# android-stuck (android, key stuck: the rows of choices following an agent through a call's questions); and, until
# they have passed on the runners a few times, ios-chat (ios, key chat: the chat view of four agents,
# e2e/ios-chat-four.sh), ios27-chat (ios, key chat27: the same with Xcode 27.0 on iOS 27.0), android-chat (android,
# key chat: e2e/android-chat-four.sh); and ios-chatfold (ios, key chatfold: screenshots of the chat view,
# e2e/ios-chat-four.sh with UITEST=ChatFold), for a change's before/after; and ios-subkeep and android-subkeep (ios
# and android, key subkeep: a sub-agent kept as a tab, with screenshots at 375 pt and 360 dp); and ios-title and
# android-title (key title: the new-tab title on a 375 pt iPhone SE and a 411 dp emulator). Every group added from now
# on runs only when named, so a push starts no more macOS jobs than before: ios-menus (ios, key menus:
# e2e/ios-menus.sh's menu and toast pictures on iOS 26.5, an iPhone 17 and an iPhone SE) and ios-menus-27 (key menus27:
# on iOS 27.0), pictures to compare a change's before and after; and ios-zoom and android-zoom (key zoom: a picture
# zoomed, opened from a terminal and from a chat); and ios-edge and ios-edge-27 (ios, keys edge and edge27: every swipe
# back from the left edge, ios-edge-back.sh, on iOS 26.5 and 27.0, and a recording of two): about an hour each.
# android-updnotify (android, key updnotify) and ios-notify (ios, key notify): the app's notifications of a new
# version, and on iOS how long after leaving the screen it can still post; and ios-kitty (ios, key kitty: Oh My
# Pi's pictures, e2e/ios-kitty.sh); and ios-pinorder and android-pinorder (key pinorder: Pinned order on an iPhone SE, 375 pt,
# and at 360 dp, light and dark, with screenshots; pin_order_before takes those of the build before).
# release: every group a push runs and the named ones that assert what the app does on the device (release_named),
# as the nightly run takes them and a release must pass on its own commit (release-gate.py). Not in it:
# ios-menus and ios-menus-27 (pictures only) and ios-edge-27 (ios-edge again on iOS 27, an hour).
android_keys="main panes tunnel notify progress picture"
android_named="ask stuck chat subkeep title zoom updnotify pinorder"
ios_keys="a b onerror zoom title onerror27 edge edge27 menus menus27 ask chat chat27 chatfold subkeep notify kitty pinorder"
all="ios-main ios-b ios-onerror $(for k in $android_keys; do printf 'android-%s ' "$k"; done)android-update remote-picture user-path"
release_named="ios-onerror-27 ios-edge ios-ask android-ask android-stuck ios-chat ios27-chat android-chat ios-chatfold ios-subkeep android-subkeep ios-title android-title ios-zoom android-zoom android-updnotify ios-notify ios-kitty ios-pinorder android-pinorder"
named_only="ios-onerror-27 ios-edge ios-edge-27 ios-ask android-ask android-stuck ios-chat ios27-chat android-chat ios-chatfold ios-subkeep android-subkeep ios-title android-title ios-menus ios-menus-27 ios-zoom android-zoom android-updnotify ios-notify ios-kitty ios-pinorder android-pinorder"

json() { printf '['; sep=; for x in "$@"; do printf '%s"%s"' "$sep" "$x"; sep=,; done; printf ']\n'; }

plan() {
  list=$(printf '%s\n' "$1" | tr ', ' '\n\n' | grep -v '^$')
  main=${2:-0} b=${3:-0} chat27=${4:-0}
  if [ "$main" != 0 ] || [ "$b" != 0 ] || [ "$chat27" != 0 ]; then
    [ -z "$list" ] || { echo "groups.sh: a run of copies (ios_main_repeat, ios_b_repeat, ios27_chat_repeat) takes no groups: $1" >&2; return 2; }
    if [ "$main" != 0 ]; then ios=$(json a); elif [ "$b" != 0 ]; then ios=$(json b); else ios=$(json chat27); fi
    printf 'ios=%s\nandroid=[]\nupdate=false\nremote=false\nuser_path=false\n' "$ios"
    return 0
  fi
  [ -n "$list" ] || list=$(printf '%s\n' $all)
  if printf '%s\n' "$list" | grep -qx release; then list=$(printf '%s\n' $list $all $release_named | grep -vx release | sort -u); fi
  for g in $list; do
    case " $all $named_only " in *" $g "*) ;; *) echo "groups.sh: no group named $g; the groups: $all $named_only" >&2; return 2 ;; esac
  done
  has() { printf '%s\n' "$list" | grep -qx "$1"; }
  set --
  has ios-main && set -- "$@" a
  has ios-b && set -- "$@" b
  has ios-onerror && set -- "$@" onerror
  has ios-zoom && set -- "$@" zoom
  has ios-title && set -- "$@" title
  has ios-onerror-27 && set -- "$@" onerror27
  has ios-edge && set -- "$@" edge
  has ios-edge-27 && set -- "$@" edge27
  has ios-menus && set -- "$@" menus
  has ios-menus-27 && set -- "$@" menus27
  has ios-ask && set -- "$@" ask
  has ios-chat && set -- "$@" chat
  has ios27-chat && set -- "$@" chat27
  has ios-chatfold && set -- "$@" chatfold
  has ios-subkeep && set -- "$@" subkeep
  has ios-notify && set -- "$@" notify
  has ios-kitty && set -- "$@" kitty
  has ios-pinorder && set -- "$@" pinorder
  echo "ios=$(json "$@")"
  set --
  for k in $android_keys $android_named; do has "android-$k" && set -- "$@" "$k"; done
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
  check 'ios=["zoom"] android=["zoom"] update=false remote=false user_path=false' "ios-zoom android-zoom" 0 0
  check 'ios=["onerror","onerror27"] android=[] update=false remote=false user_path=false' "ios-onerror ios-onerror-27" 0 0
  check 'ios=["onerror27"] android=[]' ios-onerror-27 0 0
  check 'ios=["ask"] android=["ask"] update=false remote=false user_path=false' "ios-ask android-ask" 0 0
  check 'ios=["a"] android=["main","ask"]' "ios-main android-main android-ask" 0 0
  check 'ios=[] android=["stuck"] update=false remote=false user_path=false' android-stuck 0 0
  check 'ios=[] android=["ask","stuck"]' "android-ask android-stuck" 0 0
  check 'ios=["menus","menus27"] android=[] update=false remote=false user_path=false' "ios-menus,ios-menus-27" 0 0
  check 'ios=["edge","edge27"] android=[] update=false remote=false user_path=false' "ios-edge-27 ios-edge" 0 0
  check 'ios=["notify"] android=["updnotify"] update=false remote=false user_path=false' "ios-notify android-updnotify" 0 0
  check 'ios=["kitty"] android=["picture"] update=false' "ios-kitty android-picture" 0 0
  check 'ios=["b"] android=[] update=false remote=false user_path=false' ios-b 0 0
  check 'ios=["chat","chat27"] android=["chat"] update=false remote=false user_path=false' "ios-chat ios27-chat android-chat" 0 0
  check 'ios=["chat27"] android=[]' ios27-chat 0 0
  check 'ios=["chatfold"] android=[] update=false remote=false user_path=false' ios-chatfold 0 0
  check 'ios=["pinorder"] android=["pinorder"] update=false remote=false user_path=false' "ios-pinorder android-pinorder" 0 0
  check 'ios=["a"] android=["tunnel","picture"] update=false remote=false user_path=true' "android-picture, ios-main,android-tunnel user-path" 0 0
  check 'ios=[] android=[] update=true remote=true user_path=false' "android-update remote-picture" 0 0
  check 'ios=["subkeep"] android=["subkeep"] update=false remote=false user_path=false' "ios-subkeep android-subkeep" 0 0
  check 'ios=["a","b","onerror","zoom","title","onerror27","edge","ask","chat","chat27","chatfold","subkeep","notify","kitty","pinorder"] android=["main","panes","tunnel","notify","progress","picture","ask","stuck","chat","subkeep","title","zoom","updnotify","pinorder"] update=true remote=true user_path=true' release 0 0
  check 'ios=["a","b","onerror","zoom","title","onerror27","edge","menus","ask"' "release, ios-menus" 0 0
  check 'takes no groups' release 1 0
  for g in $release_named; do case " $named_only " in *" $g "*) ;; *) echo "FAIL: groups.sh self-test: release names $g, which is not a named group"; fail=1 ;; esac; done
  check 'no group named ios-mian' ios-mian 0 0
  check 'no group named android' android 0 0
  check 'ios=["a"] android=[] update=false remote=false user_path=false' "" 3 0
  check 'ios=["b"] android=[]' "" 0 2
  check 'takes no groups' ios-b 2 0
  check 'ios=["chat27"] android=[] update=false remote=false user_path=false' "" 0 0 4
  check 'ios=["a"] android=[]' "" 1 0 4
  check 'takes no groups' ios27-chat 0 0 4
  plan ios-mian 0 0 >/dev/null 2>&1 && { echo "FAIL: groups.sh self-test: an unknown group exits 0"; fail=1; }
  # devices.yml runs each Android key (its job's name map and its case) and no other.
  yml="$(dirname "$0")/../../../.github/workflows/devices.yml"
  if [ -f "$yml" ]; then
    named=$(grep "^    name: Android \${{ fromJSON" "$yml" | grep -o '"[a-z]*":"' | tr -d '":' | tr '\n' ' ')
    [ "$named" = "$android_keys $android_named " ] || { echo "FAIL: groups.sh self-test: devices.yml names the Android keys $named, here: $android_keys $android_named"; fail=1; }
    for k in $android_keys $android_named; do
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
