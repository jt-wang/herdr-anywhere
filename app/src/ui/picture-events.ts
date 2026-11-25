/** Bind native decode callbacks to the image request that created their view. */
export function pictureEvents<T>(image: T, owner: AbortController | undefined,
  current: () => { image: T | null; owner: AbortController | undefined },
  loaded: (size: { width: number; height: number }) => void, failed: () => void) {
  const valid = () => { const now = current(); return now.image === image && now.owner === owner && !owner?.signal.aborted }
  return {
    onLoad: (size: { width: number; height: number }) => { if (valid()) loaded(size) },
    onError: () => { if (valid()) failed() },
  }
}
