// This pairing's local read model and outbox. Never a source for shared layout writes.
import type { Document, View } from './model/doc.ts'
import type { FolderInfo } from './model/model.ts'
import type { Term } from './model/terminals.ts'
import type { ChatAnswer } from './model/chat.ts'

export type Draft = { address: string; text: string }
export type CachedChat = ChatAnswer & { historyLoaded?: boolean }
export type HomeSnapshot = {
  at: number
  docs: [string | null, Document][]
  terms: Term[]
  folders: [string, FolderInfo][]
  view: View
  home: string
}
export type ContentIO = { load: () => Promise<string | null>; save: (text: string) => Promise<void> }

export class Content {
  present = false
  home?: HomeSnapshot
  chats = new Map<string, CachedChat>()
  pending: Draft[] = []
  sending: Draft[] = []
  private writes = Promise.resolve()
  private last = ''
  constructor(private io?: ContentIO) {}

  async load() {
    const text = await this.io?.load()
    if (!text) return
    try {
      const v = JSON.parse(text)
      if (v.version !== 1 || !Array.isArray(v.chats) || !Array.isArray(v.pending) || !Array.isArray(v.sending)) return
      this.home = v.home
      // Cursors are leases from a Companion process, not durable evidence of earlier messages. This also drops
      // speculative cursors saved by older apps; the first live page must be allowed to replace them.
      this.chats = new Map(v.chats.map(([address, answer]: [string, CachedChat]) =>
        [address, { ...answer, before: undefined, historyLoaded: false }]))
      this.pending = v.pending
      this.sending = v.sending
      this.present = true
    } catch {}
  }

  remember(address: string, answer: CachedChat) {
    this.chats.delete(address)
    this.chats.set(address, answer)
    while (this.chats.size > 12) this.chats.delete(this.chats.keys().next().value!)
  }

  save() {
    if (!this.io) return Promise.resolve()
    // At most 12 chats, 40 recent turns each and 1M serialized characters in total. Keep whole turns, not silently
    // truncated messages. Pagination fetched this run remains in memory; a fresh remote read restores its cursor.
    const chats: [string, CachedChat][] = []
    let left = 1_000_000 - 2
    for (const [address, answer] of [...this.chats].reverse()) {
      const turns = answer.turns?.slice(-40) ?? []
      while (turns.length && JSON.stringify([address, { ...answer, turns }]).length + 1 > left) turns.shift()
      const a = { ...answer, turns: answer.turns ? turns : undefined, before: undefined, historyLoaded: false }
      const size = JSON.stringify([address, a]).length + 1
      if (size <= left) { chats.unshift([address, a]); left -= size }
    }
    const text = JSON.stringify({ version: 1, home: this.home, chats, pending: this.pending, sending: this.sending })
    if (text === this.last) return this.writes
    this.last = text
    // Serialize writes so a slow snapshot cannot overwrite a newer outbox. A failed write stays observable to the
    // send that awaits it; a later operation may try again.
    const write = this.writes.catch(() => {}).then(() => this.io!.save(text)).catch((e) => {
      if (this.last === text) this.last = ''
      throw e
    })
    this.writes = write
    return write
  }
}
