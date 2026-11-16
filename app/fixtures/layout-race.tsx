// A real Root screen over the protocol fake, with one competing document write and a held read response.
import { signal, Storage, Text, View } from 'nsh'
import type { Store } from '../src/store.ts'
import { panes, terminalOf } from '../src/model/layout.ts'
import { FakeCore, settle } from '../src/testing/fakecore.ts'

export class LayoutRace {
  private store?: Store
  private terminal = ''
  private originalPane = ''
  private conflicts = 0
  private input: string[] = []
  private version = 0
  private phase = signal('ready')

  constructor(private core: FakeCore) {
    const doc = { spaces: [] as any[] }
    core.addSpace(doc, 'splayout', '/work/orchard', 'orchard', [
      { id: 'tblayout', name: 'original', argv: ['sh'], screen: 'original terminal' },
    ])
    core.docs.set('', { value: doc, version: 1 })
  }

  attach(store: Store) {
    this.store = store
    const call = store.call.bind(store)
    store.call = async (method, params, opts) => {
      try {
        const result = await call(method, params, opts)
        if (method === 'terminal.run') this.terminal = result.address
        return result
      } catch (error: any) {
        if (method === 'doc.set' && error.kind === 'conflict' && this.phase() === 'armed') {
          this.conflicts++
          this.originalPane = store.model.paneOf(this.terminal) ?? ''
          await this.record('blocked')
          this.phase.set('blocked')
        }
        throw error
      }
    }
    const onSend = this.core.onSend
    this.core.onSend = (address, text, force) => {
      onSend?.(address, text, force)
      this.input = [address, text]
      // An acknowledgement from the fake terminal, only after the UI sends to the new terminal.
      if (address === this.terminal && text === 'layout input ok') {
        this.core.screens.set(address, text)
        this.core.output(address, `\x1b[H\x1b[2J${text}`)
      }
    }
  }

  private arm() {
    if (this.phase() !== 'ready' || this.store?.conn.get() !== 'ready') throw new Error('layout race is not ready')
    const d = this.core.docs.get('')!
    this.version = d.version
    const value = JSON.parse(JSON.stringify(d.value))
    value.spaces[0].name = 'grove'
    this.core.docs.set('', { value, version: d.version + 1 })
    this.core.holding.add('doc.get')
    this.phase.set('armed')
  }

  private async release() {
    if (this.phase() !== 'blocked' || this.conflicts !== 1 || !this.originalPane) throw new Error('no held layout conflict')
    this.core.holding.delete('doc.get')
    this.core.release()
    await settle(30)
    await this.record('released')
    this.phase.set('released')
  }

  private async record(stage: string) {
    const store = this.store!
    const doc = this.core.doc()
    const evidence = {
      stage, terminal: this.terminal, originalPane: this.originalPane,
      pane: store.model.paneOf(this.terminal) ?? null, focused: store.model.focusedPane() ?? null,
      rename: store.model.space('splayout')?.name ?? null,
      documentRename: doc.spaces[0].name,
      documentPanes: doc.spaces.flatMap((s: any) => s.tabs).flatMap((t: any) => panes(t.root))
        .filter((p: any) => terminalOf(p) === this.terminal).map((p: any) => p.id),
      runs: this.core.calls.filter(c => c.method === 'terminal.run').length,
      conflicts: this.conflicts, beforeVersion: this.version, version: this.core.docs.get('')!.version,
      input: this.input, screen: this.core.screens.get(this.terminal) ?? '',
    }
    await Storage.setItem('layout-evidence', JSON.stringify(evidence))
    console.log(`layout-race-evidence ${JSON.stringify(evidence)}`)
  }

  view(root: any) {
    const action = (fn: () => void | Promise<void>) => () => {
      Promise.resolve().then(fn).catch(async e => {
        await Storage.setItem('layout-error', String(e))
        console.log('layout-race-error ' + String(e))
        this.phase.set('error')
      })
    }
    return <View class="flex-1">
      {root}
      <View class="flex-row gap-3 px-2 py-2 bg-background">
        <View role="button" testID="layout-arm" onPress={action(() => this.arm())}><Text>Arm</Text></View>
        <View role="button" testID="layout-release" onPress={action(() => this.release())}><Text>Release</Text></View>
        <View role="button" testID="layout-snapshot" onPress={action(() => this.record(this.phase() === 'blocked' ? 'visible' : 'input'))}><Text>Snapshot</Text></View>
        <Text testID={`layout-${this.phase()}`}>{this.phase()}</Text>
      </View>
    </View>
  }
}
