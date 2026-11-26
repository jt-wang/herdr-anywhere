// The progress page's model over `herdr-anywhere track tree --json`: one nestable structure, the task.
// Version one is view only: the page shows, the computer decides.
import { expect, test } from 'nsh/test'
import { agentsLine, findTask, flagsOf, goalOf, homeOf, visibleRows, items, langLine, paneOf, panesOf, parseAgents, pathOf, pickLine, stageText, subLine, switchLine, taskOn, trackLine, type Task } from './progress.ts'
import { parseTree } from './progress.ts'

// Real output of `track tree --json` for the sample scene: fixtures/make-tree-v2.sh writes it.
const real = async () => parseTree(JSON.stringify((await import('../../fixtures/tree-v2.json')).default))

test('the tree reads: what needs the user, what just finished, and tasks with the tasks under them', async () => {
  const t = await real()
  expect(t.needsYou.map((x) => [x.id, x.group, x.waiting])).toEqual([['T6', 'waiting', '价格取整，还是保留到分？']])
  expect(t.justDone.map((x) => [x.id, x.said ?? '', x.path])).toEqual([
    ['T2', '', ['上线 Acme 商店的新结账流程', '让顾客能保存银行卡']], ['P2', '对比写好了，等你看', []]])
  expect(t.tasks.map((x) => x.id)).toEqual(['P1', 'P3'])
  expect(t.tasks[0].children.map((x) => x.id)).toEqual(['T1', 'T5', 'T6', 'T9'])
  expect(t.tasks[0].children[0].children.map((x) => [x.id, x.group])).toEqual([['T2', 'done'], ['T3', 'parked'], ['T4', 'parked']])
  expect(t.tasks[0].children[1].pane_rows).toEqual([]) // null in the JSON
  expect(t.queue).toBe(0)
  expect(parseTree('{}')).toEqual({ needsYou: [], justDone: [], tasks: [], closed: [], queue: 0, organizerWaits: undefined })
  expect(parseTree('{"queue":2,"organizer_waits":{"addr":"t8"}}').organizerWaits).toEqual({ addr: 't8' })
})

test('a card says the stage in the phone\'s words and, under it, why it waits or the agent\'s words', async () => {
  const t = await real()
  const p1 = t.tasks[0]
  // Stages come as codes; the phone words them (English here: tests run in English unless they say).
  expect(stageText('parked')).toBe('Queued')
  expect(stageText('waiting')).toBe('Waiting on you')
  expect(stageText('done')).toBe('Done')
  expect(subLine(p1.children[2])).toBe('价格取整，还是保留到分？')
  expect(subLine(p1.children[1])).toBe('等银行卡的活做完再跑 · 你说过：跑完告诉我')
  expect(items(p1.children[0])).toEqual([
    { done: true, text: '加上银行卡表单' }, { done: false, text: '输入时就校验卡号' }, { done: false, text: '让顾客能删掉存过的卡' }])
})

test('a task is found anywhere in the tree, with the titles above it', async () => {
  const t = await real()
  expect(findTask(t, 'T3')?.outcome).toBe('输入时就校验卡号')
  expect(pathOf(t, 'T3')).toEqual(['上线 Acme 商店的新结账流程', '让顾客能保存银行卡'])
  expect(pathOf(t, 'P2')).toEqual([])
  expect(findTask(t, 'nope')).toBe(undefined)
})

// herdr-core only: `--herdr -` keeps track from asking old Herdr for its agent list on the user's computer.
// Through the companion's /api/cli: the arguments as they are, no shell (a Windows computer has cmd.exe).
test('commands are the companion\'s own arguments, one list per run', () => {
  expect(trackLine(['tree', "P'1 & %x%"]).runs).toEqual([['track', 'tree', "P'1 & %x%", '--herdr', '-']])
  expect(agentsLine().runs).toEqual([['progress', 'agents']])
  // Picking the filing agent turns filing on in the same go.
  expect(pickLine('pi', 'ja').runs).toEqual([['progress', 'organizer', '--agent', 'pi', '--lang', 'ja'], ['progress', 'on']])
  expect(switchLine(false).runs).toEqual([['progress', 'off']])
  expect(langLine('zh-Hant').runs).toEqual([['progress', 'lang', 'zh-Hant']])
})

// For a companion older than /api/cli, through machine.exec.
test('commands run the companion from where the README installs it, else from PATH, with every argument quoted', () => {
  const b = `b="$HOME/.local/bin/herdr-anywhere"; [ -x "$b" ] || b=herdr-anywhere; "$b"`
  expect(trackLine(['tree', "P'1"]).line).toBe(`${b} track 'tree' 'P'\\''1' '--herdr' '-'`)
  expect(agentsLine().line).toBe(`${b} progress 'agents'`)
  expect(pickLine('pi', 'ja').line).toBe(`${b} progress 'organizer' '--agent' 'pi' '--lang' 'ja' && "$b" progress 'on'`)
  expect(langLine('zh-Hant').line).toBe(`${b} progress 'lang' 'zh-Hant'`)
})

test('the agents to pick from: the ones that can be picked first', () => {
  const a = parseAgents(JSON.stringify({ agents: [{ id: 'codex', name: 'Codex', cmd: 'codex', usable: false, note: 'needs 0.163 or later', needs: '0.163', version: '0.150.0' },
    { id: 'kimi-code', name: 'Kimi Code', cmd: 'kimi', usable: false, note: 'installed only as a .cmd script', script: true }, { id: 'pi', name: 'Pi', cmd: 'pi', usable: true }], chosen: 'pi' }))
  expect(a).toEqual({ agents: [{ id: 'pi', name: 'Pi', usable: true, note: '', needs: '', version: '', script: false },
    { id: 'codex', name: 'Codex', usable: false, note: 'needs 0.163 or later', needs: '0.163', version: '0.150.0', script: false },
    { id: 'kimi-code', name: 'Kimi Code', usable: false, note: 'installed only as a .cmd script', needs: '', version: '', script: true }], chosen: 'pi' })
  expect(parseAgents('{}')).toEqual({ agents: [], chosen: '' })
})

test('the agent list says which task a pane is on: the open one it was last handed and still works on', async () => {
  const t = await real()
  expect(taskOn(t, 't2')?.id).toBe('T9') // T1 was handed on to the receipt email task
  expect(taskOn(t, 't4')?.id).toBe('T6')
  expect(taskOn(t, 't3')).toBe(undefined) // P2 is done
})

// The computer sends each pane's row; the phone words what is wrong in its language (track.go flags the same cases).
test('what is wrong with a task\'s panes is worded on the phone', () => {
  const task = (rows: Task['pane_rows']): Task => ({ id: 'T1', outcome: 'x', stage: 'doing', group: 'stuck', evidence: [], pane_rows: rows, notes: [], children: [], done: 0, total: 1 })
  expect(flagsOf(task([{ id: 't2', name: 'phone', state: 'live', status: 'blocked', msg: 'approve Bash: git push' }]))).toEqual(['phone asks: approve Bash: git push'])
  expect(flagsOf(task([{ id: 't2', state: 'live', status: 'blocked' }]))).toEqual(['t2 is waiting on you'])
  expect(flagsOf(task([{ id: 't2', name: 'api', state: 'live', status: 'idle', quiet: true, quiet_for_ms: 2 * 3_600_000 }]))).toEqual(['api: no new turn in 2h since handed this'])
  expect(flagsOf(task([{ id: 't3', state: 'missing' }, { id: 't4', state: 'live', status: 'blocked', handed_on: true }]))).toEqual(["Can't find t3"])
  expect(subLine(task([{ id: 't2', name: 'phone', state: 'live', status: 'error' }]))).toBe('phone hit an error')
  // An error says so, however long the pane has been quiet; what the task waits on comes before the agent's words.
  expect(flagsOf(task([{ id: 't2', name: 'api', state: 'live', status: 'error', quiet: true, quiet_for_ms: 3_600_000 }]))).toEqual(['api hit an error'])
  expect(subLine({ ...task([]), waiting: 'push?', said: 'all done' })).toBe('push?')
})

// Small trees of our own, for what the sample scene does not have.
const mk = (id: string, children: Task[] = [], more: Partial<Task> = {}): Task =>
  ({ id, outcome: id, stage: 'doing', group: 'doing', evidence: [], pane_rows: [], notes: [], children, done: 0, total: 1, ...more })
const treeOf = (...tasks: Task[]) => ({ needsYou: [], justDone: [], tasks, closed: [], queue: 0 })

test('a dropped task is not one of its parent\'s; a pane is on the deepest task it was handed', () => {
  expect(items(mk('P', [mk('A', [], { group: 'done' }), mk('B', [], { group: 'dropped' }), mk('C')]))).toEqual([
    { done: true, text: 'A' }, { done: false, text: 'C' }])
  const on = [{ id: 't9' }]
  expect(taskOn(treeOf(mk('P', [mk('Q', [], { pane_rows: on })], { pane_rows: on })), 't9')?.id).toBe('Q')
})

test('rows go at most two steps in, and folding a heading leaves the headings beside it', () => {
  const rows = homeOf(treeOf(mk('G', [mk('A', [mk('B', [mk('C', [mk('D')])])]), mk('E', [mk('E1')])]))).groups[0].rows
  expect(rows.map((r) => `${r.task.id}${r.depth}`).sort()).toEqual(['A0', 'B1', 'C2', 'D2', 'E0', 'E11'])
  expect(visibleRows(rows, new Set(['A'])).map((r) => r.task.id).sort()).toEqual(['A', 'E', 'E1'])
})

// Agents answer in Markdown. The page shows it as paragraphs and list items, without the marks:
// a raw "**" or "- " on the phone reads as noise (seen on a real task page).
test('what an agent said reads as paragraphs and list items, without Markdown marks', async () => {
  const { blocks, plain } = await import('./progress.ts')
  const said = '大发布的稿子还没写完。**t27** 定好了方向：\n\n- **主打的点：** 同一张列表里\n- **按顺序排：**\n  1. 每一行认得出是哪个 agent；\n  2. 用 `Gboard` 回车不再吞字\n\n## 下一步\n见 [计划](https://example.com/plan)。'
  expect(blocks(said)).toEqual([
    { text: '大发布的稿子还没写完。t27 定好了方向：' },
    { mark: '•', text: '主打的点： 同一张列表里' },
    { mark: '•', text: '按顺序排：' },
    { mark: '1.', text: '每一行认得出是哪个 agent；', depth: 1 },
    { mark: '2.', text: '用 Gboard 回车不再吞字', depth: 1 },
    { text: '下一步', head: true },
    { text: '见 计划。' },
  ])
  // On a card, one line of it.
  expect(plain(said)).toBe('大发布的稿子还没写完。t27 定好了方向： • 主打的点： 同一张列表里 • 按顺序排： 1. 每一行认得出是哪个 agent； 2. 用 Gboard 回车不再吞字 下一步 见 计划。')
  expect(blocks('Fixed it. All tests pass.')).toEqual([{ text: 'Fixed it. All tests pass.' }])
  expect(blocks('')).toEqual([])
})

// The home page takes its structure from Things 3: loose tasks first, then each bigger task as a heading with its
// tasks under it (loose ones first, then the ones with tasks of their own as subheadings); what needs the user first in
// each list, queued work after, and what finished today last, where it sits; older finished and dropped work leaves for
// the "done" entry at the bottom.
const node = (id: string, group: string, children: any[] = []) => ({ id, outcome: id, stage: group, group, evidence: [], pane_rows: [], notes: [], children, done: 0, total: 0 })
test('the home page: loose tasks first, then headings with their tasks, ordered by what needs the user, today\'s finished last', async () => {
  const tr = parseTree(JSON.stringify({
    needs_you: [{ ...node('A3', 'waiting'), path: ['A'] }],
    just_done: [{ ...node('A6', 'done'), path: ['A'] }, { ...node('B4', 'done'), path: ['A', 'B'] }, { ...node('G', 'done'), path: [] }],
    tasks: [
      node('L1', 'parked'), node('L2', 'doing'),
      node('A', 'doing', [node('A1', 'parked'), node('A6', 'done'), node('A2', 'doing'), node('A3', 'waiting'), node('A4', 'done'),
        node('B', 'doing', [node('B1', 'parked'), node('B4', 'done'), node('B2', 'review'), node('B3', 'dropped')]), node('A5', 'stuck')]),
      // Nothing open under it but itself: a loose task.
      node('C', 'doing', [node('C1', 'done')]),
      // Waits on the user with nothing open under it: only under "needs you".
      node('D', 'waiting', [node('D1', 'done')]),
      node('E', 'done', [node('E1', 'done')]),
    ],
    closed: [node('F', 'dropped'), node('G', 'done')],
  }))
  tr.needsYou.push({ ...(findTask(tr, 'D') as Task), path: [] })
  const h = homeOf(tr)
  expect(h.loose.map((x) => x.id)).toEqual(['L2', 'C', 'L1'])
  expect(h.after.map((x) => x.id)).toEqual(['G']) // finished today with no goal: after the goals
  expect(h.groups.map((g) => [g.task.id, g.rows.map((r) => `${r.task.id}${r.head ? ':head' : ''}@${r.depth}`)])).toEqual([
    ['A', ['A5@0', 'A2@0', 'A1@0', 'A6@0', 'B:head@0', 'B2@1', 'B1@1', 'B4@1']],
  ])
  expect(h.done.map((x) => x.id)).toEqual(['E', 'F'])
})

test('the sample scene: each goal a heading, the card work a subheading, today\'s finished where it sits', async () => {
  const h = homeOf(await real())
  expect(h.loose).toEqual([])
  expect(h.after.map((x) => x.id)).toEqual(['P2'])
  expect(h.groups.map((g) => [g.task.id, g.rows.map((r) => `${r.task.id}${r.head ? ':head' : ''}@${r.depth}`)])).toEqual([
    ['P1', ['T9@0', 'T5@0', 'T1:head@0', 'T3@1', 'T4@1', 'T2@1']],
    ['P3', ['T10@0', 'T11@0']],
  ])
  expect(h.done).toEqual([])
})

// "Needs you" takes a task out of its goal: with more than one goal on the page, its row names the goal it is under.
test('a task that needs the user names its goal when the page has more than one', async () => {
  const t = await real()
  expect(goalOf(t, t.needsYou[0])).toBe('上线 Acme 商店的新结账流程')
  t.tasks = t.tasks.filter((x) => x.id === 'P1')
  expect(goalOf(t, t.needsYou[0])).toBe('')
})

// A task's panes by name; the number only tells two of the same name apart.
test('a task\'s panes: by name, and none once the task is closed', async () => {
  const t = await real()
  expect(panesOf(findTask(t, 'T1')!).map((p) => [p.label, p.kind, p.addr])).toEqual([['checkout', 'moved', 't2']])
  expect(panesOf(findTask(t, 'T9')!).map((p) => [p.label, p.kind, p.addr])).toEqual([['checkout', 'live', 't2']])
  expect(panesOf(findTask(t, 'P2')!)).toEqual([])
  const two: Task = { ...findTask(t, 'T9')!, pane_rows: [{ id: 't2', name: 'web', found: true, state: 'live' }, { id: 't7', name: 'web', found: true, state: 'live' }, { id: 't8', found: false }, { id: 't6', found: false, state: 'live' }] }
  expect(panesOf(two).map((p) => [p.label, p.kind])).toEqual([['web · t2', 'live'], ['web · t7', 'live'], ['t8', 'missing'], ['t6', 'missing']])
  // A card opens the last pane handed the task that is still there and still on it.
  const rows = [{ id: 'a', found: true, state: 'live' }, { id: 'z', found: true, state: 'live' }, { id: 'b', found: false, state: 'live' }, { id: 'c', found: true, state: 'live', handed_on: true }]
  expect(paneOf({ ...two, pane_rows: rows })?.addr).toBe('z')
})

// A heading the user folded hides what is under it, down to the next row at its own depth or above.
test('a folded heading hides its own rows, and only those', async () => {
  const rows = homeOf(await real()).groups[0].rows
  const ids = (folded: string[]) => visibleRows(rows, new Set(folded)).map((r) => r.task.id)
  expect(ids([])).toEqual(['T9', 'T5', 'T1', 'T3', 'T4', 'T2'])
  expect(ids(['T1'])).toEqual(['T9', 'T5', 'T1'])
  expect(ids(['T9'])).toEqual(['T9', 'T5', 'T1', 'T3', 'T4', 'T2']) // not a heading
})
