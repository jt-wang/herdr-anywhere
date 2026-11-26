// The progress scene's tree: the real `herdr-anywhere track tree --json` for the sample scene
// (fixtures/make-tree-v2.sh writes tree-v2.json and tree-en.json). The page only reads it: version one changes nothing from the phone.
// Panes t2 checkout, t3 payments, t4 mobile are the fake core's terminals in fixtures/fake.tsx's "progress" scene.
import { locale } from 'nsh'
import v2 from './tree-v2.json'
import en from './tree-en.json'

type Node = Record<string, any> & { id: string; children: Node[] }
export type FakeTree = { needs_you: any[]; just_done: any[]; tasks: Node[]; closed: Node[]; queue: number; organizer_waits?: { addr: string } }

/** The scene in the phone's language, as the sorting agent writes it: Chinese for a Chinese phone, else English. */
export function progressTree(): FakeTree {
  return JSON.parse(JSON.stringify(/^zh/i.test(locale()) ? v2 : en))
}

/** `herdr-anywhere progress agents` on a computer with five agents, its Codex too old, and on Windows its Oh My Pi
 *  installed as a .cmd script whose program can't be found. */
export const AGENTS = {
  agents: [
    { id: 'claude-code', name: 'Claude Code', cmd: 'claude --model claude-haiku-5-5', usable: true },
    { id: 'pi', name: 'Pi', cmd: 'pi', usable: true },
    { id: 'kimi-code', name: 'Kimi Code', cmd: 'kimi --yolo', usable: true },
    { id: 'codex', name: 'Codex', cmd: 'codex', usable: false, note: 'needs 0.163 or later to report whether it is busy; this computer has 0.150.0', needs: '0.163', version: '0.150.0' },
    { id: 'omp', name: 'Oh My Pi', cmd: 'omp', usable: false, note: "installed only as a .cmd script, which herdr-core can't start as an agent: C:\\npm\\omp.cmd", script: true },
  ],
  chosen: '',
}

function find(ts: Node[], id: string, above: Node[] = []): { node: Node; above: Node[] } | undefined {
  for (const t of ts) {
    if (t.id === id) return { node: t, above }
    const f = find(t.children, id, [...above, t])
    if (f) return f
  }
}

/** A task's agent asks the user something: the task waits on the user and joins "needs you". */
/** What the agent said last about task id (Markdown, as agents write). */
export function saidOn(tree: FakeTree, id: string, said: string) {
  const f = find(tree.tasks, id)
  if (f) f.node.said = said
}

export function waitOn(tree: FakeTree, id: string, waiting: string) {
  const f = find(tree.tasks, id)
  if (!f) return
  Object.assign(f.node, { group: 'waiting', stage: 'waiting', waiting })
  tree.needs_you = tree.needs_you.filter((x) => x.id !== id)
  tree.needs_you.push({ ...f.node, path: f.above.map((a) => a.outcome), updated: 9 })
}
