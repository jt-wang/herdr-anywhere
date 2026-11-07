// Picking a new space's or tab's folder by browsing the computer instead of typing it from memory: the companion lists a folder's subfolders and says when one doesn't
// exist (companion/README.md, GET/POST /api/folders). A typed path that doesn't exist is said so, with the close names
// next to it, and Create folder when the folder above it exists: a mistyped folder higher up is never made on the way.
// Nothing is made without that tap.
import { Signal } from '../store.ts'
import { t } from '../strings/index.ts'

export type Folder = { name: string; git: boolean; hidden?: boolean }
export type Listing = { path: string; home: string; parent: string; git: boolean; folders: Folder[]; truncated?: boolean }
export type Missing = { missing: true; path: string; existing: string }
export type FolderApi = {
  /** `hidden`: dot folders too, marked `hidden`. */
  list(path: string, hidden?: boolean): Promise<Listing | Missing>
  /** Makes the folder and its missing parents; the folder made. */
  create(path: string): Promise<string>
}

/** How the computer writes paths, told by its home: `C:\\Users\\sam` is Windows, `\\` between parts and a drive
 * (`C:\\`) at the top, as the companion's Go answers there; anything else has `/` and `/` at the top. */
type Style = { sep: string; win: boolean }
const styleOf = (home: string): Style => (/^[A-Za-z]:\\/.test(home) ? { sep: '\\', win: true } : { sep: '/', win: false })
/** The top a path starts at (`/`, `C:\\`), or '' for one that isn't absolute. */
const rootOf = (p: string, s: Style) => (s.win ? (/^[A-Za-z]:\\/.test(p) ? p.slice(0, 3) : '') : p.startsWith('/') ? '/' : '')
const join = (dir: string, name: string, s: Style) => (dir.endsWith(s.sep) ? dir + name : dir + s.sep + name)
const dirname = (p: string, s: Style) => {
  const root = rootOf(p, s)
  const i = p.lastIndexOf(s.sep)
  return i < root.length ? root : p.slice(0, i)
}
const leaf = (p: string, s: Style) => p.slice(p.lastIndexOf(s.sep) + 1)
/** A top's name in the breadcrumb: `/`, or the drive (`C:`). */
const rootLabel = (root: string, s: Style) => (s.win ? root.slice(0, 2) : root)

/** `/Users/sam/src` reads `~/src` under home `/Users/sam`; `C:\\Users\\sam\\src` reads `~\\src`. */
export function tilde(path: string, home: string): string {
  if (!home) return path
  if (path === home) return '~'
  return path.startsWith(home + styleOf(home).sep) ? '~' + path.slice(home.length) : path
}

/** A typed `~…` or absolute path (`/…`; on Windows `C:\\…`, with `/` read as `\\`), absolute; other text is a filter. */
export function expand(text: string, home: string): string | undefined {
  const s = styleOf(home)
  let t = text.trim()
  if (s.win) t = t.replace(/\//g, '\\').replace(/^([A-Za-z]:)$/, '$1\\')
  if (t === '~') return home
  if (t.startsWith('~' + s.sep)) return home + t.slice(1)
  return rootOf(t, s) ? t : undefined
}

/** The folders whose names contain the text, any case; those that start with it first. */
export function filterFolders(folders: Folder[], text: string): Folder[] {
  const q = text.trim().toLowerCase()
  if (!q) return folders
  const has = folders.filter((f) => f.name.toLowerCase().includes(q))
  return [...has.filter((f) => f.name.toLowerCase().startsWith(q)), ...has.filter((f) => !f.name.toLowerCase().startsWith(q))]
}

/** Edits between two names, a swap of two neighbours counting as one (optimal string alignment). */
function distance(a: string, b: string): number {
  const d = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array(b.length).fill(0)])
  for (let j = 1; j <= b.length; j++) d[0][j] = j
  for (let i = 1; i <= a.length; i++) {
    for (let j = 1; j <= b.length; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1
      d[i][j] = Math.min(d[i - 1][j] + 1, d[i][j - 1] + 1, d[i - 1][j - 1] + cost)
      if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) d[i][j] = Math.min(d[i][j], d[i - 2][j - 2] + 1)
    }
  }
  return d[a.length][b.length]
}

/** The names a typed one is a slip of: a letter off for short names, about one in three for longer; nearest first. */
export function closeNames(name: string, folders: Folder[], max = 3): string[] {
  const q = name.toLowerCase()
  const allowed = Math.max(1, Math.floor(q.length / 3))
  return folders
    .map((f) => ({ name: f.name, d: distance(q, f.name.toLowerCase()) }))
    .filter((f) => f.d > 0 ? f.d <= allowed : f.name !== name)
    .sort((a, b) => a.d - b.d)
    .slice(0, max)
    .map((f) => f.name)
}

/** The path as parts to tap to go up: `~` and its folders inside home, `/` (or the drive) and its folders outside. */
export function crumbs(path: string, home: string): { label: string; path: string }[] {
  const s = styleOf(home)
  const inHome = !!home && (path === home || path.startsWith(home + s.sep))
  const root = rootOf(path, s) || s.sep
  const out = [inHome ? { label: '~', path: home } : { label: rootLabel(root, s), path: root }]
  let at = out[0].path
  for (const part of path.slice(inHome ? home.length : root.length).split(s.sep).filter(Boolean)) {
    at = join(at, part, s)
    out.push({ label: part, path: at })
  }
  return out
}

/** The folders spaces and terminals use, each once, most recently used first. */
export function recentFolders(used: { folder: string; at: number }[]): string[] {
  const last = new Map<string, number>()
  for (const u of used) if (u.folder) last.set(u.folder, Math.max(last.get(u.folder) ?? 0, u.at))
  return [...last].sort((a, b) => b[1] - a[1]).map(([f]) => f)
}

/** `create`: the folder to make and the one it goes in, when that one exists. */
export type MissingNote = { path: string; text: string; suggestions: string[]; create?: { name: string; parent: string } }

/** The picker's state: the folder listed, the text typed in its field, and a typed path that doesn't exist. */
export class FolderPicker {
  readonly listing = new Signal<Listing | undefined>(undefined)
  readonly text = new Signal('')
  readonly error = new Signal('')
  /** The folder a typed path names when that folder (or one above it) doesn't exist, and the nearest that does. */
  private gone?: { path: string; existing: Listing | undefined }
  private asked = 0
  private hidden = false
  private s: Style

  constructor(private api: FolderApi, private home: string) {
    this.s = styleOf(home)
  }

  /** "Show hidden": lists the folder again with its dot folders, or without. */
  async showHidden(on: boolean): Promise<void> {
    this.hidden = on
    const l = this.listing.get()
    if (l) await this.go(this.gone?.path ?? l.path)
  }

  /** The folder listed: what "Use this folder" picks. */
  here(): string | undefined {
    return this.gone && this.typed() ? undefined : this.listing.get()?.path
  }

  /** The folder above the one listed, for the "up" row; none at the top of the breadcrumb. */
  up(): { label: string; path: string } | undefined {
    const l = this.listing.get()
    if (!l) return undefined
    const c = crumbs(l.path, this.home)
    return c.length > 1 ? c[c.length - 2] : undefined
  }

  /** Recent folders, at home only, filtered by the search: each with its name (its last part, ~ for home) and path. */
  recent(folders: string[]): { name: string; path: string }[] {
    const l = this.listing.get()
    if (!l || l.path !== this.home || this.typed()) return []
    const q = this.text.get().trim().toLowerCase()
    return folders
      .map((path) => ({ name: path === this.home ? '~' : leaf(path, this.s) || rootLabel(rootOf(path, this.s), this.s), path }))
      .filter((f) => !q || f.name.toLowerCase().includes(q) || tilde(f.path, this.home).toLowerCase().includes(q))
  }

  /** Lists `path`; a missing one keeps the note and lists the nearest folder above it instead. */
  async go(path: string): Promise<void> {
    const n = ++this.asked
    this.error.set('')
    try {
      const r = await this.api.list(path, this.hidden)
      if (n !== this.asked) return
      if ('missing' in r) {
        const existing = await this.api.list(r.existing, this.hidden).catch(() => undefined)
        if (n !== this.asked) return
        this.gone = { path: r.path, existing: existing && !('missing' in existing) ? existing : undefined }
        this.listing.set(this.gone.existing)
        return
      }
      this.gone = undefined
      this.listing.set(r)
    } catch (e) {
      if (n === this.asked) this.error.set(e instanceof Error ? e.message : String(e))
    }
  }

  /** The field: a name filters the folder shown; a path goes to its folder and filters by its last part. */
  async type(text: string): Promise<void> {
    this.text.set(text)
    const t = this.typed()
    if (t && this.listing.get()?.path !== t.dir && this.gone?.path !== t.dir) await this.go(t.dir)
  }

  private typed(): { dir: string; leaf: string } | undefined {
    const abs = expand(this.text.get(), this.home)
    if (abs === undefined) return undefined
    const root = rootOf(abs, this.s)
    if (abs.endsWith(this.s.sep)) {
      let dir = abs
      while (dir.endsWith(this.s.sep) && dir.length > root.length) dir = dir.slice(0, -1)
      return { dir, leaf: '' }
    }
    return { dir: dirname(abs, this.s), leaf: leaf(abs, this.s) }
  }

  /** The subfolders on screen: the listed folder's, filtered by what is typed. */
  shown(): Folder[] {
    const l = this.listing.get()
    if (!l) return []
    const t = this.typed()
    if (this.gone && t) return []
    return filterFolders(l.folders, t ? t.leaf : this.text.get())
  }

  /** A typed path that names a folder that exists (not the one listed): it can be used as typed. */
  typedFolder(): string | undefined {
    const l = this.listing.get()
    const t = this.typed()
    if (!l || !t?.leaf || this.gone || !l.folders.some((f) => f.name === t.leaf)) return undefined
    return join(t.dir, t.leaf, this.s)
  }

  /** A typed path that doesn't exist: said plainly, with close names from the nearest folder that does. */
  missing(): MissingNote | undefined {
    const typed = this.typed()
    const l = this.listing.get()
    if (!typed) return undefined
    const s = this.s
    const path = typed.leaf ? join(typed.dir, typed.leaf, s) : typed.dir
    const note = (suggestions: string[]) => ({ path, text: t('pathMissing', { path: tilde(path, this.home) }), suggestions })
    if (this.gone) {
      // The first part that is missing is the slip; the rest of the typed path follows the close name.
      const near = this.gone.existing
      if (!near) return note([])
      const rest = path.slice(near.path.endsWith(s.sep) ? near.path.length : near.path.length + 1).split(s.sep)
      const tail = rest.slice(1).join(s.sep)
      return note(closeNames(rest[0], near.folders).map((n) => (tail ? join(join(near.path, n, s), tail, s) : join(near.path, n, s))))
    }
    if (!l || !typed.leaf || l.folders.some((f) => f.name === typed.leaf)) return undefined
    return { ...note(closeNames(typed.leaf, l.folders).map((n) => join(typed.dir, n, s))), create: { name: typed.leaf, parent: typed.dir } }
  }

  /** "Create folder": makes the missing folder on the computer, after that tap only, when the folder above exists. */
  async create(): Promise<string> {
    const m = this.missing()
    if (!m?.create) throw new Error('nothing to create')
    return this.api.create(m.path)
  }
}
