import assert from "node:assert/strict";
import childProcess from "node:child_process";
import { readFileSync } from "node:fs";
import { syncBuiltinESMExports } from "node:module";
import test from "node:test";

test("Pi and OMP report ordered leaf snapshots on start and tree changes", async () => {
  const execFile = childProcess.execFile;
  const terminal = process.env.HC_TERMINAL_ID;
  const calls = [];
  childProcess.execFile = (bin, args, options, done) => {
    calls.push({ bin, args, options });
    setImmediate(() => done(new Error("sample hc failure")));
  };
  syncBuiltinESMExports();
  try {
    for (const agent of ["pi", "omp"]) {
      calls.length = 0;
      const source = readFileSync(new URL(agent === "pi" ? "herdr-anywhere.ts" : "herdr-anywhere-omp.ts", import.meta.url), "utf8");
      const { default: extension } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);
      const handlers = {};
      const api = { on: (name, handler) => { handlers[name] = handler; } };
      delete process.env.HC_TERMINAL_ID;
      extension(api);
      assert.deepEqual(handlers, {});
      process.env.HC_TERMINAL_ID = "sample";
      extension(api);
      let leaf = "a1";
      let entries = [{ id: "a1" }, { id: "a2" }];
      const ctx = { sessionManager: {
        getSessionFile: () => "/sample/session.jsonl",
        getLeafId: () => leaf,
        getEntries: () => entries,
      } };
      const started = handlers.session_start({}, ctx);
      leaf = null;
      const switched = handlers.session_tree({ newLeafId: null }, ctx);
      await Promise.all([started, switched]);
      entries = [];
      await handlers.session_start({}, ctx);
      assert.deepEqual(calls.map(c => c.args.at(-1)), [
        "herdr-anywhere.chat.leaf=a1:a2",
        "herdr-anywhere.chat.leaf=~:a2",
        "herdr-anywhere.chat.leaf=~:~",
      ]);
      assert.ok(calls.every(c => c.args[0] === "label" && c.args[1] === "sample" && c.options.timeout === 5000));
      ctx.sessionManager.getSessionFile = () => undefined;
      await handlers.session_start({}, ctx);
      assert.equal(calls.length, 3);
    }
  } finally {
    childProcess.execFile = execFile;
    syncBuiltinESMExports();
    if (terminal === undefined) delete process.env.HC_TERMINAL_ID;
    else process.env.HC_TERMINAL_ID = terminal;
  }
});
