import { diag } from '../diag.ts'
import type { RowBox } from './pin-drag.ts'

// A diagnostic failure must not change the gesture or replace a write's rejection.
export function pinDiag(what: string, fields: Record<string, string>) {
  try { diag(what, fields) } catch {}
}
export function pinDropEvidence(gesture: number, data: {
  from: number; target: number; finger: number; scrolled: number; dy: number
  held?: { top: number; h: number }; list: { top: number; viewH: number }; rows: RowBox[]
}) {
  pinDiag('pin-drop', {
    gesture: String(gesture), from: String(data.from), target: String(data.target),
    finger: String(data.finger), scrolled: String(data.scrolled), dy: String(data.dy),
    held: data.held ? '1' : '0', heldTop: String(data.held?.top ?? 0), heldH: String(data.held?.h ?? 0),
    listTop: String(data.list.top), listH: String(data.list.viewH),
    rowYs: data.rows.map(r => r.y).join(','), rowHs: data.rows.map(r => r.h).join(','),
  })
}
export function pinWriteEvidence(gesture: number, from: number, target: number,
  write: () => Promise<void>, index: () => number): Promise<void> {
  const fields = { gesture: String(gesture), from: String(from), target: String(target) }
  pinDiag('pin-write-start', fields)
  const pending = write()
  const settled = (what: string) => {
    try { pinDiag(what, { ...fields, after: String(index()) }) } catch {}
  }
  void pending.then(() => settled('pin-write-success'), () => settled('pin-write-rejected'))
  return pending
}
