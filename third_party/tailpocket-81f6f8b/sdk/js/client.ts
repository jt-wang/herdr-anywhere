// One TypeScript API over the native bridge (TailPocketBridge on iOS and Android), for any
// JavaScript that runs inside a native app: NSH apps (`host`), React Native (the native module).
// The native side does the work; this file only names the calls and shapes the answers.

/** How JavaScript reaches the native bridge. `timeoutMs` 0 waits as long as the native side takes. */
export interface TailPocketTransport {
  call(name: string, args?: unknown, timeoutMs?: number): Promise<unknown>;
  on(event: string, listener: (data: unknown) => void): (() => void) | { remove(): void } | void;
}

export interface TailPocketInvite { v: number; address: string; port: number; key: string; proof: string; expiresAt: number; name?: string }
/** One way to reach a computer (v0.3). `m: "opaque"`: the relay carries the app's own encryption. */
export interface TailPocketRoute { t: "tailcat" | "relay" | "lan" | string; a?: string[]; p?: number; u?: string; r?: string; m?: "opaque"; inv?: TailPocketInvite }
/** A v2 invite (`tailpocket2:`): every route and the computer's end-to-end key. */
export interface TailPocketInvite2 { v: 2; name?: string; exp: number; pk?: string; psk?: string; via: TailPocketRoute[] }
/** Store like a password: it holds the phone's private key. `pk` and `via` (v0.3) arrive with `sync()` for links paired before v0.3;
 *  a link paired over a relay may have no `address`. */
export interface TailPocketHostLink { address?: string; port?: number; name: string; payload?: string | null; key: string; pk?: string; via?: TailPocketRoute[] }
/** `direct` or `relay` over Tailcat, `relay` over your tailpocket-relay (`relay` is its host name), or `lan`. */
export interface TailPocketPath { route: "direct" | "relay" | "lan" | string; relay: string }
export interface TailPocketConnection {
  /** `http://127.0.0.1:<port>`: requests to it reach the computer's forwarded port. */
  baseUrl: string;
  port: number;
  /** Registers this phone's end-to-end key over Tailcat (links from before v0.3) and returns the updated link; a link from the store is saved. */
  sync(timeoutMs?: number): Promise<TailPocketHostLink>;
  /** How the connection reaches the computer now; rejects with `revoked` once the computer removed this phone. */
  path(timeoutMs?: number): Promise<TailPocketPath>;
  /** The link with the computer's latest routes. */
  link(): Promise<TailPocketHostLink>;
  close(): Promise<void>;
}
export interface TailPocketNearbyHost { name: string }
export interface TailPocketPendingPairing {
  /** Show this; the computer's owner checks it matches before approving. */
  code: string;
  /** Waits for the approval; pass the invite it returns to `pair`. */
  finish(timeoutMs?: number): Promise<TailPocketInvite>;
  cancel(): Promise<void>;
}

/** Codes: invalid-invite, expired, unreachable, revoked (delete the link), mismatch, handshake-closed, denied, not-paired,
 *  unsupported, cancelled, unknown-handle, … Only `revoked` means the computer removed this phone. */
export class TailPocketError extends Error {
  code: string;
  constructor(code: string, message?: string) {
    super(message ?? code);
    this.name = "TailPocketError";
    this.code = code;
  }
}

const PAIR_TIMEOUT_MS = 90_000;
const LOCAL_TIMEOUT_MS = 300_000;
// The native side times out first; the transport only needs to outlast it.
const SLACK_MS = 15_000;

export function createTailPocket(transport: TailPocketTransport) {
  const call = async <T>(name: string, args?: unknown, timeoutMs?: number): Promise<T> => {
    try {
      return (await transport.call(name, args, timeoutMs)) as T;
    } catch (e: any) {
      const code = typeof e?.code === "string" && e.code ? e.code : String(e?.message ?? e);
      throw new TailPocketError(code, e?.message);
    }
  };
  const unsubscribe = (s: ReturnType<TailPocketTransport["on"]>) => {
    if (typeof s === "function") s();
    else if (s && typeof s.remove === "function") s.remove();
  };

  return {
    newKey: () => call<string>("tailPocket.newKey"),
    publicKey: (key: string) => call<string | null>("tailPocket.publicKey", { key }),
    inviteText: (text: string, prefix?: string) => call<string | null>("tailPocket.inviteText", prefix ? { text, prefix } : { text }),
    decodeInvite: (text: string, prefix?: string) => call<TailPocketInvite>("tailPocket.decodeInvite", prefix ? { text, prefix } : { text }),
    /** A v2 invite, or a v1 one as a single tailcat route. */
    decodeInvite2: (text: string, prefix?: string) => call<TailPocketInvite2>("tailPocket.decodeInvite2", prefix ? { text, prefix } : { text }),

    /** Shows the camera until a QR code with an invite is seen. */
    /** The camera, until a QR with an invite: v1 or v2 with the default prefixes, else `prefix` (and `prefix2`, the app's v2 prefix). */
    scan: (prefix?: string, prefix2?: string) => call<string>("tailPocket.scan", { ...(prefix ? { prefix } : {}), ...(prefix2 ? { prefix2 } : {}) }, 0),

    /** Pairs with the computer that made `invite` (the QR text, or a decoded invite) over whichever of its routes works. */
    pair: (invite: string | TailPocketInvite | TailPocketInvite2, options: { device: string; save?: boolean; slot?: string; phoneKey?: string; timeoutMs?: number; derpMapUrl?: string }) =>
      call<TailPocketHostLink>("tailPocket.pair", { invite, ...options }, (options.timeoutMs ?? PAIR_TIMEOUT_MS) + SLACK_MS),

    /** Opens the tunnel to a saved (or given) computer. */
    connect: async (options: { link?: TailPocketHostLink; slot?: string; derpMapUrl?: string } = {}): Promise<TailPocketConnection> => {
      const c = await call<{ handle: number; port: number; baseUrl: string }>("tailPocket.connect", options);
      const handle = c.handle;
      return {
        baseUrl: c.baseUrl, port: c.port,
        sync: (timeoutMs?: number) => call<TailPocketHostLink>("tailPocket.sync", timeoutMs ? { handle, timeoutMs } : { handle }, (timeoutMs ?? 30_000) + SLACK_MS),
        path: (timeoutMs?: number) => call<TailPocketPath>("tailPocket.path", timeoutMs ? { handle, timeoutMs } : { handle }, (timeoutMs ?? 5_000) + SLACK_MS),
        link: () => call<TailPocketHostLink>("tailPocket.link", { handle }),
        close: () => call<void>("tailPocket.disconnect", { handle }),
      };
    },

    /** Lists computers advertising `service` on this Wi-Fi; resolves to a stop function. */
    nearby: async (service: string, onHosts: (hosts: TailPocketNearbyHost[]) => void): Promise<() => Promise<void>> => {
      const sub = transport.on("tailPocket.nearby", (data) => onHosts(((data as { hosts?: TailPocketNearbyHost[] })?.hosts) ?? []));
      try {
        await call<void>("tailPocket.nearby.start", { service });
      } catch (e) {
        unsubscribe(sub);
        throw e;
      }
      return async () => {
        unsubscribe(sub);
        await call<void>("tailPocket.nearby.stop");
      };
    },

    /** Starts pairing with a nearby computer (by name from `nearby`, or by address): show `code`,
     *  then `pair(await finish(), …)`. */
    pairNearby: async (options: { name?: string; address?: string; device?: string; label?: string }): Promise<TailPocketPendingPairing> => {
      const { op, code } = await call<{ op: number; code: string }>("tailPocket.local.begin", options);
      return {
        code,
        finish: (timeoutMs?: number) =>
          call<TailPocketInvite>("tailPocket.local.finish", timeoutMs ? { op, timeoutMs } : { op }, (timeoutMs ?? LOCAL_TIMEOUT_MS) + SLACK_MS),
        cancel: () => call<void>("tailPocket.local.cancel", { op }),
      };
    },

    /** The native store (Keychain / Android Keystore): the phone key and saved computers by slot. */
    store: {
      phoneKey: () => call<string>("tailPocket.store.phoneKey"),
      saveLink: (link: TailPocketHostLink, slot?: string) => call<void>("tailPocket.store.saveLink", slot ? { link, slot } : { link }),
      link: (slot?: string) => call<TailPocketHostLink | null>("tailPocket.store.link", slot ? { slot } : {}),
      deleteLink: (slot?: string) => call<void>("tailPocket.store.deleteLink", slot ? { slot } : {}),
    },
  };
}

export type TailPocketClient = ReturnType<typeof createTailPocket>;
