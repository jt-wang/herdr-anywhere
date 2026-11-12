// Placed by Herdr Anywhere, so its phone app can show this Pi's conversation.
// It does one thing: when Pi starts or switches conversation, it tells the terminal (herdr-core, with `hc label`)
// which conversation file this Pi writes, and Pi's process number. Nothing goes over the network; no other Pi
// setting is changed.
// To remove it: tap Remove at "Pi chat" in the app's settings, or run `herdr-anywhere chat-setup remove pi`, or delete
// this file. Pi stops using it after /reload or a restart.
// herdr-anywhere chat setup: pi 1
// @ts-nocheck

import { execFile } from "node:child_process";

const HC = "{{HC}}";

export default function (pi) {
  const terminal = process.env.HC_TERMINAL_ID;
  if (!terminal) {
    return; // not in a herdr-core terminal
  }
  pi.on("session_start", (_event, ctx) => {
    const file = ctx?.sessionManager?.getSessionFile?.();
    if (!file) {
      return; // Pi runs without a conversation file (--no-session)
    }
    execFile(HC, ["label", terminal, `herdr-anywhere.chat.pid=${process.pid}`, `herdr-anywhere.chat.session=${file}`],
      { timeout: 5000 }, () => {});
  });
}
