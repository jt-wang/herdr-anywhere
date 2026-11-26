// Notifications from the progress tree: one per task that newly needs the user, and the gate that stops "done,
// please check" notifications for a goal the user keeps ignoring (from xiaok/hermes-autonomous).
import { expect, test } from 'nsh/test'
import { alertsFor, Outcomes } from './progress-alerts.ts'

const task = (id: string, group: string, goal = 'G1', more: Record<string, unknown> = {}) =>
  ({ id, outcome: `任务 ${id}`, stage: group, group, path: [goal === 'G1' ? 'Acme 1.0 发布' : '文档改版', '发版'], children: [], notes: [], done: 0, total: 1, evidence: [], pane_rows: [], ...more })
const tree = (...needs: any[]) => ({ needsYou: needs, justDone: [] as any[], tasks: [], closed: [], queue: 0 })
const finished = (...done: any[]) => ({ ...tree(), justDone: done })

test('only tasks that newly need the user ring, once each, saying where and what', () => {
  const o = new Outcomes()
  const first = alertsFor(null, tree(task('T1', 'waiting', 'G1', { waiting: '要 push 吗？' })), o, 0)
  expect(first).toEqual([]) // the first reading is what was already there when the app opened
  const a = alertsFor(tree(task('T1', 'waiting')), tree(task('T1', 'waiting'), task('T2', 'review', 'G1', { evidence: ['CHANGELOG.md'] })), o, 0)
  expect(a).toEqual([{ task: 'T2', goal: 'Acme 1.0 发布', kind: 'review', title: 'Acme 1.0 发布 › 发版', body: 'Done, to check: 任务 T2 (CHANGELOG.md)' }])
  const b = alertsFor(tree(task('T2', 'review')), tree(task('T2', 'review'), task('T3', 'waiting', 'G2', { waiting: '要运行 git commit 吗？' })), o, 0)
  expect(b.map((x) => [x.task, x.kind, x.body])).toEqual([['T3', 'wait', 'Waiting on you: 要运行 git commit 吗？']])
})

test('three review notifications for one goal left unopened in 30 days stop that goal\'s review notifications; waiting ones still ring', () => {
  const day = 86_400_000
  const o = new Outcomes()
  for (let i = 0; i < 3; i++) o.sent({ task: `R${i}`, goal: 'Acme 1.0 发布', kind: 'review' }, i * day)
  // Opened within an hour counts as seen; later or never counts as ignored.
  o.opened('R0', 0 + 2 * 3_600_000)
  expect(o.muted('Acme 1.0 发布', 'review', 3 * day)).toBe(true)
  const next = alertsFor(tree(), tree(task('T5', 'review'), task('T6', 'waiting')), o, 3 * day)
  expect(next.map((x) => x.task)).toEqual(['T6'])
  // A month later the old ones no longer count.
  expect(o.muted('Acme 1.0 发布', 'review', 40 * day)).toBe(false)
  // Opening one in time is not ignoring it.
  const p = new Outcomes()
  p.sent({ task: 'A', goal: 'G2', kind: 'review' }, 0); p.opened('A', 60_000)
  p.sent({ task: 'B', goal: 'G2', kind: 'review' }, 1); p.sent({ task: 'C', goal: 'G2', kind: 'review' }, 2)
  expect(p.muted('G2', 'review', 3 * day)).toBe(false)
  // Muting is per goal: another goal's review notifications still ring.
  expect(o.muted('文档改版', 'review', 3 * day)).toBe(false)
  // Waiting ones ring however many were ignored.
  for (let i = 0; i < 3; i++) o.sent({ task: `W${i}`, goal: 'Acme 1.0 发布', kind: 'wait' }, i * day)
  expect(o.muted('Acme 1.0 发布', 'wait', 3 * day)).toBe(false)
  // A task notified twice: opening it counts for the latest notification, and the earlier one stays ignored.
  const q = new Outcomes()
  q.sent({ task: 'A', goal: 'G3', kind: 'review' }, 0)
  q.sent({ task: 'A', goal: 'G3', kind: 'review' }, 2 * 3_600_000)
  q.opened('A', 2 * 3_600_000 + 60_000)
  q.sent({ task: 'B', goal: 'G3', kind: 'review' }, 3 * 3_600_000)
  expect(q.muted('G3', 'review', 3 * day)).toBe(false)
})

test('the record survives the app restarting and keeps only 30 days', () => {
  const o = new Outcomes()
  o.sent({ task: 'R1', goal: 'Acme 1.0 发布', kind: 'review' }, 0)
  o.sent({ task: 'R2', goal: 'Acme 1.0 发布', kind: 'review' }, 40 * 86_400_000)
  const back = Outcomes.from(o.save(41 * 86_400_000))
  expect(back.list().map((x) => x.task)).toEqual(['R2'])
  expect(Outcomes.from('not json').list()).toEqual([])
})

test('a top-level task says its own name and is muted on its own', () => {
  const o = new Outcomes()
  alertsFor(tree(), tree(), o, 0)
  const a = alertsFor(tree(), tree({ ...task('P2', 'review'), outcome: '比较几家支付服务商', path: [], said: '对比写好了，等你看' }), o, 0)
  expect(a).toEqual([{ task: 'P2', goal: 'P2', kind: 'review', title: '比较几家支付服务商', body: 'Done, to check: 对比写好了，等你看' }])
})

// Version one: the computer decides a task is done; the user hears once that it finished, and may look.
test('a task that just finished rings once, saying it is done', () => {
  const o = new Outcomes()
  alertsFor(null, finished(), o, 0)
  const a = alertsFor(finished(), finished(task('T2', 'done', 'G1', { said: '端口改好了' })), o, 0)
  expect(a).toEqual([{ task: 'T2', goal: 'Acme 1.0 发布', kind: 'review', title: 'Acme 1.0 发布 › 发版', body: 'Done: 任务 T2' }])
  expect(alertsFor(finished(task('T2', 'done')), finished(task('T2', 'done')), o, 0)).toEqual([])
  const top = alertsFor(finished(), finished({ ...task('P2', 'done'), outcome: '比较几家支付服务商', path: [], said: '对比写好了' }), o, 0)
  expect(top.map((x) => x.body)).toEqual(['Done: 对比写好了'])
})
