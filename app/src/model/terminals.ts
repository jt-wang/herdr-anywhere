// herdr-core's terminal records, kept true by its event stream (herdr-core/schema/README.md "Events";
// applied as herdr-app src/hc.rs `EventLines::apply` does).

export type Term = {
  address: string
  name?: string | null
  state: string
  pid?: number | null
  /** Core-owned creation/process identity, used to reject stale session metadata after a restart. */
  created_ms?: number
  identity?: unknown
  argv: string[]
  cwd: string
  labels: Record<string, string>
  keep?: boolean
  parent?: string | null
  title?: string
  cols?: number
  rows?: number
  status?: { state?: string; kind?: string; app?: string; title?: string; msg?: string } | null
  records?: Record<string, unknown>
  reported?: boolean
  turn?: number
  foreground?: any
  template?: string | null
  exit_code?: number | null
  reason?: string | null
  [k: string]: any
}

export type Applied = { changed: boolean; rereadDocs?: boolean; doc?: { machine?: string; name: string; version: number }; type?: string; terminal?: string }

export class Terminals {
  private map = new Map<string, Term>()
  private seq = -1
  /** Counts the changes applied: what was worked out from the records holds while it stays the same. */
  version = 0

  snapshot(seq: number, list: Term[]) {
    this.seq = seq
    this.version++
    this.map = new Map(list.map((t) => [t.address, { ...t }]))
  }

  get(addr: string) {
    return this.map.get(addr)
  }

  list(): Term[] {
    return [...this.map.values()]
  }

  apply(msg: any): Applied {
    if (msg.method === 'heartbeat') return { changed: false }
    if (msg.method === 'missed') {
      this.snapshot(msg.params.seq, msg.params.terminals ?? [])
      return { changed: true, rereadDocs: true }
    }
    if (msg.method !== 'event') return { changed: false }
    const { seq, type, terminal, data, machine } = msg.params
    if (seq <= this.seq) return { changed: false }
    this.seq = seq
    if (type === 'doc') return { changed: false, doc: { machine, name: data.name, version: data.version }, type }
    const t = this.map.get(terminal)
    switch (type) {
      case 'created':
      case 'restarted':
        this.map.set(terminal, { ...data })
        break
      case 'removed':
        this.map.delete(terminal)
        break
      default:
        if (!t) return { changed: false }
        switch (type) {
          case 'lost':
            t.pid = undefined
            t.foreground = undefined
            Object.assign(t, { reason: data.reason, exit_code: data.exit_code, signal: data.signal })
            break
          case 'finished':
            t.state = 'finished'
            t.foreground = undefined
            Object.assign(t, data)
            break
          case 'keep':
            t.keep = data.keep
            break
          case 'status':
            t.status = data.status
            t.records = data.records
            t.reported = data.reported
            break
          case 'turn':
            t.turn = data.turn
            break
          case 'title': {
            // An agent's spinner turning in front of its title changes nothing shown: no change, so no pass (store.ts).
            const before = cleanTitle(t.title)
            t.title = data.title
            if (cleanTitle(t.title) === before) return { changed: false }
            break
          }
          case 'labels':
            t.labels = data.labels ?? {}
            break
          case 'size':
            t.cols = data.cols
            t.rows = data.rows
            break
          case 'foreground':
            t.foreground = data ?? undefined
            break
          default:
            return { changed: false }
        }
    }
    this.version++
    return { changed: true, type, terminal }
  }
}

/** A title without an agent's spinner prefix (braille, Claude Code's stars and half circles). */
export function cleanTitle(s: string | undefined): string {
  return (s ?? '').replace(/^[⠀-⣿✳✶✻✽✢·◐-◓*✳✶✻✽✢·\s]+/u, '').trim()
}
