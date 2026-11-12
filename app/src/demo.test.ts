import { expect, test } from 'nsh/test'
import { makeDemo, type Demo } from './demo.ts'
import { programOf } from './model/programs.ts'
import { settle } from './testing/fakecore.ts'

/** What a terminal shows: what the sample computer draws when the phone attaches (testing/fakecore.ts terminal.attach),
 *  then what it writes to it, as lines. */
function screens(d: Demo) {
  const out = new Map<string, string>()
  const output = d.core.output.bind(d.core)
  d.core.output = (addr, text) => { out.set(addr, (out.get(addr) ?? '') + text); output(addr, text) }
  return (addr: string) => {
    const drawn = d.core.screens.get(addr)
    return ((drawn ? drawn.replace(/\n/g, '\r\n') : `${addr}$ `) + (out.get(addr) ?? '')).split('\r\n')
  }
}

const addressOf = (d: Demo, tab: string) => [...d.core.terms.values()].find((t) => t.labels['herdr.tab'] === tab)!.address

async function open() {
  const d = makeDemo()
  await d.store.connect()
  await settle()
  return d
}

// App Review starts a new agent in Try it: before, its tab showed as "zsh", a terminal with only a prompt.
for (const key of ['claude', 'codex']) {
  const name = programOf(undefined, [key]).name
  test(`Try it: a new ${name} shows as ${name}, with its task`, async () => {
    const d = await open()
    const screen = screens(d)
    try {
      const r = await d.store.newTab({ space: 'sp2', argv: [key], task: 'say hello' })
      await settle()
      expect(d.store.program(r.terminal).name).toBe(name)
      expect(d.store.paneName(r.terminal)).toBe(name)
      expect(d.store.program(r.terminal).agent).toBe(true)
      expect(screen(r.terminal)).toContain('> say hello')
    } finally {
      d.stop(); d.store.disconnect()
    }
  })
}

test('Try it: a new terminal is still a shell', async () => {
  const d = await open()
  try {
    const r = await d.store.newTab({ space: 'sp2' })
    await settle()
    expect(d.store.program(r.terminal).shell).toBe(true)
  } finally {
    d.stop(); d.store.disconnect()
  }
})

// App Review types into a sample agent: before, the line ran on from the end of the sample's last line.
test('Try it: a line sent to an agent shows on its own line, under the sample screen', async () => {
  const d = await open()
  const screen = screens(d)
  try {
    const addr = addressOf(d, 'tb3')
    const before = screen(addr)
    await d.store.compose(addr, 'echo hi')
    await settle()
    const after = screen(addr)
    expect(after.slice(0, before.length)).toEqual(before)
    expect(after).toContain('> echo hi')
  } finally {
    d.stop(); d.store.disconnect()
  }
})

test('Try it: a line sent to a shell shows after its prompt', async () => {
  const d = await open()
  const screen = screens(d)
  try {
    const addr = addressOf(d, 'tb4')
    await d.store.compose(addr, 'ls')
    await settle()
    expect(screen(addr)).toContain('sam@studio web % ls')
  } finally {
    d.stop(); d.store.disconnect()
  }
})
