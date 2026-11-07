# Changelog

## v0.1.15

- The computer's side runs on Windows. Install it with the PowerShell steps in the README, or have your coding agent follow INSTALL.md. It updates itself from the phone as on a Mac, and the same file runs on Windows on Arm.
- In a pane you can type straight into the terminal with your phone's keyboard, or keep the input box. Switch with the `>_` button next to the input box and the pen in the key row; a tap on the note that appears keeps your choice for every pane. Attach is in the ⋯ menu.
- Typing into the terminal: Enter with Gboard no longer deletes a character, a picked correction replaces the word instead of adding it again, and the first backspace after a Chinese, Japanese or Korean word deletes it, in the terminal and in the input box.
- Holding ⇧ or ctrl darkens only that key, and the key row stays on one line on small phones and at the largest text size.
- Android: when an agent finishes, its notification now pops up, the same as when an agent needs you. Both play Herdr's own sounds through Android's notification, and the app no longer plays a second sound of its own while in the background. Settings › Alerts says what Android does with each kind and opens its page in Android's settings. A kind you had silenced or turned off stays that way after the update.
- In OpenCode and other programs that use the mouse, a drag moves the text as far as your finger, and a flick keeps going and slows down as it does in a plain terminal, even when your finger ends near the bottom of the screen. Before, the text ran about three times as far as the finger, and scrolling stopped when the finger ended over OpenCode's prompt box.
- A terminal that doesn't load opens again once, then says so and offers Try again. Before, it could stay on Connecting… for good.
- Long-pressing an agent on home opens a menu titled with what its row says first, its tab's name or title, with its space (and "Claude Code", when Show agent names is on) under it. Before, every Claude Code's menu read only "Claude Code".
- The strip between a terminal's last line and the input box now has the terminal's own colour. In OpenCode and other programs that paint their own background, it used to show as a lighter band.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or later can update to it from home. Mac: `herdr-anywhere-macos-arm64`. Windows: `herdr-anywhere-windows-amd64.exe`.

## v0.1.14

- Agent rows on home and in the switcher lead with what each agent is about: its tab's name or title is the main text, what it is doing sits under it in a readable grey, and "Claude Code" steps back to a small grey label. Idle agents are a step dimmer. Settings › Show agent names turns the label off; the icon still shows the program.
- Kimi's icon shows on the light theme. Its K was white and disappeared against the light background; it is now dark there, with its blue dot.
- The list that opens from an agent's name at the top of a pane now shows what home shows for each agent: its tab's name or title, its status in words, what it is doing, and its sub-agents. Before, every Claude Code there read only "Claude Code".
- In programs that use the mouse, such as OpenCode, a tap on a button presses it. Before, OpenCode's dialogs (Skip, Update) ignored taps.
- Pinned is the same on the phone and in Herdr. Agents pinned in Herdr show under Pinned at the top of home on the phone. Long-press an agent on the phone to pin or unpin it, and Herdr shows the change too. This needs Herdr 0.1.12 or later on the computer; older versions keep their pins to themselves.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or later can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.13

- A phone you remove on the computer while it is connected through TailPocket now goes back to pairing and says the computer no longer knows it. Before, it could keep reconnecting. This is fixed in the computer's program.
- A phone opened from a pairing link while already paired no longer tries that used link again after the computer forgets it.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or later can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.12

- A terminal keeps the phone's width when the connection drops and comes back while the app is in the background. Before, the width went to the computer, and what an agent wrote meanwhile stayed narrow in the phone's history.
- A link an agent wrapped onto the next line after Chinese text opens whole, so an X post link opens that post.
- Copying text an agent wrapped at the phone's width gives back its lines as the agent wrote them, without a line break at the end of every row.
- Android: back from allowing the app to install updates, the download starts without another tap. After the update installs, a notification says so and opens the app.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or later can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.11

Nothing changes in the app or on the computer. The tests and the secret scan that every pull request runs pass again.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or later can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.10

- Type in the terminal, from a pane's menu, brings the keyboard up again after the key row's button put it away.
- Every pull request runs the tests and a secret scan. [CONTRIBUTING.md](CONTRIBUTING.md) says which test goes with which change.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or later can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.9

Fixes from using the phone every day.

- A terminal keeps the phone's width after you leave it, until you use it on the computer. Before, leaving handed the width to a narrow split on the computer, and what an agent wrote meanwhile stayed narrow in its history.
- The sheet for switching tabs and starting agents no longer scrolls into a blank area.
- Opening that sheet with the keyboard up puts the keyboard away first, so it no longer covers the sheet.
- OpenCode follows the phone's dark or light theme, and shows on home.
- A finished notification shows what the agent says it did, which Claude Code sends a few seconds after it finishes.
- The key row has a Shift key that applies to the next key, as Ctrl does. Shift then Tab replaces the Shift+Tab key.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or later can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.8

Updates are signed.

- Each release carries a list of its files and their SHA-256, signed with a key built into the computer's program. The computer installs a new version, and tells the phone about a new app, only when the signature and the file match.
- The computer reads new versions from the release's download page and no longer uses GitHub's API, which limits each address to 60 requests an hour.

Updating from 0.1.6 or 0.1.7 works as before: those versions don't check the signature yet, and every version from 0.1.8 on does.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 or 0.1.7 can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.7

The computer asks GitHub about new versions only after you say it may.

- The second time you open the app, it asks whether the computer may check for new versions. Until you say yes, the computer sends nothing to GitHub.
- After a yes it checks once a day, where 0.1.6 checked every 6 hours by itself.
- Settings can turn the daily check on or off, or check right away. Installing new versions by itself still waits for you to turn it on, and turns the check on with it.
- If you turned on automatic updates in 0.1.6, that counts as a yes.

Android: `Herdr-Anywhere.apk`; an installed 0.1.6 can update to it from home. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.6

Full-screen programs scroll, a busy terminal no longer knocks the phone offline, and new versions now reach you by themselves.

**Connection**

- A terminal printing quickly no longer drops the connection. It could do so every couple of seconds, which left buttons doing nothing or saying the computer wasn't connected.
- A new tab tapped while the connection is coming back waits for it and then opens.

**The terminal**

- Swiping scrolls programs that take over the screen, such as Pi, with the same glide as everywhere else.
- The terminal uses every column that fits. A column was being lost on some screens, which cut Pi's `max` to `ma`.
- A terminal on the phone's screen takes the phone's width back after a dropped connection, where it used to stay at the width of a narrower pane on the computer.
- When an agent draws less than the screen holds, the empty rows under its last line give way to the output above it.
- One Esc on screen.
- Going to another screen puts the keyboard away, and a resized dark terminal has no white line at its edge.

**Notifications**

- A notification says what the agent asks or did, in its own words, and then which space and tab it is in.

**Updates**

- Home says when a new version is out, and one tap installs it: the computer first, then the app. On Android the system asks you to confirm, and the app says how large the download is first.
- The computer replaces itself and carries on as the new version, and phones reconnect by themselves. Nothing goes in unless it matches the release's digest and runs as that version.
- Settings can have the computer install new versions by itself. It's off until you turn it on.
- `herdr-anywhere update` does the same from a terminal, and `herdr-anywhere update --check` only says whether there is one.

The computer asks GitHub about new releases every 6 hours. The phone reaches GitHub only to download the app after you tap Download.

This is the last update you install by hand. Android: `Herdr-Anywhere.apk`, which updates an installed 0.1.5, and asks once for permission to install updates. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.5

Herdr Anywhere stays with your agents after you put the phone down, and the terminal now behaves like one on a phone.

**It stays connected**

- On Android the app keeps its connection in the background, with a quiet notification saying so, and an agent's question reaches you as a notification while you're away. Settings can turn this off. The app asks once to post notifications.
- A connection that died without a word no longer sits there showing Connected: the app notices in about 15 seconds and reconnects, through TailPocket too.

**The terminal**

- Scrolling carries on after your finger lifts and slows down.
- Lines with emoji and Chinese stay in their columns.
- The keyboard opening no longer resizes the terminal, which printed a long answer into the scrollback twice.
- Links open wherever they are, even wrapped across rows. Press and drag selects text.
- Two fingers no longer change the text size: it's in Settings.
- Settings and other screens open with the keyboard put away, not half hidden under it.

**Telling agents what to do**

- The composer grows with what you type, for several lines.
- The key row has Enter, and Ctrl turns the keys it can combine into Ctrl+Enter, Ctrl+Tab and Ctrl+arrows.
- Attach photos and files up to 2 GB: their path goes in your message, and the file lands where the terminal runs, on another machine too.

**Agents and panes**

- Add an agent from a space's header or from any space on home.
- Home and each pane show what the agent is doing.
- Split right and Split down work on a phone: it says what a small screen costs, then shows the panes side by side, and a tap picks the one you're typing to.

Android: `Herdr-Anywhere.apk`, which updates an installed 0.1.3. Mac: `herdr-anywhere-macos-arm64`, which attachments need.

## v0.1.4

The README's Try it goes straight to the steps. `INSTALL.md` checks the Mac and Herdr for you.

Build the app for your own iPhone under your own Apple account: `app/ios/README.md`. Try it on the pairing screen opens the app on sample data, with no computer. Settings and the pairing screen link to the privacy page.

No new app or companion: the download links still give the v0.1.3 files.

## v0.1.3

Herdr Anywhere has its own icon: a ram drawn for this app, with a curled horn, the prompt on its face and signal arcs above its head. It replaces the previous icon.

- On Android the icon follows the shape your launcher uses, and the notification icon is the new ram too.
- iOS builds now have an app icon.

Android: `Herdr-Anywhere.apk`, which updates an installed 0.1.2. Mac: `herdr-anywhere-macos-arm64`, unchanged apart from the version it prints.

## v0.1.2

The terminal now carries the symbols agents print, so they no longer depend on the fonts of the phone.

- Claude Code's `⎿` and Pi's spinner no longer show as a box with a question mark. Found on the iOS 26.3 simulator.
- More technical symbols come with the app too: `⎰`–`⎽` and `⏜`–`⏡`.

Android: `Herdr-Anywhere.apk`, which updates an installed 0.1.1. Mac: `herdr-anywhere-macos-arm64`.

## v0.1.1

Using Herdr Anywhere no longer takes a clone or Go. The release holds everything ready to run, under names that stay the same from one version to the next.

- `herdr-anywhere-macos-arm64`: the companion for Apple silicon Macs. The README's Try it installs it with one command.
- `Herdr-Anywhere.apk`: the Android app. It updates an installed 0.1.0.
- `INSTALL.md` now only downloads these files, so a coding agent can set it up without building anything. Building, for an Intel Mac, an iPhone or the browser, has its own section in the README.

## v0.1.0

The first public release. Use the agents running in Herdr on your Mac from your phone.

- See which agents need you, answer them, and open any terminal, from an Android or iOS app or a browser.
- Pair by pressing `cmd-shift-m` in the Herdr desktop app and scanning a QR code. It then connects from any network, end-to-end encrypted, with no account to create.
- Settings › Computer shows whether the connection is direct or through a relay. You can remove a phone from the dialog or with `herdr-anywhere revoke`.
- A sound and a toast when an agent needs you or finishes; system notifications on Android and in the browser while the app is alive.
- In English, 简体中文, 繁體中文, 日本語, 한국어, Español and Русский.

Android: `Herdr-Anywhere-0.1.0.apk` below. iOS: build it with Xcode, simulator tested so far.
