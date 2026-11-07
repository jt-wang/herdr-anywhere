// Add phone: a QR code the Herdr Anywhere phone app scans, with the phones already paired
// below it. Runs herdr-anywhere for everything. The code stays in memory: it
// comes from `pair --json` and is drawn as divs, never written to a file.
// Opened as a dialog (`mod.open add-phone`, bound by `herdr-anywhere install-dialog`).

import { View, div } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { exec, theme, props, closeOverlay } from "herdr";
import { BIN, STATE } from "./config.js";
import { addedSince, ago, parseDevices, parsePair, remaining, runs } from "./logic.js";

const MODULE = 5; // points per module: a 528-character invite is 81 modules across
const QUIET = 4; // modules of white around the code, as the QR standard asks

export default class AddPhone extends View {
  init(_props, cx) {
    this.view = { kind: "loading" };
    this.devices = null;
    this.confirming = null;
    this.handle = cx.focus_handle();
    this.handle.focus();
    this.getCode(cx);
    cx.timer.every(2000, (cx) => this.pollDevices(cx));
    cx.timer.every(1000, (cx) => {
      if (this.view.kind !== "code") return;
      if (Date.now() >= this.view.expires) this.view = { kind: "expired" };
      cx.notify();
    });
  }

  run(args) {
    return exec(BIN, [args[0], ...(STATE ? ["--state", STATE] : []), ...args.slice(1)]);
  }

  // The list comes first: a phone is "new" against what was there before the code.
  getCode(cx) {
    this.view = { kind: "loading" };
    cx.notify();
    cx.spawn(async (cx) => {
      try {
        this.devices = parseDevices(await this.run(["devices", "--json"])) ?? this.devices;
        this.view = parsePair(await this.run(["pair", "--json"]));
      } catch {
        this.view = { kind: "error", message: `Cannot run ${BIN}. Run herdr-anywhere install-dialog again.` };
      }
      cx.notify();
    });
  }

  start(cx) {
    this.view = { kind: "starting" };
    cx.notify();
    cx.spawn(async (cx) => {
      const r = await this.run(["start"]).catch((e) => ({ code: 1, stderr: String(e) }));
      if (r.code === 0) return this.getCode(cx);
      this.view = { kind: "start-failed", message: r.stderr.trim() };
      cx.notify();
    });
  }

  pollDevices(cx) {
    cx.spawn(async (cx) => {
      const now = parseDevices(await this.run(["devices", "--json"]).catch(() => ({ code: 1 })));
      if (!now || JSON.stringify(now) === JSON.stringify(this.devices)) return;
      const added = this.view.kind === "code" ? addedSince(this.devices, now) : [];
      this.devices = now;
      if (added.length) this.view = { kind: "paired", name: added[0].name };
      cx.notify();
    });
  }

  remove(id, cx) {
    this.confirming = null;
    cx.spawn(async (cx) => {
      const r = await this.run(["revoke", id]).catch((e) => ({ code: 1, stderr: String(e) }));
      if (r.code !== 0) this.view = { kind: "error", message: r.stderr.trim() };
      this.devices = parseDevices(await this.run(["devices", "--json"])) ?? this.devices;
      cx.notify();
    });
  }

  primary(cx) {
    const k = this.view.kind;
    if (k === "not-running" || k === "start-failed") this.start(cx);
    else if (k === "expired" || k === "paired" || k === "error") this.getCode(cx);
  }

  key(e, cx) {
    if (e.key === "escape") closeOverlay(props().overlay);
    else if (e.key === "enter") this.primary(cx);
    else return;
    cx.stop_propagation();
  }

  button(t, id, label, onClick, solid = true) {
    return div()
      .id(id)
      .h(32)
      .px(14)
      .flex()
      .items_center()
      .rounded(8)
      .when(solid, (el) => el.bg(t.solid).text_color(t.solid_foreground))
      .when(!solid, (el) => el.bg(t.raised).text_color(t.foreground))
      .text_size(12.5)
      .font_medium()
      .cursor_pointer()
      .on_click((_e, cx) => onClick(cx))
      .child(label);
  }

  // Runs of one colour share a div: an 81-module code took 3191 when measured.
  code(rows) {
    const size = (rows.length + QUIET * 2) * MODULE;
    const body = v_flex().size(size).p(QUIET * MODULE).bg("#ffffff").rounded(6);
    rows.forEach((row) => {
      const line = h_flex().h(MODULE).w_full();
      for (const [dark, length] of runs(row)) {
        line.child(div().h(MODULE).w(length * MODULE).when(dark, (el) => el.bg("#000000")));
      }
      body.child(line);
    });
    return body;
  }

  message(t, text, action) {
    const box = v_flex().items_center().justify_center().gap(14).py(40).px(12);
    box.child(div().text_size(13).text_color(t.foreground).child(text));
    return action ? box.child(action) : box;
  }

  body(t) {
    const v = this.view;
    switch (v.kind) {
      case "code":
        return v_flex()
          .items_center()
          .gap(12)
          .child(this.code(v.rows))
          .child(div().text_size(13).text_color(t.foreground).child("Open Herdr on your phone and scan this code."))
          .child(div().text_size(12).text_color(t.muted).child(`Works once. Expires in ${remaining(v.expires, Date.now())}.`));
      case "loading":
        return this.message(t, "Getting a code…");
      case "starting":
        return this.message(t, "Starting Herdr Anywhere…");
      case "not-running":
        return this.message(t, "Herdr Anywhere isn't running on this computer.", this.button(t, "ap-start", "Start", (cx) => this.start(cx)));
      case "start-failed":
        return this.message(t, `Herdr Anywhere didn't start: ${v.message}`, this.button(t, "ap-start", "Try again", (cx) => this.start(cx)));
      case "expired":
        return this.message(t, "This code expired.", this.button(t, "ap-new", "New code", (cx) => this.getCode(cx)));
      case "paired":
        return this.message(t, `${v.name} is paired.`, this.button(t, "ap-new", "Add another", (cx) => this.getCode(cx)));
      default:
        return this.message(t, v.message, this.button(t, "ap-new", "Try again", (cx) => this.getCode(cx)));
    }
  }

  phones(t) {
    const list = v_flex().id("ap-phones").max_h(160).overflow_y_scroll().mt(16).pt(12).gap(2).border_t_1().border_color(t.border);
    list.child(div().pb(4).text_size(12).font_medium().text_color(t.muted).child("Paired phones"));
    for (const d of this.devices) {
      const confirming = this.confirming === d.id;
      list.child(
        h_flex()
          .items_center()
          .h(36)
          .gap(10)
          .child(
            v_flex()
              .flex_1()
              .child(div().text_size(13).text_color(t.foreground).child(d.name))
              .child(div().text_size(11).text_color(t.muted).child(ago(d.last_seen, Date.now()))),
          )
          .child(
            div()
              .id(`ap-remove-${d.id}`)
              .px(8)
              .py(4)
              .rounded(6)
              .text_size(12)
              .text_color(confirming ? t.blocked : t.muted)
              .hover((el) => el.bg(t.hover))
              .cursor_pointer()
              .on_click((_e, cx) => {
                if (confirming) return this.remove(d.id, cx);
                this.confirming = d.id;
                cx.notify();
              })
              .child(confirming ? "Remove it?" : "Remove"),
          ),
      );
    }
    return list;
  }

  render() {
    const t = theme();
    return v_flex()
      .id("add-phone")
      .track_focus(this.handle)
      .on_key_down((e, cx) => this.key(e, cx))
      .w_full()
      .p(18)
      .rounded(12)
      .bg(t.popover)
      .text_color(t.foreground)
      .child(div().pb(14).text_size(15).font_semibold().child("Add phone"))
      .child(this.body(t))
      .when(Boolean(this.devices?.length), (el) => el.child(this.phones(t)));
  }
}
