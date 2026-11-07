// The companion's folder calls (GET/POST /api/folders) over a made-up home, for tests and the demo fixture: the
// answers have the companion's shapes (companion/internal/server/folders.go). A home like `C:\Users\sam` makes it a
// Windows computer, answering as Go does there: `\` between parts, `/` typed turned into `\`, a drive's folder above
// is the drive.
import type { FolderApi, Listing, Missing } from '../model/folders.ts'

export class FakeFolders implements FolderApi {
  private dirs = new Set<string>()
  private git: Set<string>
  readonly created: string[] = []
  /** While set, answers wait for release(): a test of answers that come back late. */
  delay = false
  private waiting: (() => void)[] = []

  private sep: string

  /** `elsewhere`: folders outside home, as full paths. */
  constructor(private home: string, folders: string[], git: string[] = [], elsewhere: string[] = []) {
    this.sep = /^[A-Za-z]:\\/.test(home) ? '\\' : '/'
    for (const f of folders) this.add(`${home}${this.sep}${f}`)
    for (const f of elsewhere) this.add(f)
    this.add(home)
    this.git = new Set(git.map((g) => `${home}${this.sep}${g}`))
  }

  private root(p: string) { return this.sep === '/' ? '/' : p.slice(0, 3) }
  private up(p: string) { const i = p.lastIndexOf(this.sep); return i < this.root(p).length ? this.root(p) : p.slice(0, i) }

  private add(path: string) {
    for (let p = path; p !== this.root(p); p = this.up(p)) this.dirs.add(p)
    this.dirs.add(this.root(path))
  }

  release() { for (const w of this.waiting.splice(0)) w() }

  private abs(path: string) {
    let p = path.trim()
    if (this.sep === '\\') p = p.replace(/\//g, '\\')
    const a = p === '' || p === '~' ? this.home : p.startsWith('~' + this.sep) ? this.home + p.slice(1) : p
    let out = a
    while (out.endsWith(this.sep) && out.length > this.root(out).length) out = out.slice(0, -1)
    return out
  }

  async list(path: string, hidden = false): Promise<Listing | Missing> {
    if (this.delay) await new Promise<void>((r) => this.waiting.push(r))
    const p = this.abs(path)
    if (!this.dirs.has(p)) {
      let e = p
      do e = this.up(e); while (!this.dirs.has(e) && e !== this.root(e))
      return { missing: true, path: p, existing: e }
    }
    const prefix = p.endsWith(this.sep) ? p : `${p}${this.sep}`
    const folders = [...this.dirs]
      .filter((d) => d.startsWith(prefix) && d !== p && !d.slice(prefix.length).includes(this.sep))
      .map((d) => ({ name: d.slice(prefix.length), git: this.git.has(d), ...(d.slice(prefix.length).startsWith('.') ? { hidden: true } : {}) }))
      .filter((f) => hidden || !f.hidden)
      .sort((a, b) => (a.name.toLowerCase() < b.name.toLowerCase() ? -1 : 1))
    return { path: p, home: this.home, parent: this.up(p), git: this.git.has(p), folders }
  }

  async create(path: string): Promise<string> {
    const p = this.abs(path)
    this.created.push(p)
    this.add(p)
    return p
  }
}
