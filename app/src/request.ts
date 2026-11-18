// Observe HTTP status and existing byte metadata only. Never read/clone bodies or serialize anything for logging.
import { diag, diagnosticEndpoint, diagnosticRequest } from './diag.ts'

export async function diagnosticFetch(fetcher: typeof fetch, url: string, init?: RequestInit): Promise<Response> {
  const request = diagnosticRequest(), started = Date.now()
  const verb = init?.method ?? 'GET'
  const fields: Record<string, string> = { request, endpoint: diagnosticEndpoint(url), method: ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD'].includes(verb) ? verb : 'other' }
  if (typeof Blob !== 'undefined' && init?.body instanceof Blob) fields.requestBytes = String(init.body.size)
  diag('http-start', fields)
  try {
    const response = await fetcher(url, init)
    const bytes: Record<string, string> = {}
    // Content-Length is an advertised size, not a measurement of received bytes (and may be absent/compressed).
    try {
      const size = response.headers?.get('content-length')
      if (size && /^(0|[1-9]\d{0,14})$/.test(size)) bytes.responseBytes = size
    } catch {} // A host without compatible headers still returns its original response.
    diag('http-end', { ...fields, ...bytes, elapsed: String(Math.max(0, Date.now() - started)), status: String(response.status), result: response.ok ? 'ok' : 'error' })
    return response
  } catch (error) {
    diag('http-end', { ...fields, elapsed: String(Math.max(0, Date.now() - started)), result: 'error' })
    throw error
  }
}
