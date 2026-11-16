import type { ChatTurn } from '../src/model/chat.ts'

// Synthetic conversation with many commands and interleaved progress messages.
export const hierarchyTurn: ChatTurn = {
  id: 'review', from: 'you', said: 'Check the catalog filters and keep the current colors.', end: 'open',
  reply: 'The filters now keep your selection. I am checking the empty results view.',
  steps: [
    { id: 'progress-start', kind: 'note', title: 'I will check how the filters update the catalog.' },
    ...Array.from({ length: 12 }, (_, i) => ({ id: `command-${i}`, kind: 'run', title: `sed -n '${i * 20 + 1},${i * 20 + 20}p' catalog/filter.ts`, ...(i === 7 ? { failed: true } : {}) })),
    { id: 'progress-latest', kind: 'note', title: 'The selection is preserved. I am checking the empty state.' },
    { id: 'check', kind: 'run', title: 'make test-catalog' },
  ],
}
