// What the Add phone dialog decides, without drawing anything: tested by logic.test.mjs.
import { fill } from "./strings.js";

function json(text) {
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

// A finished `pair --json` as what the dialog shows. An error's message is the CLI's own detail, in English
// like all its output; an empty one means the answer could not be read.
export function parsePair({ code, stdout, stderr }) {
  const reply = json(stdout);
  if (reply?.error === "not-running") return { kind: "not-running" };
  if (reply?.error) return { kind: "error", message: reply.error };
  if (code === 0 && Array.isArray(reply?.qr) && reply.expires) {
    return { kind: "code", expires: Date.parse(reply.expires), rows: reply.qr };
  }
  return { kind: "error", message: stderr?.trim() || "" };
}

// null when the list could not be read: that is not the same as no phones.
export function parseDevices({ code, stdout }) {
  if (code !== 0) return null;
  const list = json(stdout);
  return Array.isArray(list) ? list : null;
}

// One row of the code as [dark, length] runs, so a row takes as few divs as it can.
export function runs(row) {
  const out = [];
  for (const c of row) {
    const dark = c === "1";
    const last = out[out.length - 1];
    if (last && last[0] === dark) last[1] += 1;
    else out.push([dark, 1]);
  }
  return out;
}

export function addedSince(before, after) {
  if (!before) return [];
  const known = new Set(before.map((d) => d.id));
  return after.filter((d) => !known.has(d.id));
}

export function remaining(expires, now) {
  const seconds = Math.max(0, Math.round((expires - now) / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

// How long ago a phone was last seen, in the words `s` (strings.js) and the phone app's short spans.
export function ago(lastSeen, now, s) {
  if (!lastSeen) return s.neverConnected;
  const minutes = Math.floor((now - Date.parse(lastSeen)) / 60000);
  if (minutes < 1) return s.lastSeenNow;
  const hours = Math.floor(minutes / 60);
  const age = minutes < 60 ? `${minutes}${s.units.m}` : hours < 24 ? `${hours}${s.units.h}` : `${Math.floor(hours / 24)}${s.units.d}`;
  return fill(s.lastSeen, { age });
}
