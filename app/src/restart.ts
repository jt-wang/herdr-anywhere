// What to run on the computer so this phone connects again, from how it was paired: the link it keeps. The
// banner shows it when the computer refuses (nothing listens) or a tunnel is left unanswered.
import type { Link } from './link.ts'

/** A command to run there, or, when how serve listens there can't be known (a name, a proxy), the address to check. */
export type Restart = { command?: string; address?: string }

const DEFAULT_PORT = '7681'

/** A private IPv4 address: what `serve --lan` listens on (companion/internal/lan: 10/8, 172.16/12, 192.168/16). */
function lanIPv4(host: string): boolean {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host)
  if (!m) return false
  const [a, b] = [Number(m[1]), Number(m[2])]
  return a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168)
}

const ipLiteral = (host: string) => /^\d{1,3}(\.\d{1,3}){3}$/.test(host) || host.startsWith('[')

export function restartFor(l: Link): Restart {
  if (l.via === 'tailpocket') return { command: 'herdr-anywhere start' }
  if (l.via === 'demo') return {}
  const m = /^(https?):\/\/(\[[^\]]+\]|[^/:?#]+)(?::(\d+))?/.exec(l.url)
  if (!m) return {}
  const [, scheme, host, given] = m
  const port = given ?? (scheme === 'https' ? '443' : '80')
  // https, or a name: something in front of serve (pair --url), or a name only that network resolves.
  if (scheme !== 'http' || !ipLiteral(host)) return { address: given ? `${host}:${given}` : host }
  // start keeps serve running, also after a restart (start_lan_test.go): --lan on serve's own port, 7681, or on the
  // first --listen's; --listen for any other address, and for this computer's loopback, where a plain start takes any
  // free port.
  if (lanIPv4(host)) return { command: port === DEFAULT_PORT ? 'herdr-anywhere start --lan' : `herdr-anywhere start --lan --listen 127.0.0.1:${port}` }
  return { command: `herdr-anywhere start --listen ${host}:${port}` }
}
