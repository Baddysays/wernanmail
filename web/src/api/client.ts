export class ApiError extends Error {
  code: string
  status: number

  constructor(code: string, status: number) {
    super(code)
    this.code = code
    this.status = status
  }
}

type ErrorBody = { code?: string }
type DataBody<T> = { data: T }

async function parse<T>(res: Response): Promise<T> {
  const text = await res.text()
  let json: unknown = null
  if (text) {
    try {
      json = JSON.parse(text)
    } catch {
      json = null
    }
  }
  if (!res.ok) {
    const code =
      json && typeof json === 'object' && 'code' in json
        ? String((json as ErrorBody).code ?? 'mail.internal_error')
        : 'mail.internal_error'
    throw new ApiError(code, res.status)
  }
  if (json && typeof json === 'object' && 'data' in json) {
    return (json as DataBody<T>).data
  }
  return json as T
}

export async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(path, { credentials: 'include' })
  return parse<T>(res)
}

export async function apiPost<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'include',
    headers: body != null ? { 'Content-Type': 'application/json' } : undefined,
    body: body != null ? JSON.stringify(body) : undefined,
  })
  return parse<T>(res)
}

export type LoginPayload = {
  imapHost: string
  imapPort?: number
  smtpHost: string
  smtpPort?: number
  username: string
  password: string
  tls?: boolean
}

export function login(payload: LoginPayload) {
  return apiPost<{ username: string }>('/api/auth/login', payload)
}

export function logout() {
  return apiPost<{ status: string }>('/api/auth/logout')
}

export function fetchFolders() {
  return apiGet<import('./types').Folder[]>('/api/folders')
}

export function fetchMessages(folder: string, limit = 50) {
  const q = new URLSearchParams({ folder, limit: String(limit) })
  return apiGet<import('./types').MessageSummary[]>(`/api/messages?${q}`)
}

export function fetchMessage(id: string, folder: string) {
  const q = new URLSearchParams({ folder })
  return apiGet<import('./types').MessageDetail>(
    `/api/messages/${encodeURIComponent(id)}?${q}`,
  )
}

export type SendPayload = {
  to: string[]
  cc?: string[]
  bcc?: string[]
  subject: string
  text: string
  html?: string
}

export function sendMessage(payload: SendPayload) {
  return apiPost<{ status: string }>('/api/messages/send', payload)
}
