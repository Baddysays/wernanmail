export function authHeader(user, pass) {
  // UTF-8 safe Basic auth (btoa alone breaks on non-Latin1).
  const raw = `${user}:${pass}`
  const bytes = new TextEncoder().encode(raw)
  let bin = ''
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i])
  return 'Basic ' + btoa(bin)
}

export async function api(path, { token, user, pass, method = 'GET', body } = {}) {
  const headers = {
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
  const json = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(json?.error?.message || json?.error?.code || res.statusText)
  }
  return json.data
}

export function domainName(d) {
  return d?.name || d?.Name || ''
}

export function domainId(d) {
  return d?.id ?? d?.ID
}

export function dnsRecordsFor(domain) {
  const name = domainName(domain) || 'example.com'
  const selector = domain?.dkimSelector || domain?.DKIMSelector || 'wernan'
  const dkim = domain?.dkimPublic || domain?.DKIMPublic || ''
  return [
    {
      id: 'spf',
      label: 'SPF',
      host: '@',
      type: 'TXT',
      value: `v=spf1 mx a:mail.${name} -all`,
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
      value: `v=DMARC1; p=none; rua=mailto:postmaster@${name}`,
    },
  ]
}
