import { expect, test } from 'nsh/test'
import { connectionEvidence, CONNECTION_EVIDENCE_KEY } from './connection-evidence.ts'

test('a new test-app launch preserves the previous launch trace', async () => {
  const disk = new Map<string, string>()
  const launch = (n: number) => {
    const options = {
      storageKey: `${CONNECTION_EVIDENCE_KEY}-${n}`,
      now: () => 100 * n,
      send: () => {},
      write: async (key: string, value: string) => { disk.set(key, value) },
    }
    return connectionEvidence(options)
  }
  const first = launch(1)
  first.record('connection-lost', { attempt: '1', wanted: 'true', retrying: 'false' })
  await first.flush()
  const second = launch(2)
  second.record('connection-connect', { attempt: '1', step: 'ready' })
  await second.flush()
  const persisted = [...disk.values()].flatMap((value) => JSON.parse(value))
  expect(persisted.map((row: any) => [row.time, row.what])).toEqual([
    [100, 'connection-lost'], [200, 'connection-connect'],
  ])
})
