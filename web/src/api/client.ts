export class ApiError extends Error {
  code: string
  status: number

  constructor(code: string, status: number, message?: string) {
    super(message || code)
    this.code = code
    this.status = status
  }
}

type ErrorBody = { code?: string; message?: string; error?: { code?: string; message?: string } }
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
    let code = 'mail.internal_error'
    let message = res.statusText || code
    if (json && typeof json === 'object') {
      const body = json as ErrorBody
      if (body.error?.code) code = String(body.error.code)
      else if (body.code) code = String(body.code)
      if (body.error?.message) message = String(body.error.message)
      else if (body.message) message = String(body.message)
      else message = code
    }
    throw new ApiError(code, res.status, message)
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
  return apiPost<{ username: string; impersonated?: boolean }>('/api/auth/login', payload)
}

export const IMPERSONATE_KEY = 'wernanmail.impersonate'

export type ImpersonatePayload = {
  token: string
  imapHost?: string
  imapPort?: number
  smtpHost?: string
  smtpPort?: number
  tls?: boolean
}

export function impersonateLogin(payload: ImpersonatePayload) {
  return apiPost<{ username: string; impersonated: boolean; impersonatedBy?: string }>(
    '/api/auth/impersonate',
    payload,
  )
}

export function fetchMe() {
  return apiGet<{ username: string; impersonated: boolean; impersonatedBy?: string }>('/api/auth/me')
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

export function updateMessageFlags(
  id: string,
  folder: string,
  flags: { add?: string[]; remove?: string[] },
) {
  return apiPatch<{ status: string }>(`/api/messages/${encodeURIComponent(id)}/flags`, {
    folder,
    add: flags.add ?? [],
    remove: flags.remove ?? [],
  })
}

export function trashMessage(id: string, folder: string) {
  return apiPost<{ status: string }>(
    `/api/messages/${encodeURIComponent(id)}/trash?folder=${encodeURIComponent(folder)}`,
  )
}

async function apiPatch<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method: 'PATCH',
    credentials: 'include',
    headers: body != null ? { 'Content-Type': 'application/json' } : undefined,
    body: body != null ? JSON.stringify(body) : undefined,
  })
  return parse<T>(res)
}
