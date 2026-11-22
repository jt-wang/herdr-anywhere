// Placed by Herdr Anywhere, so its phone app can show this Oh My Pi's conversation.
// It reports only the session file, process number and tree position to the local terminal; no other setting is changed.
// To remove it: tap Remove at "Oh My Pi chat" in settings, run `herdr-anywhere chat-setup remove omp`, or delete
// this file. Oh My Pi stops using it after /restart.
// herdr-anywhere chat setup: omp 2
// @ts-nocheck

import { execFile } from "node:child_process";

const HC = "{{HC}}";

export default function (omp) {
  const terminal = process.env.HC_TERMINAL_ID;
  if (!terminal) {
    return;
  }
  let pending = Promise.resolve();
  const report = (_event, ctx) => {
    const file = ctx?.sessionManager?.getSessionFile?.();
    if (!file) {
      return;
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
  omp.on("session_start", report);
  omp.on("session_tree", report);
}
