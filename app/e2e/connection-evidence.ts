import type { ConnSubscriberSite } from '../src/store.ts'
import type { Diag } from '../src/diag.ts'

export const CONNECTION_EVIDENCE_KEY = 'e2e-connection-diag'
const fieldsByEvent: Record<string, string[]> = {
  'firsttap': ['step', 'time'],
  'pin-grant': ['gesture', 'pageY'],
  'pin-lift': ['gesture'],
  'pin-release': ['gesture', 'moves', 'firstY', 'lastY', 'firstDy', 'lastDy', 'dy', 'lifted', 'dragging'],
  'pin-terminate': ['gesture', 'moves', 'firstY', 'lastY', 'firstDy', 'lastDy', 'dy', 'lifted', 'dragging'],
  'pin-drop': ['gesture', 'from', 'target', 'finger', 'scrolled', 'dy', 'held', 'heldTop', 'heldH', 'listTop', 'listH', 'rowYs', 'rowHs'],
  'pin-write-start': ['gesture', 'from', 'target'],
  'pin-write-success': ['gesture', 'from', 'target', 'after'],
  'pin-write-rejected': ['gesture', 'from', 'target', 'after'],
  'connection-connect': ['attempt', 'step'],
  'connection-connect-catch': ['attempt', 'name'],
  'connection-lost': ['attempt', 'wanted', 'retrying'],
  'connection-step': ['attempt', 'step'],
  'connection-return': ['attempt', 'reason'],
  'connection-retry-scheduled': ['attempt', 'delay'],
  'connection-retry-fired': ['attempt', 'wanted', 'hasClient'],
  'connection-error': ['attempt', 'step', 'name'],
}

// Only the test entry installs this hook. Local records do not depend on HTTP delivery.
export function connectionEvidence(options: {
  write: (key: string, value: string) => Promise<void>
  send: (what: string, fields: Record<string, string>) => void | Promise<unknown>
  now: () => number
  storageKey?: string
}) {
  const events: { seq: number; time: number; what: string; fields: Record<string, string> }[] = []
  let pending = Promise.resolve()
  const append = (what: string, fields: Record<string, string>) => {
    events.push({ seq: events.length + 1, time: options.now(), what, fields })
    const snapshot = JSON.stringify(events)
    // Serialize writes: an older snapshot can never finish after a newer one.
    pending = pending.then(() => options.write(options.storageKey ?? CONNECTION_EVIDENCE_KEY, snapshot)).catch(() => {})
  }
  const record: Diag = (what, fields) => {
    try {
      const keys = fieldsByEvent[what]
      if (keys) {
        const fixed: Record<string, string> = {}
        for (const key of keys) if (fields[key] !== undefined) fixed[key] = fields[key]
        append(what, fixed)
      }
    } catch { /* Diagnostics must not replace the Store's original exception. */ }
    // Local only: do not send these markers through the operation they measure.
    const mark = (phase: string) => {
      if (what !== 'connection-step' || fields.step !== 'conn-before') return
      try { append('connection-hook', { attempt: fields.attempt, step: 'conn-before', phase }) } catch {}
    }
    mark('send-before')
    try {
      const sent = options.send(what, fields)
      mark('send-return')
      void Promise.resolve(sent).catch(() => {})
    } catch { mark('send-threw') } // HTTP is optional evidence.
  }
  // Installed only on the real test app's connection Signal, before Root subscribes.
  const traceSubscribers = (conn: {
    set(value: string): void
    sub(f: (value: string) => void, site?: ConnSubscriberSite): () => unknown
  }) => {
    const set = conn.set.bind(conn), sub = conn.sub.bind(conn)
    let active = false, next = 0
    const callbacks = new WeakMap<(value: string) => void, (value: string) => void>()
    const mark = (phase: string, id: number) => {
      try { append('connection-subscriber', { phase, id: String(id) }) } catch {}
    }
    conn.sub = (f, site) => {
      let wrapped = callbacks.get(f)
      if (!wrapped) {
        const id = ++next
        try {
          append('connection-subscriber', { phase: 'registered', id: String(id), site: site ?? 'unlabelled' })
        } catch {}
        wrapped = (value) => {
          const recording = active
          if (recording) mark('enter', id)
          f(value)
          if (recording) mark('return', id)
        }
        callbacks.set(f, wrapped)
      }
      return sub(wrapped, site)
    }
    conn.set = (value) => {
      const previous = active
      active = value === 'reconnecting'
      try {
        if (active) mark('notify-enter', 0)
        set(value)
        if (active) mark('notify-return', 0)
      } finally { active = previous }
    }
  }
  return { record, traceSubscribers, flush: () => pending }
}
