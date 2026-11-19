// What to run on the computer so this phone connects again, from how it was paired: the link it keeps. The
// banner shows it when the computer refuses (nothing listens) or a tunnel is left unanswered.
import type { Link } from './link.ts'

/** A command to run there, or, when how serve listens there can't be known (a name, a proxy), the address to check.
 *  `pair`: the command shows a new QR code, which Pair again scans. */
export type Restart = { command?: string; address?: string; pair?: boolean }

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
  // The LAN: from TailPocket v0.3 `start --lan` opens only the encrypted local route, which a phone paired by a `#pair=`
  // link can't use, and the unencrypted port closes; a new QR code brings this phone back. start keeps serve running,
  // also after a restart: --listen for any other address, and for this computer's loopback, where a plain start takes
  // any free port.
  if (lanIPv4(host)) return { command: 'herdr-anywhere pair', pair: true }
  return { command: `herdr-anywhere start --listen ${host}:${port}` }
}
