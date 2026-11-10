// What to run on the computer so this phone connects again, from how it was paired (the link it keeps).
import { expect, test } from 'nsh/test'
import { restartFor } from './restart.ts'

const link = (url: string, via?: 'tailpocket' | 'demo') => ({ url, token: 't', device: 'd', name: 'n', enc: 'bin' as const, via })

test('through Tailcat, start: it runs serve --tailcat and keeps it running', () => {
  expect(restartFor(link('tailpocket:', 'tailpocket'))).toEqual({ command: 'herdr-anywhere start' })
})

test("on the LAN (a private IPv4 address), start --lan: it listens there again whatever this computer's address is now, also after a restart", () => {
  expect(restartFor(link('http://192.168.1.20:7681'))).toEqual({ command: 'herdr-anywhere start --lan' })
  expect(restartFor(link('http://10.0.0.7:7681'))).toEqual({ command: 'herdr-anywhere start --lan' })
  expect(restartFor(link('http://172.20.1.2:7681'))).toEqual({ command: 'herdr-anywhere start --lan' })
  // --lan takes its port from the first --listen (start_lan_test.go: start --lan --listen 127.0.0.1:9000).
  expect(restartFor(link('http://192.168.1.5:7690'))).toEqual({ command: 'herdr-anywhere start --lan --listen 127.0.0.1:7690' })
})

test('on another address (a Tailscale 100.x, an IPv6 one), start --listen on that address', () => {
  expect(restartFor(link('http://100.101.102.103:7681'))).toEqual({ command: 'herdr-anywhere start --listen 100.101.102.103:7681' })
  expect(restartFor(link('http://172.32.0.1:7681'))).toEqual({ command: 'herdr-anywhere start --listen 172.32.0.1:7681' })
  expect(restartFor(link('http://[fd7a:115c:a1e0::1]:7681'))).toEqual({ command: 'herdr-anywhere start --listen [fd7a:115c:a1e0::1]:7681' })
})

test("on this computer's own loopback (the web page on the computer), start --listen on its port: a plain start takes any free port", () => {
  expect(restartFor(link('http://127.0.0.1:7681'))).toEqual({ command: 'herdr-anywhere start --listen 127.0.0.1:7681' })
  expect(restartFor(link('http://127.0.0.1:7700'))).toEqual({ command: 'herdr-anywhere start --listen 127.0.0.1:7700' })
})

test('a name or a proxy in front (pair --url): how serve listens there is unknown, so the address the phone uses', () => {
  expect(restartFor(link('http://mac.example:7681'))).toEqual({ address: 'mac.example:7681' })
  expect(restartFor(link('https://herdr.example'))).toEqual({ address: 'herdr.example' })
})

test('Try it has no computer', () => {
  expect(restartFor(link('demo:', 'demo'))).toEqual({})
})
