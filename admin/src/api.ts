import type { AdminCreds, DnsRecord, Domain } from './types'

export function authHeader(user: string, pass: string): string {
  // UTF-8 safe Basic auth (btoa alone breaks on non-Latin1).
  const raw = `${user}:${pass}`
  const bytes = new TextEncoder().encode(raw)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]!)
  return 'Basic ' + btoa(bin)
}

export type ApiOptions = {
  token?: string
  user?: string
  pass?: string
  method?: string
  body?: unknown
}

type ApiErrorBody = {
  error?: { message?: string; code?: string }
}

export class ApiError extends Error {
  status: number
  code?: string
  constructor(message: string, status: number, code?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

export function isUnauthorized(err: unknown): boolean {
  return err instanceof ApiError && err.status === 401
}

export async function api<T = unknown>(path: string, opts: ApiOptions | AdminCreds = {}): Promise<T> {
  const { token, user, pass, method = 'GET', body } = opts as ApiOptions & AdminCreds
  const headers: Record<string, string> = {
    ...(body ? { 'Content-Type': 'application/json' } : {}),
  }
  if (token) {
    headers.Authorization = `Bearer ${token}`
  } else if (user != null && pass != null) {
    headers.Authorization = authHeader(user, pass)
  }
  const res = await fetch(path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  })
  const json = (await res.json().catch(() => ({}))) as ApiErrorBody & { data?: T }
  if (!res.ok) {
    throw new ApiError(json?.error?.message || json?.error?.code || res.statusText, res.status, json?.error?.code)
  }
  return json.data as T
}

export function asList<T>(v: T[] | null | undefined): T[] {
  return Array.isArray(v) ? v : []
}

export function domainName(d: Domain | null | undefined): string {
  return d?.name || d?.Name || ''
}

export function domainId(d: Domain | null | undefined): string | undefined {
  const id = d?.id ?? d?.ID
  return id == null ? undefined : String(id)
}

export function dnsRecordsFor(domain: Domain | null | undefined, publicIP?: string): DnsRecord[] {
  const name = domainName(domain) || 'example.com'
  const selector = domain?.dkimSelector || domain?.DKIMSelector || 'wernan'
  const dkim = domain?.dkimPublic || domain?.DKIMPublic || ''
  const mailHost = `mail.${name}`
  const ip = (publicIP || '').trim() || '62.109.27.220'
  return [
    {
      id: 'mx',
      label: 'MX',
      host: '@',
      type: 'MX',
      value: `10 ${mailHost}.`,
    },
    {
      id: 'spf',
      label: 'SPF',
      host: '@',
      type: 'TXT',
      value: `v=spf1 a mx a:${mailHost} ip4:${ip} -all`,
    },
    {
      id: 'dkim',
      label: 'DKIM',
      host: `${selector}._domainkey`,
      type: 'TXT',
      value: dkim || '(generate DKIM on the domain first)',
      ready: Boolean(dkim),
    },
    {
      id: 'dmarc',
      label: 'DMARC',
      host: '_dmarc',
      type: 'TXT',
      value: `v=DMARC1; p=none; rua=mailto:postmaster@${name}; adkim=r; aspf=r`,
    },
    {
      id: 'mta-sts',
      label: 'MTA-STS',
      host: '_mta-sts',
      type: 'TXT',
      value: `v=STSv1; id=${new Date().toISOString().slice(0, 10).replace(/-/g, '')}`,
    },
    {
      id: 'tls-rpt',
      label: 'TLS-RPT',
      host: '_smtp._tls',
      type: 'TXT',
      value: `v=TLSRPTv1; rua=mailto:postmaster@${name}`,
    },
    {
      id: 'bimi',
      label: 'BIMI',
      host: 'default._bimi',
      type: 'TXT',
      value: `v=BIMI1; l=https://${name}/bimi.svg`,
    },
    {
      id: 'ptr',
      label: 'PTR (reverse DNS)',
      host: ip,
      type: 'PTR',
      value: `${mailHost}.  (set at VPS panel; must match MAIL_EHLO)`,
    },
  ]
}