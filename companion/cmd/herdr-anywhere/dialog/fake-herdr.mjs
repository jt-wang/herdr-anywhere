// Stands in for Herdr's modules so the tests run the real main.js under node.
import { registerHooks } from 'node:module'

const SOURCES = {
  'gpui-kit': `export const { View, div } = globalThis.fakeHerdr.kit;`,
  'gpui-base': `export const { v_flex, h_flex } = globalThis.fakeHerdr.kit;`,
  herdr: `export const { exec, theme, props, closeOverlay } = globalThis.fakeHerdr.herdr;`,
  config: `export const BIN = "herdr-anywhere"; export const STATE = "/state";`,
}

registerHooks({
  resolve(specifier, context, next) {
    if (specifier in SOURCES) return { url: `fake:${specifier}`, shortCircuit: true }
    if (specifier === './config.js') return { url: 'fake:config', shortCircuit: true }
    return next(specifier, context)
  },
  load(url, context, next) {
    if (url.startsWith('fake:')) return { format: 'module', source: SOURCES[url.slice(5)], shortCircuit: true }
    return next(url, context)
  },
})

// An element records its id, text, click handler and children, and takes any styling call.
function element() {
  const node = { $id: null, $kids: [], onClick: null, onKey: null }
  const self = new Proxy(node, {
    get(target, name) {
      if (name in target) return target[name]
      if (name === 'when') return (cond, f) => (cond ? f(self) : self)
      if (name === 'hover') return () => self
      return (...args) => {
        if (name === 'id') target.$id = args[0]
        if (name === 'child') target.$kids.push(args[0])
        if (name === 'on_click') target.onClick = args[0]
        if (name === 'on_key_down') target.onKey = args[0]
        return self
      }
    },
  })
  return self
}

export const replies = {}
export const calls = []
export const closed = []

globalThis.fakeHerdr = {
  kit: { View: class {}, div: element, v_flex: element, h_flex: element },
  herdr: {
    exec: async (bin, args) => {
      calls.push([bin, ...args].join(' '))
      // A reply may be a function of the arguments: pair --json and pair --json --window answer differently.
      const reply = typeof replies[args[0]] === 'function' ? replies[args[0]](args) : replies[args[0]]
      if (reply instanceof Error) throw reply
      return reply ?? { code: 0, stdout: '', stderr: '' }
    },
    theme: () => new Proxy({}, { get: (_, name) => String(name) }),
    props: () => ({ overlay: 'the-overlay' }),
    closeOverlay: (id) => closed.push(id),
  },
}

export function texts(node, out = []) {
  for (const k of node.$kids) typeof k === 'string' ? out.push(k) : texts(k, out)
  return out
}

export function find(node, id) {
  if (node.$id === id) return node
  for (const k of node.$kids) {
    if (typeof k === 'string') continue
    const hit = find(k, id)
    if (hit) return hit
  }
  return null
}

// What the dialog needs of its context; the timers are kept so a test can fire them.
export function context() {
  const pending = []
  const cx = {
    renders: 0,
    timers: [],
    focus_handle: () => ({ focus() {} }),
    notify() { cx.renders += 1 },
    stop_propagation() {},
    spawn(f) { pending.push(f(cx)) },
    timer: { every: (ms, f) => cx.timers.push([ms, f]) },
    settle: async () => { for (let i = 0; i < 20; i++) await new Promise((r) => setImmediate(r)) },
  }
  return cx
}
