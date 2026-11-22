// Placed by Herdr Anywhere, so its phone app can show this Pi's conversation.
// It reports the session file, process number and tree position to the local terminal (with `hc label`).
// Nothing goes over the network; no other Pi setting is changed.
// To remove it: tap Remove at "Pi chat" in the app's settings, or run `herdr-anywhere chat-setup remove pi`, or delete
// this file. Pi stops using it after /reload or a restart.
// herdr-anywhere chat setup: pi 2
// @ts-nocheck

import { execFile } from "node:child_process";

const HC = "{{HC}}";

export default function (pi) {
  const terminal = process.env.HC_TERMINAL_ID;
  if (!terminal) {
    return; // not in a herdr-core terminal
  }
  let pending = Promise.resolve();
  const report = (_event, ctx) => {
    const file = ctx?.sessionManager?.getSessionFile?.();
    if (!file) {
      return; // Pi runs without a conversation file (--no-session)
    }
    const leaf = ctx.sessionManager.getLeafId() ?? "~";
    const entries = ctx.sessionManager.getEntries();
    const tail = entries.at(-1)?.id ?? "~";
    pending = pending.then(() => new Promise(resolve => {
      execFile(HC, ["label", terminal, `herdr-anywhere.chat.pid=${process.pid}`, `herdr-anywhere.chat.session=${file}`,
        `herdr-anywhere.chat.leaf=${leaf}:${tail}`], { timeout: 5000 }, () => resolve());
    }));
    return pending;
  };
  pi.on("session_start", report);
  pi.on("session_tree", report);
}
