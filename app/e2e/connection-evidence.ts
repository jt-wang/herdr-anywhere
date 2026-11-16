import type { Diag } from '../src/diag.ts'

export const CONNECTION_EVIDENCE_KEY = 'e2e-connection-diag'
const fieldsByEvent: Record<string, string[]> = {
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
}) {
  const events: { seq: number; time: number; what: string; fields: Record<string, string> }[] = []
  let pending = Promise.resolve()
  const record: Diag = (what, fields) => {
    try {
      const keys = fieldsByEvent[what]
      if (keys) {
        const fixed: Record<string, string> = {}
        for (const key of keys) if (fields[key] !== undefined) fixed[key] = fields[key]
        events.push({ seq: events.length + 1, time: options.now(), what, fields: fixed })
        const snapshot = JSON.stringify(events)
        // Serialize writes: an older snapshot can never finish after a newer one.
        pending = pending.then(() => options.write(CONNECTION_EVIDENCE_KEY, snapshot)).catch(() => {})
      }
    } catch { /* Diagnostics must not replace the Store's original exception. */ }
    try { void Promise.resolve(options.send(what, fields)).catch(() => {}) } catch { /* HTTP is optional evidence. */ }
  }
  return { record, flush: () => pending }
}
