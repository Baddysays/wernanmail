import { useCallback, useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { LOCALES, setAdminLang } from './i18n'
import { api, dnsRecordsFor, domainId, domainName } from './api'
import { Select } from './Select'
import type {
  AdminCreds,
  Alias,
  AuditEntry,
  Dashboard,
  DnsCheck,
  DnsStatus,
  Domain,
  HostStats,
  Mailbox,
  NavTab,
  OpsStatus,
  QuarantineItem,
  QueueJob,
  SettingGroup,
  SparkSample,
} from './types'

const NAV: NavTab[] = ['overview', 'domains', 'mailboxes', 'queue', 'quarantine', 'settings', 'backup', 'audit']

const SETTING_GROUPS: SettingGroup[] = [
  {
    id: 'mail',
    fields: [
      { key: 'mail.max_message_bytes', type: 'number' },
      { key: 'mail.default_quota_bytes', type: 'number' },
      { key: 'mail.retention_days', type: 'number' },
      { key: 'mail.bounce_enabled', type: 'bool' },
      { key: 'mail.body_template_plain', type: 'textarea' },
      { key: 'mail.body_template_html', type: 'textarea' },
      { key: 'mail.footer_plain', type: 'textarea' },
      { key: 'mail.footer_html', type: 'textarea' },
      { key: 'mail.footer_skip_replies', type: 'bool' },
      { key: 'mail.require_tls_outbound', type: 'bool' },
    ],
  },
  {
    id: 'delivery',
    fields: [
      { key: 'mail.relay_host', type: 'text' },
      { key: 'mail.greylist_seconds', type: 'number' },
    ],
  },
  {
    id: 'rates',
    fields: [
      { key: 'mail.rate_submit_per_min', type: 'number' },
      { key: 'mail.rate_smtp_conn_per_min', type: 'number' },
      { key: 'mail.rate_send_per_hour', type: 'number' },
      { key: 'mail.rate_auth_fail_per_min', type: 'number' },
    ],
  },
  {
    id: 'antispam',
    fields: [
      { key: 'antispam.flag_at', type: 'number' },
      { key: 'antispam.quarantine_at', type: 'number' },
      { key: 'antispam.reject_at', type: 'number' },
      { key: 'antispam.rbls', type: 'text' },
      { key: 'antispam.reject_message', type: 'text' },
    ],
  },
  {
    id: 'quarantine',
    fields: [
      { key: 'antivirus.enabled', type: 'bool' },
      { key: 'quarantine.retention_days', type: 'number' },
    ],
  },
  {
    id: 'security',
    fields: [
      { key: 'security.password_min_length', type: 'number' },
      { key: 'security.password_require_digit', type: 'bool' },
      { key: 'security.password_require_upper', type: 'bool' },
      { key: 'admin.superuser_enabled', type: 'bool' },
      { key: 'admin.webmail_url', type: 'text' },
    ],
  },
]

const SPARK_KEY = 'wm_queue_spark'
const SPARK_MAX = 24

function loadSpark(): SparkSample[] {
  try {
    const raw: unknown = JSON.parse(sessionStorage.getItem(SPARK_KEY) || '[]')
    return Array.isArray(raw) ? raw.slice(-SPARK_MAX) : []
  } catch {
    return []
  }
}

function pushSpark(n: number): SparkSample[] {
  const next = [...loadSpark(), { t: Date.now(), n: Number(n) || 0 }].slice(-SPARK_MAX)
  sessionStorage.setItem(SPARK_KEY, JSON.stringify(next))
  return next
}

function formatBytes(n: unknown): string {
  const v = Number(n) || 0
  if (v >= 1 << 30) return `${(v / (1 << 30)).toFixed(1)} GB`
  if (v >= 1 << 20) return `${Math.round(v / (1 << 20))} MB`
  if (v >= 1 << 10) return `${Math.round(v / (1 << 10))} KB`
  return `${v} B`
}

function bytesToMbInput(bytes: number | undefined): string {
  const v = Number(bytes) || 0
  if (!v) return ''
  return String(Math.round(v / (1 << 20)))
}

function mbInputToBytes(raw: string): number {
  const s = String(raw ?? '').trim()
  if (s === '') return 0
  const n = Number(s)
  if (!Number.isFinite(n) || n < 0) throw new Error('invalid quota')
  return Math.round(n * (1 << 20))
}

function Sparkline({ samples }: { samples: SparkSample[] }) {
  const vals = samples.length ? samples.map((s) => s.n) : [0, 0]
  const max = Math.max(1, ...vals)
  const w = 280
  const h = 72
  const pts = vals.map((v, i) => {
    const x = (i / Math.max(1, vals.length - 1)) * w
    const y = h - 8 - (v / max) * (h - 16)
    return [x, y]
  })
  const line = pts.map((p, i) => `${i ? 'L' : 'M'}${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(' ')
  const area = `${line} L${w},${h} L0,${h} Z`
  return (
    <svg className="spark" viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" aria-hidden>
      <path className="fill" d={area} />
      <path className="line" d={line} />
    </svg>
  )
}

function LangSelect() {
  const { i18n, t } = useTranslation()
  const code = i18n.language?.slice(0, 2) || 'en'
  return (
    <Select
      className="lang-select"
      aria-label={t('lang.label')}
      value={code}
      onChange={(v) => setAdminLang(v)}
      options={LOCALES.map((l) => ({ value: l.code, label: l.label }))}
    />
  )
}

function Meter({ pct }: { pct: number }) {
  const p = Math.max(0, Math.min(100, Number(pct) || 0))
  const cls = p >= 85 ? 'hot' : p >= 70 ? 'warn' : ''
  return (
    <div className={`meter ${cls}`}>
      <span style={{ width: `${p}%` }} />
    </div>
  )
}

function ResourcesPanel({ host }: { host: HostStats | null }) {
  const { t } = useTranslation()
  if (!host) {
    return (
      <section className="resources panel">
        <div className="resources-head">
          <h3>{t('resources.title')}</h3>
        </div>
        <p className="muted">{t('resources.loading')}</p>
      </section>
    )
  }
  const mailRss = host.mailRssBytes ?? 0
  const hostMem = host.mem?.totalBytes || 0
  const ramShare = hostMem > 0 ? (mailRss / hostMem) * 100 : 0
  const dataBytes = host.dataBytes ?? 0
  const binBytes = host.binBytes ?? 0
  const projectDisk = dataBytes + binBytes
  const diskFree = host.disk?.freeBytes ?? 0
  const diskShare =
    projectDisk > 0 && diskFree + projectDisk > 0
      ? (projectDisk / (diskFree + projectDisk)) * 100
      : 0
  const cpu = host.mailCpuPercent ?? 0
  const procs = host.processes || []
  return (
    <section className="resources">
      <div className="resources-head">
        <h3>{t('resources.title')}</h3>
        <span className="muted">{t('resources.hint')}</span>
      </div>
      <div className="resource-grid">
        <div className="resource-card">
          <p className="label">{t('resources.ram')}</p>
          <p className="value">{formatBytes(mailRss)}</p>
          <Meter pct={ramShare} />
          <p className="sub">
            {t('resources.ramShare', { pct: ramShare.toFixed(1), host: formatBytes(hostMem) })}
          </p>
        </div>
        <div className="resource-card">
          <p className="label">{t('resources.disk')}</p>
          <p className="value">{formatBytes(projectDisk)}</p>
          <Meter pct={Math.min(100, diskShare)} />
          <p className="sub">
            {t('resources.diskBreakdown', {
              data: formatBytes(dataBytes),
              bin: formatBytes(binBytes),
            })}
          </p>
        </div>
        <div className="resource-card">
          <p className="label">{t('resources.cpu')}</p>
          <p className="value">
            {cpu.toFixed(1)}
            <span className="value-unit">%</span>
          </p>
          <Meter pct={Math.min(100, cpu)} />
          <p className="sub">{t('resources.cpuHint', { n: procs.length })}</p>
        </div>
      </div>
      <div className="panel proc-panel">
        <p className="label">{t('resources.services')}</p>
        <div className="proc-list">
          {procs.map((p) => (
            <span className="proc-chip" key={`${p.name}-${p.pid}`}>
              <strong>{p.name}</strong>
              <span>{formatBytes(p.rssBytes)}</span>
              {typeof p.cpuPercent === 'number' ? <span>{p.cpuPercent.toFixed(1)}%</span> : null}
            </span>
          ))}
          {!procs.length ? <span className="muted">{t('resources.noProcs')}</span> : null}
        </div>
      </div>
    </section>
  )
}

function CopyBtn({ text }: { text: string }) {
  const { t } = useTranslation()
  const [done, setDone] = useState(false)
  return (
    <button
      type="button"
      className="ghost"
      disabled={!text || text.startsWith('(')}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setDone(true)
          setTimeout(() => setDone(false), 1200)
        } catch {
          /* ignore */
        }
      }}
    >
      {done ? t('actions.copied') : t('actions.copy')}
    </button>
  )
}

function DNSDrawer({
  open,
  onClose,
  domain,
}: {
  open: boolean
  onClose: () => void
  domain: Domain | null
}) {
  const { t } = useTranslation()
  if (!open) return null
  const records = dnsRecordsFor(domain)
  const name = domainName(domain) || '—'
  return (
    <>
      <div className="drawer-backdrop" onClick={onClose} aria-hidden />
      <aside className="drawer" role="dialog" aria-label={t('dns.title')}>
        <header>
          <div>
            <h3>{t('dns.title')}</h3>
            <p className="muted" style={{ margin: '0.35rem 0 0' }}>
              {t('dns.body', { name })}
            </p>
          </div>
          <button type="button" className="ghost" onClick={onClose}>
            {t('actions.close')}
          </button>
        </header>
        {records.map((r) => (
          <div className="dns-block" key={r.id}>
            <div className="lab">
              {r.label}
              {r.id === 'dkim' && !r.ready ? ` · ${t('dns.genFirst')}` : ''}
            </div>
            <div className="dns-meta">
              <div>
                {t('dns.host')} <code>{r.host}</code>
              </div>
              <div>
                {t('dns.type')} <code>{r.type}</code>
              </div>
            </div>
            <pre className="dns-value">{r.value}</pre>
            <CopyBtn text={r.ready === false ? '' : r.value} />
          </div>
        ))}
        <p className="foot-note">{t('dns.footer', { name })}</p>
      </aside>
    </>
  )
}

type DnsChipLabels = {
  checking: string
  missing: string
  ok: string
  warn?: string
}

function dnsChip(check: DnsCheck | undefined, labels: DnsChipLabels) {
  if (!check) return { state: 'warn', text: labels.checking }
  switch (check.state) {
    case 'ok':
      return { state: 'ok', text: labels.ok }
    case 'warn':
      return { state: 'warn', text: labels.warn || check.detail || labels.missing }
    case 'missing':
      return { state: 'bad', text: labels.missing }
    default:
      return { state: 'warn', text: check.detail || labels.checking }
  }
}

function HealthStrip({
  dash,
  dns,
  updatedAt,
}: {
  dash: Dashboard | null
  dns: DnsStatus | null
  updatedAt: number | null
}) {
  const { t } = useTranslation()
  const queueN = dash?.queuePending ?? 0
  const dead = dash?.queueDead ?? 0
  const tlsOk = typeof location !== 'undefined' && location.protocol === 'https:'

  const mx = dnsChip(dns?.mx, { ok: t('health.ready'), missing: t('health.missing'), checking: t('health.checking'), warn: dns?.mx?.detail })
  const spf = dnsChip(dns?.spf, {
    ok: t('health.published'),
    missing: t('health.publishTxt'),
    checking: t('health.checking'),
    warn: t('health.publishTxt'),
  })
  const dkim = dnsChip(dns?.dkim, {
    ok: t('health.published'),
    missing: t('health.notInDns'),
    checking: t('health.checking'),
    warn:
      dns?.dkim?.detail === 'no local key'
        ? t('health.noKey')
        : dns?.dkim?.detail?.includes('mismatch')
          ? t('health.mismatch')
          : t('health.notInDns'),
  })
  const dmarc = dnsChip(dns?.dmarc, {
    ok: t('health.published'),
    missing: t('health.publishTxt'),
    checking: t('health.checking'),
    warn: t('health.publishTxt'),
  })

  const chips = [
    { id: 'mx', label: 'MX', title: dns?.mx?.detail, ...mx },
    { id: 'spf', label: 'SPF', title: dns?.spf?.detail, ...spf },
    { id: 'dkim', label: 'DKIM', title: dns?.dkim?.detail, ...dkim },
    { id: 'dmarc', label: 'DMARC', title: dns?.dmarc?.detail, ...dmarc },
    { id: 'tls', label: 'TLS', state: tlsOk ? 'ok' : 'warn', text: tlsOk ? 'https' : 'http' },
    { id: 'queue', label: 'QUEUE', state: dead > 0 ? 'bad' : queueN > 20 ? 'warn' : 'ok', text: String(queueN) },
  ]
  const ago = updatedAt ? Math.max(0, Math.round((Date.now() - updatedAt) / 1000)) : null
  return (
    <div className="health-strip" title={dns?.domain ? `DNS: ${dns.domain}` : undefined}>
      {chips.map((c) => (
        <span key={c.id} className={`health-chip ${c.state}`} title={'title' in c ? c.title : undefined}>
          <span className="dot" />
          <strong>{c.label}</strong> {c.text}
        </span>
      ))}
      <span className="health-meta">
        {dead > 0 ? `${t('health.dead', { n: dead })} · ` : ''}
        {ago == null ? '—' : ago < 5 ? t('health.justNow') : t('health.updated', { s: ago })}
      </span>
    </div>
  )
}

export function App() {
  const { t } = useTranslation()
  const [creds, setCreds] = useState<AdminCreds | null>(() => {
    try {
      return JSON.parse(sessionStorage.getItem('wm_admin') || 'null') as AdminCreds | null
    } catch {
      return null
    }
  })
  const [tab, setTab] = useState<NavTab>('overview')
  const [error, setError] = useState('')
  const [dash, setDash] = useState<Dashboard | null>(null)
  const [spark, setSpark] = useState<SparkSample[]>(loadSpark)
  const [domains, setDomains] = useState<Domain[]>([])
  const [mailboxes, setMailboxes] = useState<Mailbox[]>([])
  const [aliases, setAliases] = useState<Alias[]>([])
  const [selectedDomain, setSelectedDomain] = useState<Domain | null>(null)
  const [selectedMailbox, setSelectedMailbox] = useState<Mailbox | null>(null)
  const [queue, setQueue] = useState<QueueJob[]>([])
  const [quarantine, setQuarantine] = useState<QuarantineItem[]>([])
  const [settings, setSettings] = useState<Record<string, unknown>>({})
  const [audit, setAudit] = useState<AuditEntry[]>([])
  const [form, setForm] = useState({
    domain: '',
    catchAll: '',
    localPart: '',
    password: '',
    displayName: '',
    aliasLocal: '',
    aliasMailboxId: '',
    quotaMb: '',
  })
  const [domainEdit, setDomainEdit] = useState({
    enabled: true,
    catchAll: '',
    defaultQuotaMb: '',
  })
  const [mbEdit, setMbEdit] = useState({
    displayName: '',
    quotaMb: '',
    enabled: true,
    password: '',
  })
  const [login, setLogin] = useState({ username: 'admin', password: '' })
  const [dnsOpen, setDnsOpen] = useState(false)
  const [navOpen, setNavOpen] = useState(false)
  const [dnsStatus, setDnsStatus] = useState<DnsStatus | null>(null)
  const [hostStats, setHostStats] = useState<HostStats | null>(null)
  const [ops, setOps] = useState<OpsStatus | null>(null)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)

  const authed = Boolean(creds?.token)
  const dnsDomain = selectedDomain || domains[0] || null

  const refreshDash = useCallback(async () => {
    const d = await api<Dashboard>('/api/admin/dashboard', creds!)
    setDash(d)
    setSpark(pushSpark(d.queuePending ?? 0))
    setUpdatedAt(Date.now())
    try {
      const q = dnsDomain?.name ? `?domain=${encodeURIComponent(dnsDomain.name)}` : ''
      const [dns, host, opsData] = await Promise.all([
        api<DnsStatus>(`/api/admin/dns-status${q}`, creds!),
        api<HostStats>('/api/admin/host-stats', creds!),
        api<OpsStatus>('/api/admin/ops', creds!),
      ])
      setDnsStatus(dns)
      setHostStats(host)
      setOps(opsData)
    } catch {
      /* ignore probe errors in strip */
    }
    return d
  }, [creds, dnsDomain?.name])

  const loadDomainExtras = useCallback(
    async (domain: Domain) => {
      if (!domain?.id) return
      const [mbs, als] = await Promise.all([
        api<Mailbox[]>(`/api/admin/domains/${domain.id}/mailboxes`, creds!),
        api<Alias[]>(`/api/admin/domains/${domain.id}/aliases`, creds!),
      ])
      setMailboxes(mbs)
      setAliases(als)
    },
    [creds],
  )

  const load = useCallback(async () => {
    if (!authed) return
    setError('')
    try {
      const list = await api<Domain[]>('/api/admin/domains', creds!)
      setDomains(list)
      await refreshDash()
      const settingsData = await api<Record<string, unknown>>('/api/admin/settings', creds!)
      setSettings(settingsData)
      if (tab === 'queue') setQueue(await api<QueueJob[]>('/api/admin/queue', creds!))
      if (tab === 'quarantine') setQuarantine(await api<QuarantineItem[]>('/api/admin/quarantine', creds!))
      if (tab === 'audit') setAudit(await api<AuditEntry[]>('/api/admin/audit', creds!))
      if ((tab === 'mailboxes' || tab === 'domains') && selectedDomain) {
        await loadDomainExtras(selectedDomain)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [authed, creds, tab, selectedDomain, refreshDash, loadDomainExtras])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!authed) return
    if ((tab === 'domains' || tab === 'mailboxes') && !selectedDomain && domains.length) {
      void selectDomain(domains[0])
    }
  }, [authed, tab, domains, selectedDomain])

  useEffect(() => {
    if (!authed) return
    const id = setInterval(() => {
      void refreshDash().catch(() => {})
    }, 20000)
    return () => clearInterval(id)
  }, [authed, refreshDash])

  useEffect(() => {
    if (!navOpen) return
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') setNavOpen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prev
      window.removeEventListener('keydown', onKey)
    }
  }, [navOpen])

  async function doLogin(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setError('')
    try {
      const data = await api<{ token: string; username?: string }>('/api/admin/login', {
        method: 'POST',
        body: login,
      })
      const next: AdminCreds = { token: data.token, user: data.username || login.username }
      sessionStorage.setItem('wm_admin', JSON.stringify(next))
      setCreds(next)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  async function selectDomain(d: Domain) {
    const id = domainId(d)
    if (!id) return
    const sel: Domain = { ...d, id, name: domainName(d) }
    setSelectedDomain(sel)
    setSelectedMailbox(null)
    setDomainEdit({
      enabled: d.enabled !== false,
      catchAll: d.catchAll || '',
      defaultQuotaMb: bytesToMbInput(d.defaultQuotaBytes),
    })
    await loadDomainExtras(sel)
  }

  function selectMailbox(m: Mailbox) {
    setSelectedMailbox(m)
    setMbEdit({
      displayName: m.displayName || '',
      quotaMb: bytesToMbInput(m.quotaBytes),
      enabled: m.enabled !== false,
      password: '',
    })
    setForm((f) => ({ ...f, aliasMailboxId: String(m.id) }))
  }

  async function createDomain(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setBusy(true)
    try {
      await api('/api/admin/domains', {
        ...creds,
        method: 'POST',
        body: { name: form.domain, catchAll: form.catchAll || '' },
      })
      setForm((f) => ({ ...f, domain: '', catchAll: '' }))
      setDomains(await api<Domain[]>('/api/admin/domains', creds!))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function saveDomainSettings(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!selectedDomain) return
    setBusy(true)
    try {
      let defaultQuotaBytes = 0
      try {
        defaultQuotaBytes = mbInputToBytes(domainEdit.defaultQuotaMb)
      } catch {
        setError(t('domains.invalidQuota'))
        return
      }
      const updated = await api<Domain>(`/api/admin/domains/${selectedDomain.id}`, {
        ...creds,
        method: 'PATCH',
        body: {
          enabled: domainEdit.enabled,
          catchAll: domainEdit.catchAll,
          defaultQuotaBytes,
        },
      })
      const list = await api<Domain[]>('/api/admin/domains', creds!)
      setDomains(list)
      const refreshed = list.find((d) => domainId(d) === selectedDomain.id) || updated
      setSelectedDomain({
        ...refreshed,
        id: domainId(refreshed) ?? selectedDomain.id,
        name: domainName(refreshed),
      })
      setDomainEdit({
        enabled: refreshed.enabled !== false,
        catchAll: refreshed.catchAll || '',
        defaultQuotaMb: bytesToMbInput(refreshed.defaultQuotaBytes),
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function createMailbox(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!selectedDomain) return
    setBusy(true)
    try {
      const body: {
        localPart: string
        password: string
        displayName: string
        quotaBytes?: number
      } = {
        localPart: form.localPart,
        password: form.password,
        displayName: form.displayName,
      }
      if (String(form.quotaMb).trim() !== '') {
        try {
          body.quotaBytes = mbInputToBytes(form.quotaMb)
        } catch {
          setError(t('mailboxes.invalidQuota'))
          return
        }
      }
      await api(`/api/admin/domains/${selectedDomain.id}/mailboxes`, {
        ...creds,
        method: 'POST',
        body,
      })
      setForm((f) => ({ ...f, localPart: '', password: '', displayName: '', quotaMb: '' }))
      await loadDomainExtras(selectedDomain)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function saveMailboxSettings(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!selectedDomain || !selectedMailbox) return
    setBusy(true)
    try {
      let quotaBytes = 0
      try {
        quotaBytes = mbInputToBytes(mbEdit.quotaMb)
      } catch {
        setError(t('mailboxes.invalidQuota'))
        return
      }
      const body: {
        displayName: string
        quotaBytes: number
        enabled: boolean
        password?: string
      } = {
        displayName: mbEdit.displayName,
        quotaBytes,
        enabled: mbEdit.enabled,
      }
      if (mbEdit.password) body.password = mbEdit.password
      const updated = await api<Mailbox>(`/api/admin/domains/${selectedDomain.id}/mailboxes/${selectedMailbox.id}`, {
        ...creds,
        method: 'PATCH',
        body,
      })
      await loadDomainExtras(selectedDomain)
      setSelectedMailbox(updated)
      setMbEdit({
        displayName: updated.displayName || '',
        quotaMb: bytesToMbInput(updated.quotaBytes),
        enabled: updated.enabled !== false,
        password: '',
      })
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function openAsUser() {
    if (!selectedDomain || !selectedMailbox) return
    setBusy(true)
    setError('')
    try {
      const data = await api<{ url?: string; token?: string }>(
        `/api/admin/domains/${selectedDomain.id}/mailboxes/${selectedMailbox.id}/impersonate`,
        {
        ...creds,
        method: 'POST',
        },
      )
      if (data.url) {
        window.open(data.url, '_blank', 'noopener,noreferrer')
      } else if (data.token) {
        const base = String(settings['admin.webmail_url'] || '').replace(/\/$/, '')
        if (!base) {
          setError(t('mailboxes.webmailUrlMissing'))
          return
        }
        window.open(`${base}/login?impersonate=${encodeURIComponent(data.token)}`, '_blank', 'noopener,noreferrer')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function createAlias(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!selectedDomain || !form.aliasLocal || !form.aliasMailboxId) return
    setBusy(true)
    try {
      await api(`/api/admin/domains/${selectedDomain.id}/aliases`, {
        ...creds,
        method: 'POST',
        body: { localPart: form.aliasLocal, mailboxId: Number(form.aliasMailboxId) },
      })
      setForm((f) => ({ ...f, aliasLocal: '', aliasMailboxId: form.aliasMailboxId }))
      await loadDomainExtras(selectedDomain)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function genDkim() {
    if (!selectedDomain) return
    setBusy(true)
    try {
      await api(`/api/admin/domains/${selectedDomain.id}/dkim`, { ...creds, method: 'POST' })
      const list = await api<Domain[]>('/api/admin/domains', creds!)
      setDomains(list)
      const refreshed = list.find((d) => domainId(d) === selectedDomain.id)
      if (refreshed) {
        setSelectedDomain({
          ...refreshed,
          id: domainId(refreshed) ?? selectedDomain.id,
          name: domainName(refreshed),
        })
      }
      setDnsOpen(true)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function saveSettings(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setBusy(true)
    try {
      setSettings(
        await api<Record<string, unknown>>('/api/admin/settings', { ...creds, method: 'PUT', body: settings }),
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function downloadBackup() {
    setBusy(true)
    try {
      const data = await api<unknown>('/api/admin/backup', creds!)
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `wernanmail-backup-${new Date().toISOString().slice(0, 10)}.json`
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function restoreBackupFile(file: File | undefined) {
    if (!file) return
    setBusy(true)
    setError('')
    try {
      const text = await file.text()
      const parsed = JSON.parse(text) as {
        data?: { settings?: Record<string, unknown>; domains?: Domain[] }
        settings?: Record<string, unknown>
        domains?: Domain[]
      }
      const payload = parsed?.data && typeof parsed.data === 'object' ? parsed.data : parsed
      const result = await api<{ settings?: number; domains?: number }>('/api/admin/backup/restore', {
        ...creds,
        method: 'POST',
        body: {
          settings: payload.settings || {},
          domains: payload.domains || [],
        },
      })
      await load()
      setError('')
      alert(t('backup.restored', { s: result.settings ?? 0, d: result.domains ?? 0 }))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const healthy = useMemo(() => {
    const pending = dash?.queuePending ?? 0
    const dead = dash?.queueDead ?? 0
    return dead === 0 && pending < 50
  }, [dash])

  const mailboxLabel = (id: string) => {
    const m = mailboxes.find((x) => x.id === id)
    return m ? `${m.localPart}@${selectedDomain?.name || ''}` : `#${id}`
  }

  if (!authed) {
    return (
      <div className="login-page">
        <a className="mode-switch" href="/">
          {t('mode.asUser')}
        </a>
        <aside className="login-hero" aria-label={t('app.name')}>
          <div className="login-aurora" aria-hidden>
            <span className="blob a" />
            <span className="blob b" />
            <span className="blob c" />
          </div>
          <div className="login-grain" aria-hidden />
          <div className="login-hero-content">
            <div className="login-mark" aria-hidden>
              <svg viewBox="0 0 32 32" width="28" height="28">
                <rect width="32" height="32" rx="8" fill="currentColor" opacity="0.14" />
                <path
                  d="M7 11.2c0-.66.54-1.2 1.2-1.2h15.6c.66 0 1.2.54 1.2 1.2v9.6c0 .66-.54 1.2-1.2 1.2H8.2c-.66 0-1.2-.54-1.2-1.2v-9.6zm2.1.55 6.9 4.55 6.9-4.55"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1.6"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
            </div>
            <p className="login-brand">{t('app.name')}</p>
            <p className="login-tagline">{t('app.tagline')}</p>
          </div>
        </aside>

        <section className="login-panel">
          <form className="login-form" onSubmit={doLogin}>
            <h2 className="login-title">{t('login.title')}</h2>
            <p className="login-sub">{t('login.subtitle')}</p>
            <div className="login-field">
              <label htmlFor="admin-user">{t('login.username')}</label>
              <input
                id="admin-user"
                value={login.username}
                onChange={(e) => setLogin({ ...login, username: e.target.value })}
                autoComplete="username"
                autoFocus
              />
            </div>
            <div className="login-field">
              <label htmlFor="admin-pass">{t('login.password')}</label>
              <input
                id="admin-pass"
                type="password"
                value={login.password}
                onChange={(e) => setLogin({ ...login, password: e.target.value })}
                autoComplete="current-password"
              />
            </div>
            {error ? <p className="login-err" role="alert">{error}</p> : null}
            <button className="login-submit" type="submit">
              {t('login.submit')}
            </button>
          </form>
        </section>
      </div>
    )
  }

  function logoutAdmin() {
    if (creds?.token) {
      void api('/api/admin/logout', { method: 'POST', token: creds.token }).catch(() => {})
    }
    sessionStorage.removeItem('wm_admin')
    setCreds(null)
    setNavOpen(false)
  }

  function renderNavButtons() {
    return NAV.map((id) => (
      <button
        key={id}
        type="button"
        className={tab === id ? 'active' : ''}
        onClick={() => {
          setTab(id)
          setNavOpen(false)
        }}
      >
        {t(`nav.${id}`)}
      </button>
    ))
  }

  return (
    <div className={`shell${navOpen ? ' nav-open' : ''}`}>
      <header className="topbar">
        <h1 className="brand-mark">
          {t('app.name')}
          <span>{t('app.adminSubtitle')}</span>
        </h1>
        <button
          type="button"
          className={`nav-toggle${navOpen ? ' open' : ''}`}
          aria-expanded={navOpen}
          aria-controls="admin-nav-drawer"
          aria-label={navOpen ? t('actions.closeMenu') : t('actions.menu')}
          onClick={() => setNavOpen((v) => !v)}
        >
          <span />
          <span />
          <span />
        </button>
        <nav className="top-nav top-nav-desktop" aria-label={t('app.adminSubtitle')}>
          {renderNavButtons()}
        </nav>
        <div className="topbar-actions">
          <button type="button" className="ghost" onClick={logoutAdmin}>
            {t('actions.logout')}
          </button>
        </div>
      </header>

      {/* Drawer outside sticky header — iOS backdrop-filter breaks position:fixed inside it */}
      {navOpen ? (
        <button type="button" className="nav-backdrop" aria-label={t('actions.closeMenu')} onClick={() => setNavOpen(false)} />
      ) : null}
      <nav
        id="admin-nav-drawer"
        className={`top-nav top-nav-drawer${navOpen ? ' open' : ''}`}
        aria-hidden={!navOpen}
      >
        {renderNavButtons()}
        <button type="button" className="ghost nav-logout-mobile" onClick={logoutAdmin}>
          {t('actions.logout')}
        </button>
      </nav>

      {tab !== 'overview' ? <HealthStrip dash={dash} dns={dnsStatus} updatedAt={updatedAt} /> : null}

      <main className="main">
        {error ? <p className="err">{error}</p> : null}

        {tab === 'overview' ? (
          <div className="overview">
            <div>
              <section className="hero">
                <h2>{healthy ? t('overview.healthyTitle') : t('overview.attentionTitle')}</h2>
                <p>
                  {healthy
                    ? dnsStatus?.dkim?.state === 'ok'
                      ? t('overview.healthyBodyOk')
                      : t('overview.healthyBody')
                    : t('overview.attentionBody')}
                </p>
              </section>
              <HealthStrip dash={dash} dns={dnsStatus} updatedAt={updatedAt} />
              <ResourcesPanel host={hostStats} />
              <div className="overview-metrics">
                <div className="metric">
                  <h3>{t('overview.queueTitle')}</h3>
                  <Sparkline samples={spark} />
                  <p className="muted metric-foot">
                    {t('overview.pendingNow')}: <strong>{dash?.queuePending ?? '—'}</strong>
                    {dash?.queueDead ? ` · ${t('overview.deadShort', { n: dash.queueDead })}` : ''}
                  </p>
                </div>
                <div className="metric">
                  <h3>{t('overview.quarantineTitle')}</h3>
                  <p className="big">{dash?.quarantine ?? '—'}</p>
                  <button type="button" className="linkish" onClick={() => setTab('quarantine')}>
                    {t('overview.viewQuarantine')}
                  </button>
                </div>
              </div>
              <div className="panel policy-panel">
                <h3>{t('overview.snapTitle')}</h3>
                <div className="snap-grid">
                  <div>
                    <span className="muted">{t('overview.quota')}</span>
                    <strong>{formatBytes(settings['mail.default_quota_bytes'])}</strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.spamQ')}</span>
                    <strong>{String(settings['antispam.quarantine_at'] ?? '—')}</strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.spamR')}</span>
                    <strong>{String(settings['antispam.reject_at'] ?? '—')}</strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.av')}</span>
                    <strong>
                      {String(settings['antivirus.enabled']).toLowerCase() === 'true' ? t('overview.on') : t('overview.off')}
                    </strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.relay')}</span>
                    <strong>{String(settings['mail.relay_host'] || t('overview.none'))}</strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.greylist')}</span>
                    <strong>
                      {Number(ops?.greylistSeconds || settings['mail.greylist_seconds'] || 0) > 0
                        ? t('overview.greylistOn', { s: ops?.greylistSeconds ?? settings['mail.greylist_seconds'] })
                        : t('overview.off')}
                    </strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.bounces')}</span>
                    <strong>
                      {(ops?.bounceEnabled ?? String(settings['mail.bounce_enabled']).toLowerCase() === 'true')
                        ? t('overview.on')
                        : t('overview.off')}
                    </strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.tls')}</span>
                    <strong>{ops?.tlsConfigured ? t('overview.on') : t('overview.tlsOff')}</strong>
                  </div>
                  <div>
                    <span className="muted">{t('overview.sendRate')}</span>
                    <strong>{String(ops?.rateSendPerHour ?? settings['mail.rate_send_per_hour'] ?? '—')}/h</strong>
                  </div>
                </div>
              </div>
            </div>
            <aside className="stack-aside">
              <div className="panel">
                <h3>{t('overview.dnsTitle')}</h3>
                <p className="muted">{t('overview.dnsBody')}</p>
                <p className="muted domains-count">
                  {t('overview.domainsCount')}: <strong>{dash?.domains ?? domains.length}</strong>
                </p>
                <div className="row">
                  <button type="button" className="primary" onClick={() => setDnsOpen(true)}>
                    {t('overview.openDns')}
                  </button>
                  <button type="button" className="ghost" onClick={() => setTab('domains')}>
                    {t('nav.domains')}
                  </button>
                </div>
              </div>
            </aside>
          </div>
        ) : null}

        {tab === 'domains' ? (
          <>
            <div className="page-head">
              <div>
                <h2>{t('domains.title')}</h2>
                <p>{t('domains.subtitle')}</p>
              </div>
              <form className="row" onSubmit={createDomain}>
                <input
                  placeholder={t('domains.placeholder')}
                  value={form.domain}
                  onChange={(e) => setForm({ ...form, domain: e.target.value })}
                  required
                />
                <input
                  placeholder={t('domains.catchAll')}
                  value={form.catchAll}
                  onChange={(e) => setForm({ ...form, catchAll: e.target.value })}
                />
                <button className="primary" type="submit" disabled={busy}>
                  {t('domains.add')}
                </button>
              </form>
            </div>
            <div className="split">
              <div className="panel list-panel">
                <table>
                  <thead>
                    <tr>
                      <th>{t('domains.colDomain')}</th>
                      <th>{t('domains.colDkim')}</th>
                      <th>{t('domains.colStatus')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {domains.map((d) => {
                      const id = domainId(d)
                      return (
                        <tr
                          key={id}
                          className={selectedDomain?.id === id ? 'selected' : ''}
                          onClick={() => void selectDomain(d)}
                        >
                          <td>{domainName(d)}</td>
                          <td>{d.dkimPublic ? t('domains.dkimReady') : '—'}</td>
                          <td>
                            <span className="badge">{d.enabled === false ? t('domains.off') : t('domains.active')}</span>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
                {!domains.length ? <p className="empty">{t('domains.empty')}</p> : null}
              </div>
              <div className="panel detail-panel">
                {selectedDomain ? (
                  <>
                    <h3>{selectedDomain.name}</h3>
                    <div className="row">
                      <button type="button" className="primary" onClick={() => void genDkim()} disabled={busy}>
                        {selectedDomain.dkimPublic ? t('domains.rotateDkim') : t('domains.genDkim')}
                      </button>
                      <button type="button" className="ghost" onClick={() => setDnsOpen(true)}>
                        {t('domains.dnsRecords')}
                      </button>
                      <button type="button" className="ghost" onClick={() => setTab('mailboxes')}>
                        {t('domains.mailboxes')}
                      </button>
                    </div>
                    <div className="detail-section">
                      <h4>{t('domains.settings')}</h4>
                      <form className="form-stack" onSubmit={saveDomainSettings}>
                        <label className="check-row">
                          <input
                            type="checkbox"
                            checked={domainEdit.enabled}
                            onChange={(e) => setDomainEdit({ ...domainEdit, enabled: e.target.checked })}
                          />
                          <span>{t('domains.enabled')}</span>
                        </label>
                        <div className="field">
                          <label>{t('domains.catchAll')}</label>
                          <input
                            value={domainEdit.catchAll}
                            onChange={(e) => setDomainEdit({ ...domainEdit, catchAll: e.target.value })}
                            placeholder={t('domains.catchAllPh')}
                          />
                        </div>
                        <div className="field">
                          <label>{t('domains.defaultQuotaMb')}</label>
                          <input
                            type="number"
                            min="0"
                            step="1"
                            value={domainEdit.defaultQuotaMb}
                            onChange={(e) => setDomainEdit({ ...domainEdit, defaultQuotaMb: e.target.value })}
                            placeholder={t('domains.quotaInherit')}
                          />
                          <p className="muted foot-note">{t('domains.quotaHint')}</p>
                        </div>
                        <div className="detail-actions">
                          <button className="primary" type="submit" disabled={busy}>
                            {t('actions.save')}
                          </button>
                        </div>
                      </form>
                    </div>
                    <div className="detail-section">
                      <h4>{t('domains.selector')}</h4>
                      <code>{selectedDomain.dkimSelector || 'wernan'}</code>
                    </div>
                    <div className="detail-section">
                      <h4>{t('domains.quickAdd')}</h4>
                      <form className="form-stack" onSubmit={createMailbox}>
                        <div className="field">
                          <label>{t('domains.localPart')}</label>
                          <input
                            value={form.localPart}
                            onChange={(e) => setForm({ ...form, localPart: e.target.value })}
                            required
                          />
                        </div>
                        <div className="field">
                          <label>{t('domains.displayName')}</label>
                          <input
                            value={form.displayName}
                            onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                          />
                        </div>
                        <div className="field">
                          <label>{t('domains.password')}</label>
                          <input
                            type="password"
                            value={form.password}
                            onChange={(e) => setForm({ ...form, password: e.target.value })}
                            required
                          />
                        </div>
                        <div className="field">
                          <label>{t('mailboxes.quotaMb')}</label>
                          <input
                            type="number"
                            min="0"
                            step="1"
                            value={form.quotaMb}
                            onChange={(e) => setForm({ ...form, quotaMb: e.target.value })}
                            placeholder={t('mailboxes.quotaDefault')}
                          />
                        </div>
                        <div className="detail-actions">
                          <button className="primary" type="submit" disabled={busy}>
                            {t('domains.addMailbox')}
                          </button>
                        </div>
                      </form>
                      <p className="muted foot-note">{t('domains.mailboxCount', { n: mailboxes.length })}</p>
                    </div>
                    <div className="detail-section">
                      <h4>{t('domains.aliases')}</h4>
                      <form className="row" onSubmit={createAlias}>
                        <input
                          placeholder={t('domains.aliasLocal')}
                          value={form.aliasLocal}
                          onChange={(e) => setForm({ ...form, aliasLocal: e.target.value })}
                          required
                        />
                        <Select
                          aria-label={t('domains.aliasTarget')}
                          placeholder={t('domains.aliasTarget')}
                          value={form.aliasMailboxId}
                          onChange={(v) => setForm({ ...form, aliasMailboxId: String(v) })}
                          options={mailboxes.map((m) => ({
                            value: String(m.id),
                            label: `${m.localPart}@${selectedDomain.name}`,
                          }))}
                        />
                        <button className="primary" type="submit" disabled={busy || !mailboxes.length}>
                          {t('domains.addAlias')}
                        </button>
                      </form>
                      {aliases.length ? (
                        <table>
                          <tbody>
                            {aliases.map((a) => (
                              <tr key={a.id} style={{ cursor: 'default' }}>
                                <td>
                                  {a.localPart}@{selectedDomain.name}
                                </td>
                                <td>→ {mailboxLabel(a.mailboxId)}</td>
                              </tr>
                            ))}
                          </tbody>
                        </table>
                      ) : (
                        <p className="muted">{t('domains.noAliases')}</p>
                      )}
                    </div>
                  </>
                ) : (
                  <p className="empty">{t('domains.select')}</p>
                )}
              </div>
            </div>
          </>
        ) : null}

        {tab === 'mailboxes' ? (
          <>
            <div className="page-head">
              <div>
                <h2>{t('mailboxes.title')}</h2>
                <p>{t('mailboxes.subtitle')}</p>
              </div>
            </div>
            <div className="list-toolbar panel" style={{ marginBottom: '0.85rem' }}>
              <Select
                aria-label={t('mailboxes.selectDomain')}
                placeholder={t('mailboxes.selectDomain')}
                value={selectedDomain?.id || ''}
                onChange={(v) => {
                  const d = domains.find((x) => String(domainId(x)) === String(v))
                  if (d) void selectDomain(d)
                }}
                options={[
                  { value: '', label: t('mailboxes.selectDomain') },
                  ...domains.map((d) => ({ value: String(domainId(d)), label: domainName(d) })),
                ]}
              />
              <button type="button" className="ghost" onClick={() => void load()} disabled={!selectedDomain}>
                {t('actions.refresh')}
              </button>
            </div>
            {selectedDomain ? (
              <div className="split">
                <div className="panel list-panel">
                  <table>
                    <thead>
                      <tr>
                        <th>{t('mailboxes.colMailbox')}</th>
                        <th>{t('mailboxes.colQuota')}</th>
                        <th>{t('mailboxes.colStatus')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {mailboxes.map((m) => (
                        <tr
                          key={m.id}
                          className={selectedMailbox?.id === m.id ? 'selected' : ''}
                          onClick={() => selectMailbox(m)}
                        >
                          <td>
                            {m.localPart}@{selectedDomain.name}
                          </td>
                          <td>{m.quotaBytes ? formatBytes(m.quotaBytes) : '∞'}</td>
                          <td>
                            <span className="badge">{m.enabled === false ? t('domains.off') : t('domains.active')}</span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {!mailboxes.length ? <p className="empty">{t('mailboxes.empty')}</p> : null}
                </div>
                <div className="panel detail-panel">
                  {selectedMailbox ? (
                    <>
                      <h3>
                        {selectedMailbox.localPart}@{selectedDomain.name}
                      </h3>
                      <div className="detail-section">
                        <h4>{t('mailboxes.details')}</h4>
                        <form className="form-stack" onSubmit={saveMailboxSettings}>
                          <div className="field">
                            <label>{t('domains.displayName')}</label>
                            <input
                              value={mbEdit.displayName}
                              onChange={(e) => setMbEdit({ ...mbEdit, displayName: e.target.value })}
                            />
                          </div>
                          <div className="field">
                            <label>{t('mailboxes.quotaMb')}</label>
                            <input
                              type="number"
                              min="0"
                              step="1"
                              value={mbEdit.quotaMb}
                              onChange={(e) => setMbEdit({ ...mbEdit, quotaMb: e.target.value })}
                              placeholder={t('mailboxes.unlimited')}
                            />
                            <p className="muted foot-note">{t('mailboxes.quotaHint')}</p>
                          </div>
                          <div className="field">
                            <label>{t('mailboxes.newPassword')}</label>
                            <input
                              type="password"
                              value={mbEdit.password}
                              onChange={(e) => setMbEdit({ ...mbEdit, password: e.target.value })}
                              autoComplete="new-password"
                              placeholder={t('mailboxes.passwordOptional')}
                            />
                          </div>
                          <label className="check-row">
                            <input
                              type="checkbox"
                              checked={mbEdit.enabled}
                              onChange={(e) => setMbEdit({ ...mbEdit, enabled: e.target.checked })}
                            />
                            <span>{t('domains.enabled')}</span>
                          </label>
                          <div className="detail-actions">
                            <button className="primary" type="submit" disabled={busy}>
                              {t('actions.save')}
                            </button>
                            <button
                              type="button"
                              className="ghost"
                              disabled={busy || String(settings['admin.superuser_enabled']).toLowerCase() !== 'true'}
                              onClick={() => void openAsUser()}
                              title={
                                String(settings['admin.superuser_enabled']).toLowerCase() !== 'true'
                                  ? t('mailboxes.superuserOff')
                                  : t('mailboxes.openAsUser')
                              }
                            >
                              {t('mailboxes.openAsUser')}
                            </button>
                          </div>
                        </form>
                        <p className="muted created-line">
                          {t('mailboxes.created')}: {selectedMailbox.createdAt || '—'}
                        </p>
                      </div>
                      <div className="detail-section">
                        <h4>{t('domains.aliases')}</h4>
                        <form className="row" onSubmit={createAlias}>
                          <input
                            placeholder={t('domains.aliasLocal')}
                            value={form.aliasLocal}
                            onChange={(e) => setForm({ ...form, aliasLocal: e.target.value })}
                            required
                          />
                          <button className="primary" type="submit" disabled={busy}>
                            {t('domains.addAlias')}
                          </button>
                        </form>
                        {aliases.filter((a) => a.mailboxId === selectedMailbox.id).length ? (
                          <ul className="alias-list">
                            {aliases
                              .filter((a) => a.mailboxId === selectedMailbox.id)
                              .map((a) => (
                                <li key={a.id}>
                                  {a.localPart}@{selectedDomain.name}
                                </li>
                              ))}
                          </ul>
                        ) : (
                          <p className="muted">{t('domains.noAliases')}</p>
                        )}
                      </div>
                      <div className="detail-section">
                        <h4>{t('mailboxes.addAnother')}</h4>
                        <form onSubmit={createMailbox}>
                          <div className="row">
                            <input
                              placeholder={t('domains.localPart')}
                              value={form.localPart}
                              onChange={(e) => setForm({ ...form, localPart: e.target.value })}
                              required
                            />
                            <input
                              placeholder={t('domains.displayName')}
                              value={form.displayName}
                              onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                            />
                            <input
                              type="password"
                              placeholder={t('domains.password')}
                              value={form.password}
                              onChange={(e) => setForm({ ...form, password: e.target.value })}
                              required
                            />
                            <input
                              type="number"
                              min="0"
                              placeholder={t('mailboxes.quotaMb')}
                              value={form.quotaMb}
                              onChange={(e) => setForm({ ...form, quotaMb: e.target.value })}
                            />
                            <button className="primary" type="submit" disabled={busy}>
                              {t('actions.add')}
                            </button>
                          </div>
                        </form>
                      </div>
                    </>
                  ) : (
                    <p className="empty">{t('mailboxes.select')}</p>
                  )}
                </div>
              </div>
            ) : (
              <p className="empty panel">{t('mailboxes.chooseDomain')}</p>
            )}
          </>
        ) : null}

        {tab === 'queue' ? (
          <>
            <div className="page-head">
              <div>
                <h2>{t('queue.title')}</h2>
                <p>{t('queue.subtitle')}</p>
              </div>
            </div>
            <div className="panel list-panel">
              <table>
                <thead>
                  <tr>
                    <th>{t('queue.colId')}</th>
                    <th>{t('queue.colKind')}</th>
                    <th>{t('queue.colAttempts')}</th>
                    <th>{t('queue.colError')}</th>
                    <th>{t('queue.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {queue.map((j) => {
                    const dead = j.attempts >= j.maxAttempts
                    return (
                      <tr key={j.id}>
                        <td>{j.id}</td>
                        <td>
                          {j.kind}
                          {dead ? <span className="pill bad">{t('queue.dead')}</span> : null}
                        </td>
                        <td>
                          {j.attempts}/{j.maxAttempts}
                        </td>
                        <td>{j.lastError || '—'}</td>
                        <td className="row">
                          <button
                            type="button"
                            className="ghost"
                            onClick={async () => {
                              await api(`/api/admin/queue/${j.id}/retry`, { ...creds, method: 'POST' })
                              setQueue(await api<QueueJob[]>('/api/admin/queue', creds!))
                              await refreshDash()
                            }}
                          >
                            {t('queue.retry')}
                          </button>
                          <button
                            type="button"
                            className="ghost"
                            onClick={async () => {
                              await api(`/api/admin/queue/${j.id}/delete`, { ...creds, method: 'POST' })
                              setQueue(await api<QueueJob[]>('/api/admin/queue', creds!))
                              await refreshDash()
                            }}
                          >
                            {t('actions.delete')}
                          </button>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
              {!queue.length ? <p className="empty">{t('queue.empty')}</p> : null}
            </div>
          </>
        ) : null}

        {tab === 'quarantine' ? (
          <>
            <div className="page-head">
              <div>
                <h2>{t('quarantine.title')}</h2>
                <p>{t('quarantine.subtitle')}</p>
              </div>
            </div>
            <div className="panel list-panel">
              <table>
                <thead>
                  <tr>
                    <th>{t('quarantine.colFrom')}</th>
                    <th>{t('quarantine.colSubject')}</th>
                    <th>{t('quarantine.colVerdict')}</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {quarantine.map((q) => (
                    <tr key={q.id} style={{ cursor: 'default' }}>
                      <td>{q.fromAddr}</td>
                      <td>{q.subject}</td>
                      <td>
                        <code style={{ fontSize: '0.75rem' }}>{q.verdictJson}</code>
                      </td>
                      <td className="row">
                        <button
                          type="button"
                          className="ghost"
                          onClick={async () => {
                            await api(`/api/admin/quarantine/${q.id}/release`, { ...creds, method: 'POST' })
                            setQuarantine(await api<QuarantineItem[]>('/api/admin/quarantine', creds!))
                            await refreshDash()
                          }}
                        >
                          {t('actions.release')}
                        </button>
                        <button
                          type="button"
                          className="ghost"
                          onClick={async () => {
                            await api(`/api/admin/quarantine/${q.id}/delete`, { ...creds, method: 'POST' })
                            setQuarantine(await api<QuarantineItem[]>('/api/admin/quarantine', creds!))
                            await refreshDash()
                          }}
                        >
                          {t('actions.delete')}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!quarantine.length ? <p className="empty">{t('quarantine.empty')}</p> : null}
            </div>
          </>
        ) : null}

        {tab === 'settings' ? (
          <>
            <div className="page-head">
              <div>
                <h2>{t('settings.title')}</h2>
                <p>{t('settings.subtitle')}</p>
              </div>
            </div>
            <form className="settings-layout" onSubmit={saveSettings}>
              <section className="panel settings-card">
                <div className="settings-card-head">
                  <h3>{t('settings.groups.interface')}</h3>
                  <p className="muted">{t('settings.groupHints.interface')}</p>
                </div>
                <div className="settings-field">
                  <div className="settings-field-meta">
                    <label htmlFor="admin-lang">{t('lang.label')}</label>
                    <p className="muted">{t('settings.langHint')}</p>
                  </div>
                  <LangSelect />
                </div>
              </section>

              {SETTING_GROUPS.map((g) => (
                <section key={g.id} className="panel settings-card">
                  <div className="settings-card-head">
                    <h3>{t(`settings.groups.${g.id}`)}</h3>
                    <p className="muted">{t(`settings.groupHints.${g.id}`)}</p>
                  </div>
                  <div className="settings-fields">
                    {g.fields.map((field) => {
                      const label = t(`settings.keys.${field.key}`, { defaultValue: field.key })
                      const value = String(settings[field.key] ?? '')
                      const boolOn = String(value).toLowerCase() === 'true'
                      return (
                        <div className="settings-field" key={field.key}>
                          <div className="settings-field-meta">
                            <label htmlFor={`set-${field.key}`}>{label}</label>
                          </div>
                          {field.type === 'bool' ? (
                            <label className="settings-toggle">
                              <input
                                id={`set-${field.key}`}
                                type="checkbox"
                                checked={boolOn}
                                onChange={(e) =>
                                  setSettings({ ...settings, [field.key]: e.target.checked ? 'true' : 'false' })
                                }
                              />
                              <span>{boolOn ? t('overview.on') : t('overview.off')}</span>
                            </label>
                          ) : field.type === 'textarea' ? (
                            <textarea
                              id={`set-${field.key}`}
                              rows={3}
                              value={value}
                              onChange={(e) => setSettings({ ...settings, [field.key]: e.target.value })}
                            />
                          ) : (
                            <input
                              id={`set-${field.key}`}
                              type={field.type === 'number' ? 'number' : 'text'}
                              value={value}
                              onChange={(e) => setSettings({ ...settings, [field.key]: e.target.value })}
                            />
                          )}
                        </div>
                      )
                    })}
                  </div>
                </section>
              ))}

              {!Object.keys(settings).length ? <p className="empty">{t('settings.empty')}</p> : null}
              <div className="settings-save">
                <button className="primary" type="submit" disabled={busy}>
                  {t('settings.save')}
                </button>
              </div>
            </form>
          </>
        ) : null}

        {tab === 'backup' ? (
          <>
            <div className="page-head">
              <div>
                <h2>{t('backup.title')}</h2>
                <p>{t('backup.subtitle')}</p>
              </div>
            </div>
            <div className="panel">
              <p className="muted">{t('backup.hint')}</p>
              <div className="row" style={{ gap: '0.75rem', flexWrap: 'wrap' }}>
                <button type="button" className="primary" onClick={() => void downloadBackup()} disabled={busy}>
                  {busy ? t('backup.loading') : t('backup.download')}
                </button>
                <label className="ghost" style={{ display: 'inline-flex', alignItems: 'center', cursor: 'pointer' }}>
                  {t('backup.restore')}
                  <input
                    type="file"
                    accept="application/json,.json"
                    hidden
                    disabled={busy}
                    onChange={(e) => {
                      const f = e.target.files?.[0]
                      e.target.value = ''
                      void restoreBackupFile(f)
                    }}
                  />
                </label>
              </div>
              <p className="muted" style={{ marginTop: '0.75rem' }}>
                {t('backup.restoreHint')}
              </p>
            </div>
          </>
        ) : null}

        {tab === 'audit' ? (
          <>
            <div className="page-head">
              <div>
                <h2>{t('audit.title')}</h2>
                <p>{t('audit.subtitle')}</p>
              </div>
            </div>
            <div className="panel list-panel">
              <table>
                <thead>
                  <tr>
                    <th>{t('audit.colWhen')}</th>
                    <th>{t('audit.colActor')}</th>
                    <th>{t('audit.colAction')}</th>
                    <th>{t('audit.colTarget')}</th>
                  </tr>
                </thead>
                <tbody>
                  {audit.map((a) => (
                    <tr key={a.id} style={{ cursor: 'default' }}>
                      <td>{a.createdAt}</td>
                      <td>{a.actor}</td>
                      <td>{a.action}</td>
                      <td>{a.target}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!audit.length ? <p className="empty">{t('audit.empty')}</p> : null}
            </div>
          </>
        ) : null}
      </main>

      <DNSDrawer open={dnsOpen} onClose={() => setDnsOpen(false)} domain={dnsDomain} />
    </div>
  )
}
