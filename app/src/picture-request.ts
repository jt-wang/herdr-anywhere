/** Bound connection setup, headers and body together; abort also settles hosts that ignore AbortSignal. */
export async function pictureRequest<T>(read: (signal: AbortSignal) => Promise<T>, parent?: AbortSignal, timeout = 15_000): Promise<T> {
  const controller = new AbortController()
  let rejectAbort!: (reason: Error) => void
  const cancelled = new Promise<never>((_, reject) => { rejectAbort = reject })
  const abort = () => { controller.abort(); rejectAbort(new Error('picture-request-cancelled')) }
  const timer = setTimeout(abort, timeout)
  parent?.addEventListener('abort', abort, { once: true })
  if (parent?.aborted) abort()
  try {
    return await Promise.race([Promise.resolve().then(() => {
      if (controller.signal.aborted) throw new Error('picture-request-cancelled')
      return read(controller.signal)
    }), cancelled])
  } finally {
    clearTimeout(timer)
    parent?.removeEventListener('abort', abort)
  }
}
