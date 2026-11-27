// Trial (windows-agents.ps1): for each pane's `hc get --json`, the program the phone shows it as, and whether the phone
// lists it as an agent under its space. The phone's rule (Store.agents): a live terminal is an agent when the agent
// reports a status, or when its program is one Herdr starts as an agent (startable()). Its program: the agent's own
// report (status.app), else what runs in front (programOf).
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { programOf, startable } from '../../src/model/programs.ts'

const dir = process.argv[2]
const agents = new Set(startable().map((p) => p.key))
let n = 0
for (const f of readdirSync(dir).filter((f) => f.endsWith('.json')).sort()) {
  const raw = readFileSync(join(dir, f), 'utf8').replace(/^\uFEFF/, '')
  const j = JSON.parse(raw)
  const t = j.terminal ?? j
  const argv: string[] = t.foreground?.argv ?? t.argv ?? []
  const p = programOf(t.status?.app ?? null, argv)
  const state = t.status?.state ?? t.status ?? null
  const listed = t.state === 'live' && (!!t.status?.state || agents.has(p.key))
  console.log(`${f.replace(/\.json$/, '')}: state=${t.state} in front=${JSON.stringify(t.foreground?.name ?? null)} argv=${JSON.stringify(argv)} status=${JSON.stringify(state)} -> shown as ${p.name} (${p.key}), listed as an agent: ${listed ? 'yes' : 'NO'}`)
  n++
}
if (!n) { console.error('no pane reports'); process.exit(1) }
