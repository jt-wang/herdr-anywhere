// The page's host (web-host.js), for its tests.
export function createHost(player: { hostEvent(name: string, data: unknown): void }): { call(name: string, args?: unknown): any }
export function setupPathStyles(): { textContent: string }
