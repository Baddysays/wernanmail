import { useCallback, useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { LOCALES, setAdminLang } from './i18n'
import { api, asList, dnsRecordsFor, domainId, domainName, isUnauthorized } from './api'
import { MailboxFilters } from './MailboxFilters'
import { Select } from './Select'
import type {
  AdminCreds,
  Alias,
  AuditEntry,
  Dashboard,
  DmarcReport,
  DnsCheck,
  DnsStatus,
  Domain,
  HostStats,
  Mailbox,
  NavTab,
  OpsStatus,
  Posture,
  QuarantineItem,
  QueueJob,
  SettingGroup,
  SpamReason,
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

function QuotaBar({ used, quota }: { used?: number; quota?: number }) {
  const u = Number(used) || 0
  const q = Number(quota) || 0
  if (!q) {
    return (
      <div className="quota-cell">
        <span>{formatBytes(u)} / ∞</span>
      </div>
    )
  }
  const pct = Math.max(0, Math.min(100, (u / q) * 100))
  return (
    <div className="quota-cell">
      <span>
        {formatBytes(u)} / {formatBytes(q)}
      </span>
      <div className="quota-bar" aria-hidden>
        <span style={{ width: `${pct}%` }} />
      </div>
    </div>
  )
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

function formatWhen(raw?: string): string {
  if (!raw) return '—'
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return String(raw)
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function parseVerdict(raw?: string): { score?: number; action?: string; reasons: string[] } {
  if (!raw) return { reasons: [] }
  try {
    const v = JSON.parse(raw) as { score?: number; action?: string; reasons?: SpamReason[] }
    const reasons = (v.reasons || [])
      .map((r) => [r.code, r.detail].filter(Boolean).join(': '))
      .filter(Boolean)
    return { score: v.score, action: v.action, reasons }
  } catch {
    return { reasons: [] }
  }
}

function settingKeyLabel(
  t: (key: string, opts?: Record<string, unknown>) => string,
  key: string,
): string {
  const bag = t('settings.keys', { returnObjects: true }) as unknown
  if (bag && typeof bag === 'object' && !Array.isArray(bag) && key in (bag as Record<string, unknown>)) {
    const v = (bag as Record<string, unknown>)[key]
    if (typeof v === 'string' && v) return v
  }
  return key
}

function queueKindLabel(kind: string, t: (key: string, opts?: Record<string, unknown>) => string): string {
  const key = `queue.kinds.${kind}`
  const label = t(key, { defaultValue: '' })
  return label || kind
}

function queuePayloadSummary(raw?: string): string {
  if (!raw) return ''
  try {
    const p = JSON.parse(raw) as {
      to?: string | string[]
      from?: string
      failedTo?: string
      subject?: string
    }
    const to = Array.isArray(p.to) ? p.to.filter(Boolean).join(', ') : p.to
    if (to) return String(to)
    if (p.failedTo) return String(p.failedTo)
    if (p.from) return String(p.from)
    if (p.subject) return String(p.subject)
  } catch {
    /* ignore */
  }
  return ''
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
      id="admin-lang"
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

function ResourcesPanel({ host, ops }: { host: HostStats | null; ops?: OpsStatus | null }) {
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
      <p className="resources-foot muted">
        {typeof ops?.schemaVersion === 'number' ? (
          <span>{t('resources.schema', { v: ops.schemaVersion })}</span>
        ) : null}
        <a href="/metrics" target="_blank" rel="noreferrer">
          {t('resources.metrics')}
        </a>
      </p>
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
  publicIP,
}: {
  open: boolean
  onClose: () => void
  domain: Domain | null
  publicIP?: string
}) {
  const { t } = useTranslation()
  useEffect(() => {
    if (!open) return
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])
  if (!open) return null
  const records = dnsRecordsFor(domain, publicIP)
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

function DeliverabilityCard({
  dns,
  reports,
  posture,
  onRecheck,
}: {
  dns: DnsStatus | null
  reports: DmarcReport[] | null
  posture: Posture | null
  onRecheck?: () => void
}) {
  const { t } = useTranslation()
  const checks = [
    { id: 'spf', label: 'SPF', check: dns?.spf },
    { id: 'dkim', label: 'DKIM', check: dns?.dkim },
    { id: 'dmarc', label: 'DMARC', check: dns?.dmarc },
    {
      id: 'ptr',
      label: 'PTR',
      check: posture?.ptr,
      okText: posture?.ip ? t('deliverability.ptrOk') : t('deliverability.ready'),
    },
    {
      id: 'rbl',
      label: 'IP',
      check: posture?.rbl,
      okText: t('deliverability.ipClean'),
    },
    {
      id: 'spam',
      label: 'Spam',
      check: posture?.antispam
        ? { state: posture.antispam.state, detail: posture.antispam.detail }
        : undefined,
      okText: t('deliverability.spamOk'),
    },
  ]

  const probe = posture?.antispam?.probe

  return (
    <section className="panel deliverability-card">
      <div className="deliverability-head">
        <h3>{t('deliverability.title')}</h3>
        {onRecheck ? (
          <button type="button" className="ghost deliverability-recheck" onClick={onRecheck}>
            {t('deliverability.recheck')}
          </button>
        ) : null}
      </div>
      <div className="deliverability-checks">
        {checks.map(({ id, label, check, okText }) => {
          const status = dnsChip(check, {
            ok: okText || t('deliverability.ready'),
            missing: t('deliverability.missing'),
            checking: t('health.checking'),
            warn: check?.detail,
          })
          return (
            <div className="deliverability-check" key={id} title={check?.detail}>
              <span className={`status-dot ${status.state === 'ok' ? 'on' : status.state === 'bad' ? 'off' : ''}`} />
              <strong>{label}</strong>
              <span>{status.state === 'ok' ? status.text : check?.detail || status.text}</span>
            </div>
          )
        })}
      </div>
      {posture?.ip ? (
        <p className="deliverability-ip muted" title={posture.ipSource}>
          {t('deliverability.outboundIp', { ip: posture.ip })}
          {posture.ehlo ? ` · EHLO ${posture.ehlo}` : ''}
        </p>
      ) : null}
      {probe ? (
        <p className="deliverability-probe muted" title={posture?.antispam?.detail}>
          {t('deliverability.probe', {
            clean: probe.clean?.action || '—',
            spam: probe.spammy?.action || '—',
            score: typeof probe.spammy?.score === 'number' ? probe.spammy.score.toFixed(1) : '—',
          })}
        </p>
      ) : null}
      <p className="deliverability-hint">
        <a href="https://postmaster.google.com/" target="_blank" rel="noreferrer">
          {t('deliverability.postmaster')}
        </a>
        <span>{t('deliverability.recipientHint')}</span>
      </p>
      {reports ? (
        <div className="dmarc-summary">
          <p className="dmarc-summary-title">{t('deliverability.reports')}</p>
          {reports.length ? (
            <div className="dmarc-report-list">
              {reports.slice(0, 5).map((report, index) => (
                <div className="dmarc-report-row" key={report.id ?? `${report.ip || report.sourceIp}-${index}`}>
                  <code>{report.ip || report.sourceIp || '—'}</code>
                  <span>×{report.count ?? report.messageCount ?? 0}</span>
                  <span>DKIM {report.dkim || '—'}</span>
                  <span>SPF {report.spf || '—'}</span>
                </div>
              ))}
            </div>
          ) : (
            <>
              <p className="muted dmarc-empty">{t('deliverability.noReports')}</p>
              <p className="muted dmarc-empty-hint">{t('deliverability.noReportsHint')}</p>
            </>
          )}
        </div>
      ) : null}
    </section>
  )
}

function HealthStrip({
  dash,
  dns,
  ops,
  posture,
  updatedAt,
  onRefresh,
}: {
  dash: Dashboard | null
  dns: DnsStatus | null
  ops: OpsStatus | null
  posture: Posture | null
  updatedAt: number | null
  onRefresh?: () => void
}) {
  const { t } = useTranslation()
  const queueN = dash?.queuePending ?? posture?.queue?.pending ?? 0
  const dead = dash?.queueDead ?? posture?.queue?.dead ?? 0
  const tlsOk = Boolean(ops?.tlsConfigured)
  const stackMissing = posture?.stack?.missing?.length ?? 0
  const stackRunning = posture?.stack?.running?.length ?? 0
  const systemsOk = dead === 0 && queueN < 50 && stackMissing === 0

  const [, setTick] = useState(0)
  useEffect(() => {
    if (!updatedAt) return
    const id = setInterval(() => setTick((n) => n + 1), 1000)
    return () => clearInterval(id)
  }, [updatedAt])

  const mx = dnsChip(dns?.mx, {
    ok: t('health.ok'),
    missing: t('health.missing'),
    checking: t('health.checking'),
    warn: dns?.mx?.detail,
  })
  const spf = dnsChip(dns?.spf, {
    ok: t('health.ok'),
    missing: t('health.publishTxt'),
    checking: t('health.checking'),
    warn: t('health.publishTxt'),
  })
  const dkim = dnsChip(dns?.dkim, {
    ok: t('health.ok'),
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
    ok: t('health.ok'),
    missing: t('health.publishTxt'),
    checking: t('health.checking'),
    warn: t('health.publishTxt'),
  })

  const stackState =
    !posture?.stack ? 'warn' : stackMissing > 0 ? 'bad' : stackRunning > 0 ? 'ok' : 'warn'
  const stackText = !posture?.stack
    ? t('health.checking')
    : stackMissing > 0
      ? t('health.stackMissing', { n: stackMissing })
      : t('health.stackOk', { n: stackRunning })
  const ipChip = dnsChip(posture?.rbl, {
    ok: t('health.ipClean'),
    missing: t('health.ipCheck'),
    checking: t('health.checking'),
    warn: posture?.rbl?.detail || t('health.ipCheck'),
  })
  const spamChip = dnsChip(
    posture?.antispam ? { state: posture.antispam.state, detail: posture.antispam.detail } : undefined,
    {
      ok: t('health.spamOk'),
      missing: t('health.spamCheck'),
      checking: t('health.checking'),
      warn: posture?.antispam?.detail || t('health.spamCheck'),
    },
  )

  const chips = [
    { id: 'stack', label: 'STACK', title: posture?.stack?.missing?.join(', '), state: stackState, text: stackText },
    { id: 'mx', label: 'MX', title: dns?.mx?.detail, ...mx },
    { id: 'spf', label: 'SPF', title: dns?.spf?.detail, ...spf },
    { id: 'dkim', label: 'DKIM', title: dns?.dkim?.detail, ...dkim },
    { id: 'dmarc', label: 'DMARC', title: dns?.dmarc?.detail, ...dmarc },
    { id: 'ip', label: 'IP', title: posture?.rbl?.detail || posture?.ip, ...ipChip },
    { id: 'spam', label: 'SPAM', title: posture?.antispam?.detail, ...spamChip },
    { id: 'tls', label: 'TLS', state: tlsOk ? 'ok' : 'warn', text: tlsOk ? t('health.ok') : t('health.tlsWarn') },
    { id: 'queue', label: 'QUEUE', state: dead > 0 ? 'bad' : queueN >= 50 ? 'warn' : 'ok', text: String(queueN) },
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
        <span className={`health-systems ${systemsOk ? 'ok' : 'warn'}`}>
          {systemsOk ? t('health.allOk') : dead > 0 ? t('health.dead', { n: dead }) : t('health.attention')}
        </span>
        <span className="health-ago">
          {ago == null ? '—' : ago < 5 ? t('health.justNow') : t('health.updated', { s: ago })}
        </span>
        {onRefresh ? (
          <button type="button" className="health-refresh" onClick={onRefresh} aria-label={t('actions.refresh')}>
            ↻
          </button>
        ) : null}
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
  const [dmarcReports, setDmarcReports] = useState<DmarcReport[] | null>(null)
  const [hostStats, setHostStats] = useState<HostStats | null>(null)
  const [ops, setOps] = useState<OpsStatus | null>(null)
  const [posture, setPosture] = useState<Posture | null>(null)
  const [updatedAt, setUpdatedAt] = useState<number | null>(null)
  const [busy, setBusy] = useState(false)
  const [mbSearch, setMbSearch] = useState('')
  const [mbFilter, setMbFilter] = useState<'all' | 'active' | 'disabled'>('all')
  const [mbDetailTab, setMbDetailTab] = useState<'general' | 'aliases' | 'filters'>('general')
  const [mbSaved, setMbSaved] = useState(false)
  const [settingsSaved, setSettingsSaved] = useState(false)
  const [settingsDirty, setSettingsDirty] = useState(false)

  const authed = Boolean(creds?.token)
  const dnsDomain = selectedDomain || domains[0] || null

  const refreshDash = useCallback(async () => {
    const d = await api<Dashboard>('/api/admin/dashboard', creds!)
    setDash(d)
    setSpark(pushSpark(d.queuePending ?? 0))
    setUpdatedAt(Date.now())
    const q = dnsDomain?.name ? `?domain=${encodeURIComponent(dnsDomain.name)}` : ''
    const settled = await Promise.allSettled([
      api<DnsStatus>(`/api/admin/dns-status${q}`, creds!),
      api<HostStats>('/api/admin/host-stats', creds!),
      api<OpsStatus>('/api/admin/ops', creds!),
      api<DmarcReport[]>(`/api/admin/dmarc-reports${q}`, creds!),
      api<Posture>('/api/admin/posture', creds!),
    ])
    if (settled[0].status === 'fulfilled') setDnsStatus(settled[0].value)
    if (settled[1].status === 'fulfilled') setHostStats(settled[1].value)
    if (settled[2].status === 'fulfilled') setOps(settled[2].value)
    if (settled[3].status === 'fulfilled') setDmarcReports(asList(settled[3].value))
    if (settled[4].status === 'fulfilled') setPosture(settled[4].value)
    return d
  }, [creds, dnsDomain?.name])

  const loadDomainExtras = useCallback(
    async (domain: Domain) => {
      if (!domain?.id) return
      const [mbs, als] = await Promise.all([
        api<Mailbox[]>(`/api/admin/domains/${domain.id}/mailboxes`, creds!),
        api<Alias[]>(`/api/admin/domains/${domain.id}/aliases`, creds!),
      ])
      setMailboxes(asList(mbs))
      setAliases(asList(als))
    },
    [creds],
  )

  const clearSession = useCallback(() => {
    sessionStorage.removeItem('wm_admin')
    setCreds(null)
  }, [])

  const load = useCallback(async () => {
    if (!authed) return
    setError('')
    try {
      const list = asList(await api<Domain[]>('/api/admin/domains', creds!))
      setDomains(list)
      await refreshDash()
      const settingsData = await api<Record<string, unknown>>('/api/admin/settings', creds!)
      setSettings((prev) => (settingsDirty ? prev : settingsData))
      if (tab === 'queue') setQueue(asList(await api<QueueJob[]>('/api/admin/queue', creds!)))
      if (tab === 'quarantine') setQuarantine(asList(await api<QuarantineItem[]>('/api/admin/quarantine', creds!)))
      if (tab === 'audit') setAudit(asList(await api<AuditEntry[]>('/api/admin/audit', creds!)))
      if ((tab === 'mailboxes' || tab === 'domains') && selectedDomain) {
        await loadDomainExtras(selectedDomain)
      }
    } catch (e) {
      if (isUnauthorized(e)) {
        clearSession()
        return
      }
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [authed, creds, tab, selectedDomain, refreshDash, loadDomainExtras, clearSession, settingsDirty])

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
    setBusy(true)
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
    } finally {
      setBusy(false)
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
    setForm((f) => ({
      ...f,
      localPart: '',
      displayName: '',
      password: '',
      quotaMb: '',
      aliasLocal: '',
      aliasMailboxId: '',
    }))
    await loadDomainExtras(sel)
  }

  function selectMailbox(m: Mailbox) {
    setSelectedMailbox(m)
    setMbDetailTab('general')
    setMbSaved(false)
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
    setError('')
    try {
      const created = await api<Domain>('/api/admin/domains', {
        ...creds,
        method: 'POST',
        body: { name: form.domain, catchAll: form.catchAll || '' },
      })
      setForm((f) => ({ ...f, domain: '', catchAll: '' }))
      const list = asList(await api<Domain[]>('/api/admin/domains', creds!))
      setDomains(list)
      const next = list.find((d) => domainId(d) === domainId(created)) || created
      if (next) void selectDomain(next)
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
      const list = asList(await api<Domain[]>('/api/admin/domains', creds!))
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
    setError('')
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
      const list = asList(await api<Domain[]>('/api/admin/domains', creds!))
      setDomains(list)
      const refreshed = list.find((d) => domainId(d) === String(selectedDomain.id))
      if (refreshed) {
        setSelectedDomain({
          ...refreshed,
          id: domainId(refreshed) ?? selectedDomain.id,
          name: domainName(refreshed),
        })
      }
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
      setMbSaved(true)
      window.setTimeout(() => setMbSaved(false), 1600)
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
        const win = window.open(data.url, '_blank', 'noopener,noreferrer')
        if (!win) setError(t('mailboxes.popupBlocked'))
      } else if (data.token) {
        const base = String(settings['admin.webmail_url'] || '').replace(/\/$/, '')
        if (!base) {
          setError(t('mailboxes.webmailUrlMissing'))
          return
        }
        const win = window.open(
          `${base}/login?impersonate=${encodeURIComponent(data.token)}`,
          '_blank',
          'noopener,noreferrer',
        )
        if (!win) setError(t('mailboxes.popupBlocked'))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function createAlias(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    if (!selectedDomain) return
    if (!form.aliasLocal.trim() || !form.aliasMailboxId) {
      setError(t('domains.aliasNeedTarget'))
      return
    }
    const clash = mailboxes.some((m) => m.localPart.toLowerCase() === form.aliasLocal.trim().toLowerCase())
    if (clash) {
      setError(t('domains.aliasConflictsMailbox'))
      return
    }
    setBusy(true)
    setError('')
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

  async function deleteAlias(aliasId: string | number) {
    if (!selectedDomain) return
    if (!window.confirm(t('actions.confirmDelete'))) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/domains/${selectedDomain.id}/aliases/${aliasId}`, {
        ...creds,
        method: 'DELETE',
      })
      await loadDomainExtras(selectedDomain)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function deleteMailbox() {
    if (!selectedDomain || !selectedMailbox) return
    const addr = `${selectedMailbox.localPart}@${selectedDomain.name}`
    if (!window.confirm(t('mailboxes.confirmDelete', { addr }))) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/domains/${selectedDomain.id}/mailboxes/${selectedMailbox.id}`, {
        ...creds,
        method: 'DELETE',
      })
      setSelectedMailbox(null)
      await loadDomainExtras(selectedDomain)
      const list = asList(await api<Domain[]>('/api/admin/domains', creds!))
      setDomains(list)
      const refreshed = list.find((d) => domainId(d) === String(selectedDomain.id))
      if (refreshed) {
        setSelectedDomain({
          ...refreshed,
          id: domainId(refreshed) ?? selectedDomain.id,
          name: domainName(refreshed),
        })
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function deleteDomain() {
    if (!selectedDomain) return
    const name = selectedDomain.name
    if (!window.confirm(t('domains.confirmDelete', { name, n: mailboxes.length }))) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/domains/${selectedDomain.id}`, { ...creds, method: 'DELETE' })
      setSelectedDomain(null)
      setSelectedMailbox(null)
      setMailboxes([])
      setAliases([])
      const list = asList(await api<Domain[]>('/api/admin/domains', creds!))
      setDomains(list)
      if (list[0]) void selectDomain(list[0])
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function retryQueueJob(id: string | number) {
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/queue/${id}/retry`, { ...creds, method: 'POST' })
      setQueue(asList(await api<QueueJob[]>('/api/admin/queue', creds!)))
      await refreshDash()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function deleteQueueJob(id: string | number) {
    if (!window.confirm(t('queue.confirmDelete'))) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/queue/${id}/delete`, { ...creds, method: 'POST' })
      setQueue(asList(await api<QueueJob[]>('/api/admin/queue', creds!)))
      await refreshDash()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function releaseQuarantineItem(id: string | number) {
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/quarantine/${id}/release`, { ...creds, method: 'POST' })
      setQuarantine(asList(await api<QuarantineItem[]>('/api/admin/quarantine', creds!)))
      await refreshDash()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function deleteQuarantineItem(id: string | number) {
    if (!window.confirm(t('quarantine.confirmDelete'))) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/quarantine/${id}/delete`, { ...creds, method: 'POST' })
      setQuarantine(asList(await api<QuarantineItem[]>('/api/admin/quarantine', creds!)))
      await refreshDash()
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function genDkim() {
    if (!selectedDomain) return
    if (selectedDomain.dkimPublic && !window.confirm(t('domains.confirmRotateDkim'))) return
    setBusy(true)
    setError('')
    try {
      await api(`/api/admin/domains/${selectedDomain.id}/dkim`, { ...creds, method: 'POST' })
      const list = asList(await api<Domain[]>('/api/admin/domains', creds!))
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
    setError('')
    setSettingsSaved(false)
    try {
      setSettings(
        await api<Record<string, unknown>>('/api/admin/settings', { ...creds, method: 'PUT', body: settings }),
      )
      setSettingsDirty(false)
      setSettingsSaved(true)
      window.setTimeout(() => setSettingsSaved(false), 2000)
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
      const result = await api<{ settings?: number; domains?: number; mailboxes?: number; aliases?: number }>(
        '/api/admin/backup/restore',
        {
          ...creds,
          method: 'POST',
          body: {
            settings: payload.settings || {},
            domains: payload.domains || [],
          },
        },
      )
      setSettingsDirty(false)
      await load()
      setError('')
      alert(
        t('backup.restored', {
          s: result.settings ?? 0,
          d: result.domains ?? 0,
          m: result.mailboxes ?? 0,
          a: result.aliases ?? 0,
        }),
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const healthy = useMemo(() => {
    const pending = dash?.queuePending ?? 0
    const dead = dash?.queueDead ?? 0
    const stackOk = !posture?.stack || (posture.stack.missing?.length ?? 0) === 0
    const ipOk = !posture?.rbl || posture.rbl.state !== 'bad'
    return dead === 0 && pending < 50 && stackOk && ipOk
  }, [dash, posture])

  const filteredMailboxes = useMemo(() => {
    const q = mbSearch.trim().toLowerCase()
    return mailboxes.filter((m) => {
      if (mbFilter === 'active' && m.enabled === false) return false
      if (mbFilter === 'disabled' && m.enabled !== false) return false
      if (!q) return true
      const hay = `${m.localPart} ${m.displayName || ''}`.toLowerCase()
      return hay.includes(q)
    })
  }, [mailboxes, mbSearch, mbFilter])

  const mailboxLabel = (id: string | number) => {
    const m = mailboxes.find((x) => String(x.id) === String(id))
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
            <button className="login-submit" type="submit" disabled={busy}>
              {busy ? '…' : t('login.submit')}
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
    clearSession()
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

      {tab !== 'overview' ? (
        <HealthStrip
          dash={dash}
          dns={dnsStatus}
          ops={ops}
          posture={posture}
          updatedAt={updatedAt}
          onRefresh={() => void refreshDash().catch(() => {})}
        />
      ) : null}

      <main className="main">
        {error ? <p className="err">{error}</p> : null}

        {tab === 'overview' ? (
          <div className="overview">
            <div>
              <section className="hero">
                <h2>
                  {healthy ? (
                    <>
                      {t('overview.healthyTitleBefore')}
                      <em>{t('overview.healthyTitleEm')}</em>
                    </>
                  ) : (
                    t('overview.attentionTitle')
                  )}
                </h2>
                <p>
                  {healthy
                    ? dnsStatus?.dkim?.state === 'ok'
                      ? t('overview.healthyBodyOk')
                      : t('overview.healthyBody')
                    : t('overview.attentionBody')}
                </p>
              </section>
              <HealthStrip
                dash={dash}
                dns={dnsStatus}
                ops={ops}
                posture={posture}
                updatedAt={updatedAt}
                onRefresh={() => void refreshDash().catch(() => {})}
              />
              <ResourcesPanel host={hostStats} ops={ops} />
              <div className="overview-metrics">
                <div className="metric">
                  <h3>{t('overview.queueTitle')}</h3>
                  <p className="muted spark-label">{t('overview.queueWindow')}</p>
                  <Sparkline samples={spark} />
                  <p className="muted metric-foot">
                    {t('overview.pendingNow')}: <strong>{dash?.queuePending ?? '—'}</strong>
                    {dash?.queueDead ? ` · ${t('overview.deadShort', { n: dash.queueDead })}` : ''}
                  </p>
                </div>
                <div className="metric">
                  <h3>{t('overview.quarantineTitle')}</h3>
                  <p className="big">{dash?.quarantine ?? '—'}</p>
                  <p className="muted metric-foot">{t('overview.quarantineTotal')}</p>
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
                  <div>
                    <span className="muted">{t('overview.rbls')}</span>
                    <strong>
                      {String(
                        posture?.antispam?.rbls?.join(', ') ||
                          settings['antispam.rbls'] ||
                          t('overview.none'),
                      )}
                    </strong>
                  </div>
                </div>
              </div>
            </div>
            <aside className="stack-aside">
              <DeliverabilityCard
                dns={dnsStatus}
                reports={dmarcReports}
                posture={posture}
                onRecheck={() => void refreshDash().catch(() => {})}
              />
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
              <form className="toolbar-form" onSubmit={createDomain}>
                <div className="field">
                  <label htmlFor="new-domain">{t('domains.colDomain')}</label>
                  <input
                    id="new-domain"
                    placeholder={t('domains.placeholder')}
                    value={form.domain}
                    onChange={(e) => setForm({ ...form, domain: e.target.value })}
                    required
                  />
                </div>
                <div className="field">
                  <label htmlFor="new-domain-catchall">{t('domains.catchAll')}</label>
                  <input
                    id="new-domain-catchall"
                    placeholder={t('domains.catchAllPh')}
                    value={form.catchAll}
                    onChange={(e) => setForm({ ...form, catchAll: e.target.value })}
                  />
                </div>
                <button className="primary" type="submit" disabled={busy}>
                  {t('domains.add')}
                </button>
              </form>
            </div>
            <div className="split master-detail">
              <div className="panel list-panel">
                <table>
                  <thead>
                    <tr>
                      <th>{t('domains.colDomain')}</th>
                      <th>{t('domains.colMailboxes')}</th>
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
                          className={String(selectedDomain?.id) === String(id) ? 'selected' : ''}
                          onClick={() => void selectDomain(d)}
                        >
                          <td>
                            <span className="cell-strong">{domainName(d)}</span>
                          </td>
                          <td>{d.mailboxCount ?? '—'}</td>
                          <td>
                            {d.dkimPublic ? (
                              <span className="status-ok">{t('domains.dkimReady')}</span>
                            ) : (
                              <span className="muted">—</span>
                            )}
                          </td>
                          <td>
                            <span className={`badge ${d.enabled === false ? 'off' : 'ok'}`}>
                              {d.enabled === false ? t('domains.off') : t('domains.active')}
                            </span>
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
                    <header className="detail-head">
                      <div>
                        <h3>{selectedDomain.name}</h3>
                        <div className="detail-meta">
                          <span className={`badge ${selectedDomain.enabled === false ? 'off' : 'ok'}`}>
                            {selectedDomain.enabled === false ? t('domains.off') : t('domains.active')}
                          </span>
                          <span className={`meta-chip ${selectedDomain.dkimPublic ? 'ok' : ''}`}>
                            {selectedDomain.dkimPublic ? t('domains.dkimReady') : t('health.noKey')}
                          </span>
                          <span className="meta-chip">{t('domains.mailboxCount', { n: mailboxes.length })}</span>
                        </div>
                      </div>
                      <div className="detail-actions">
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
                    </header>

                    <section className="detail-card">
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
                          <label htmlFor="domain-catchall">{t('domains.catchAll')}</label>
                          <input
                            id="domain-catchall"
                            value={domainEdit.catchAll}
                            onChange={(e) => setDomainEdit({ ...domainEdit, catchAll: e.target.value })}
                            placeholder={t('domains.catchAllPh')}
                          />
                        </div>
                        <div className="field">
                          <label htmlFor="domain-quota">{t('domains.defaultQuotaMb')}</label>
                          <input
                            id="domain-quota"
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
                          <button type="button" className="ghost danger" disabled={busy} onClick={() => void deleteDomain()}>
                            {t('domains.delete')}
                          </button>
                        </div>
                      </form>
                    </section>

                    <section className="detail-card">
                      <h4>{t('domains.selector')}</h4>
                      <code className="selector-chip">{selectedDomain.dkimSelector || 'wernan'}</code>
                      <p className="muted foot-note">{t('domains.selectorHint')}</p>
                    </section>

                    <section className="detail-card">
                      <h4>{t('domains.quickAdd')}</h4>
                      <form className="form-stack" onSubmit={createMailbox}>
                        <div className="field-grid">
                          <div className="field">
                            <label htmlFor="quick-local">{t('domains.localPart')}</label>
                            <input
                              id="quick-local"
                              value={form.localPart}
                              onChange={(e) => setForm({ ...form, localPart: e.target.value })}
                              autoComplete="off"
                              required
                            />
                          </div>
                          <div className="field">
                            <label htmlFor="quick-name">{t('domains.displayName')}</label>
                            <input
                              id="quick-name"
                              value={form.displayName}
                              onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                              autoComplete="off"
                            />
                          </div>
                          <div className="field">
                            <label htmlFor="quick-pass">{t('domains.password')}</label>
                            <input
                              id="quick-pass"
                              type="password"
                              value={form.password}
                              onChange={(e) => setForm({ ...form, password: e.target.value })}
                              autoComplete="new-password"
                              required
                            />
                          </div>
                          <div className="field">
                            <label htmlFor="quick-quota">{t('mailboxes.quotaMb')}</label>
                            <input
                              id="quick-quota"
                              type="number"
                              min="0"
                              step="1"
                              value={form.quotaMb}
                              onChange={(e) => setForm({ ...form, quotaMb: e.target.value })}
                              placeholder={t('mailboxes.quotaDefault')}
                            />
                          </div>
                        </div>
                        <div className="detail-actions">
                          <button className="primary" type="submit" disabled={busy}>
                            {t('domains.addMailbox')}
                          </button>
                        </div>
                      </form>
                    </section>

                    <section className="detail-card">
                      <h4>{t('domains.aliases')}</h4>
                      <form className="row alias-add" onSubmit={createAlias}>
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
                        <ul className="alias-list">
                          {aliases.map((a) => (
                            <li key={a.id}>
                              <span>
                                {a.localPart}@{selectedDomain.name}
                                <span className="muted"> → {mailboxLabel(a.mailboxId)}</span>
                              </span>
                              <button
                                type="button"
                                className="ghost alias-del"
                                disabled={busy}
                                onClick={() => void deleteAlias(a.id)}
                                aria-label={t('actions.delete')}
                              >
                                {t('actions.delete')}
                              </button>
                            </li>
                          ))}
                        </ul>
                      ) : (
                        <p className="muted">{t('domains.noAliases')}</p>
                      )}
                    </section>
                  </>
                ) : (
                  <div className="empty-state">
                    <p className="empty-title">{t('domains.select')}</p>
                    <p className="muted">{t('domains.selectHint')}</p>
                  </div>
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
            <div className="list-toolbar panel" style={{ marginBottom: '0.45rem' }}>
              <Select
                aria-label={t('mailboxes.selectDomain')}
                placeholder={t('mailboxes.selectDomain')}
                value={selectedDomain?.id != null ? String(selectedDomain.id) : ''}
                onChange={(v) => {
                  const d = domains.find((x) => String(domainId(x)) === String(v))
                  if (d) void selectDomain(d)
                }}
                options={domains.map((d) => ({ value: String(domainId(d)), label: domainName(d) }))}
              />
              <input
                className="mb-search"
                type="search"
                placeholder={t('mailboxes.search')}
                value={mbSearch}
                onChange={(e) => setMbSearch(e.target.value)}
                disabled={!selectedDomain}
              />
              <Select
                aria-label={t('mailboxes.filter')}
                value={mbFilter}
                onChange={(v) => setMbFilter(v as 'all' | 'active' | 'disabled')}
                options={[
                  { value: 'all', label: t('mailboxes.filterAll') },
                  { value: 'active', label: t('mailboxes.filterActive') },
                  { value: 'disabled', label: t('mailboxes.filterDisabled') },
                ]}
              />
              <span className="muted mb-count">
                {selectedDomain
                  ? t('mailboxes.count', { n: filteredMailboxes.length, total: mailboxes.length })
                  : null}
              </span>
              <button type="button" className="ghost" onClick={() => void load()} disabled={!selectedDomain}>
                {t('actions.refresh')}
              </button>
            </div>
            {selectedDomain ? (
              <div className="split master-detail">
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
                      {filteredMailboxes.map((m) => (
                        <tr
                          key={m.id}
                          className={String(selectedMailbox?.id) === String(m.id) ? 'selected' : ''}
                          onClick={() => selectMailbox(m)}
                        >
                          <td>
                            {m.localPart}@{selectedDomain.name}
                          </td>
                          <td>
                            <QuotaBar used={m.usedBytes} quota={m.quotaBytes} />
                          </td>
                          <td>
                            <span className={`badge ${m.enabled === false ? 'off' : ''}`}>
                              {m.enabled === false ? t('domains.off') : t('domains.active')}
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  {!filteredMailboxes.length ? <p className="empty">{t('mailboxes.empty')}</p> : null}
                </div>
                <div className="panel detail-panel">
                  {selectedMailbox ? (
                    <>
                      <header className="detail-head">
                        <div>
                          <h3>
                            {selectedMailbox.localPart}@{selectedDomain.name}
                          </h3>
                          <div className="detail-meta">
                            <span className={`badge ${selectedMailbox.enabled === false ? 'off' : 'ok'}`}>
                              {selectedMailbox.enabled === false ? t('domains.off') : t('domains.active')}
                            </span>
                            <span className="meta-chip">
                              {t('mailboxes.created')}: {formatWhen(selectedMailbox.createdAt)}
                            </span>
                          </div>
                        </div>
                        <div className="detail-actions">
                          <button
                            type="button"
                            className="ghost"
                            disabled={busy || String(settings['admin.superuser_enabled']).toLowerCase() !== 'true'}
                            title={
                              String(settings['admin.superuser_enabled']).toLowerCase() !== 'true'
                                ? t('mailboxes.superuserOff')
                                : undefined
                            }
                            onClick={() => void openAsUser()}
                          >
                            {t('mailboxes.openAsUser')}
                          </button>
                          <button type="button" className="ghost danger" disabled={busy} onClick={() => void deleteMailbox()}>
                            {t('mailboxes.delete')}
                          </button>
                        </div>
                      </header>
                      <div className="detail-tabs" role="tablist">
                        <button
                          type="button"
                          role="tab"
                          aria-selected={mbDetailTab === 'general'}
                          className={mbDetailTab === 'general' ? 'active' : ''}
                          onClick={() => setMbDetailTab('general')}
                        >
                          {t('mailboxes.tabGeneral')}
                        </button>
                        <button
                          type="button"
                          role="tab"
                          aria-selected={mbDetailTab === 'aliases'}
                          className={mbDetailTab === 'aliases' ? 'active' : ''}
                          onClick={() => setMbDetailTab('aliases')}
                        >
                          {t('mailboxes.tabAliases', {
                            n: aliases.filter((a) => String(a.mailboxId) === String(selectedMailbox.id)).length,
                          })}
                        </button>
                        <button
                          type="button"
                          role="tab"
                          aria-selected={mbDetailTab === 'filters'}
                          className={mbDetailTab === 'filters' ? 'active' : ''}
                          onClick={() => setMbDetailTab('filters')}
                        >
                          {t('mailboxes.tabFilters')}
                        </button>
                      </div>
                      {mbDetailTab === 'general' ? (
                        <>
                          <section className="detail-card">
                            <h4>{t('mailboxes.details')}</h4>
                            <form className="form-stack" onSubmit={saveMailboxSettings}>
                              <div className="field">
                                <label htmlFor="mb-name">{t('domains.displayName')}</label>
                                <input
                                  id="mb-name"
                                  value={mbEdit.displayName}
                                  onChange={(e) => setMbEdit({ ...mbEdit, displayName: e.target.value })}
                                />
                              </div>
                              <div className="field">
                                <label htmlFor="mb-quota">{t('mailboxes.quotaMb')}</label>
                                <input
                                  id="mb-quota"
                                  type="number"
                                  min="0"
                                  step="1"
                                  value={mbEdit.quotaMb}
                                  onChange={(e) => setMbEdit({ ...mbEdit, quotaMb: e.target.value })}
                                  placeholder={t('mailboxes.unlimited')}
                                />
                                <div className="quota-detail">
                                  <QuotaBar used={selectedMailbox.usedBytes} quota={selectedMailbox.quotaBytes} />
                                </div>
                                <p className="muted foot-note">{t('mailboxes.quotaHint')}</p>
                              </div>
                              <div className="field">
                                <label htmlFor="mb-pass">{t('mailboxes.newPassword')}</label>
                                <input
                                  id="mb-pass"
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
                                {mbSaved ? <span className="save-flash">{t('mailboxes.saved')}</span> : null}
                              </div>
                            </form>
                          </section>
                          <section className="detail-card">
                            <h4>{t('mailboxes.addAnother')}</h4>
                            <form className="form-stack" onSubmit={createMailbox}>
                              <div className="field-grid">
                                <div className="field">
                                  <label htmlFor="mb-quick-local">{t('domains.localPart')}</label>
                                  <input
                                    id="mb-quick-local"
                                    value={form.localPart}
                                    onChange={(e) => setForm({ ...form, localPart: e.target.value })}
                                    required
                                  />
                                </div>
                                <div className="field">
                                  <label htmlFor="mb-quick-name">{t('domains.displayName')}</label>
                                  <input
                                    id="mb-quick-name"
                                    value={form.displayName}
                                    onChange={(e) => setForm({ ...form, displayName: e.target.value })}
                                  />
                                </div>
                                <div className="field">
                                  <label htmlFor="mb-quick-pass">{t('domains.password')}</label>
                                  <input
                                    id="mb-quick-pass"
                                    type="password"
                                    value={form.password}
                                    onChange={(e) => setForm({ ...form, password: e.target.value })}
                                    required
                                  />
                                </div>
                                <div className="field">
                                  <label htmlFor="mb-quick-quota">{t('mailboxes.quotaMb')}</label>
                                  <input
                                    id="mb-quick-quota"
                                    type="number"
                                    min="0"
                                    value={form.quotaMb}
                                    onChange={(e) => setForm({ ...form, quotaMb: e.target.value })}
                                    placeholder={t('mailboxes.quotaDefault')}
                                  />
                                </div>
                              </div>
                              <div className="detail-actions">
                                <button className="primary" type="submit" disabled={busy}>
                                  {t('actions.add')}
                                </button>
                              </div>
                            </form>
                          </section>
                        </>
                      ) : mbDetailTab === 'aliases' ? (
                        <section className="detail-card">
                          <h4>{t('domains.aliases')}</h4>
                          <form className="row alias-add" onSubmit={createAlias}>
                            <input
                              placeholder={t('domains.aliasLocal')}
                              value={form.aliasLocal}
                              onChange={(e) =>
                                setForm({
                                  ...form,
                                  aliasLocal: e.target.value,
                                  aliasMailboxId: String(selectedMailbox.id),
                                })
                              }
                              required
                            />
                            <button className="primary" type="submit" disabled={busy}>
                              {t('domains.addAlias')}
                            </button>
                          </form>
                          {aliases.filter((a) => String(a.mailboxId) === String(selectedMailbox.id)).length ? (
                            <ul className="alias-list">
                              {aliases
                                .filter((a) => String(a.mailboxId) === String(selectedMailbox.id))
                                .map((a) => (
                                  <li key={a.id}>
                                    <span>
                                      {a.localPart}@{selectedDomain.name}
                                    </span>
                                    <button
                                      type="button"
                                      className="ghost alias-del"
                                      disabled={busy}
                                      onClick={() => void deleteAlias(a.id)}
                                      aria-label={t('actions.delete')}
                                    >
                                      {t('actions.delete')}
                                    </button>
                                  </li>
                                ))}
                            </ul>
                          ) : (
                            <p className="empty soft">{t('domains.noAliases')}</p>
                          )}
                        </section>
                      ) : (
                        <section className="detail-card filter-section">
                          <MailboxFilters
                            key={String(selectedMailbox.id)}
                            mailboxId={selectedMailbox.id}
                            creds={creds!}
                          />
                        </section>
                      )}
                    </>
                  ) : (
                    <div className="empty-state">
                      <p className="empty-title">{t('mailboxes.select')}</p>
                      <p className="muted">{t('mailboxes.selectHint')}</p>
                    </div>
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
              <div className="page-head-meta">
                <span className="muted">
                  {queue.length
                    ? t('queue.count', {
                        n: queue.length,
                        dead: queue.filter((j) => j.attempts >= j.maxAttempts).length,
                      })
                    : null}
                  {queue.length >= 100 ? ` · ${t('list.capped', { n: 100 })}` : ''}
                </span>
                <button type="button" className="ghost" onClick={() => void load()} disabled={busy}>
                  {t('actions.refresh')}
                </button>
              </div>
            </div>
            <div className="panel list-panel">
              <table>
                <thead>
                  <tr>
                    <th>{t('queue.colKind')}</th>
                    <th>{t('queue.colStatus')}</th>
                    <th>{t('queue.colAttempts')}</th>
                    <th>{t('queue.colError')}</th>
                    <th>{t('queue.colWhen')}</th>
                    <th>{t('queue.colActions')}</th>
                  </tr>
                </thead>
                <tbody>
                  {queue.map((j) => {
                    const dead = j.attempts >= j.maxAttempts
                    const target = queuePayloadSummary(j.payloadJson)
                    return (
                      <tr key={j.id} className={dead ? 'row-dead' : ''}>
                        <td>
                          <div className="cell-stack">
                            <strong>{queueKindLabel(j.kind, t)}</strong>
                            {target ? <span className="muted">{target}</span> : null}
                            <span className="muted mono-id">#{j.id}</span>
                          </div>
                        </td>
                        <td>
                          <span className={`badge ${dead ? 'off' : ''}`}>
                            {dead ? t('queue.dead') : t('queue.pending')}
                          </span>
                        </td>
                        <td>
                          {j.attempts}/{j.maxAttempts}
                        </td>
                        <td className="cell-error">{j.lastError || '—'}</td>
                        <td className="muted">{formatWhen(j.updatedAt || j.createdAt || j.nextAt)}</td>
                        <td className="row">
                          <button
                            type="button"
                            className="ghost"
                            disabled={busy}
                            onClick={() => void retryQueueJob(j.id)}
                          >
                            {t('queue.retry')}
                          </button>
                          <button
                            type="button"
                            className="ghost"
                            disabled={busy}
                            onClick={() => void deleteQueueJob(j.id)}
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
              <div className="page-head-meta">
                <span className="muted">
                  {quarantine.length ? t('quarantine.count', { n: quarantine.length }) : null}
                  {quarantine.length >= 100 ? ` · ${t('list.capped', { n: 100 })}` : ''}
                </span>
                <button type="button" className="ghost" onClick={() => void load()} disabled={busy}>
                  {t('actions.refresh')}
                </button>
              </div>
            </div>
            <div className="panel list-panel">
              <table>
                <thead>
                  <tr>
                    <th>{t('quarantine.colFrom')}</th>
                    <th>{t('quarantine.colSubject')}</th>
                    <th>{t('quarantine.colMailbox')}</th>
                    <th>{t('quarantine.colScore')}</th>
                    <th>{t('quarantine.colReasons')}</th>
                    <th>{t('quarantine.colWhen')}</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {quarantine.map((q) => {
                    const verdict = parseVerdict(q.verdictJson)
                    const score = q.score ?? verdict.score
                    return (
                      <tr key={q.id}>
                        <td>{q.fromAddr || q.from || '—'}</td>
                        <td>{q.subject || t('quarantine.noSubject')}</td>
                        <td className="muted">{q.mailboxAddr || (q.mailboxId != null ? `#${q.mailboxId}` : '—')}</td>
                        <td>
                          {score != null ? (
                            <span className="score-pill">{Number(score).toFixed(1)}</span>
                          ) : (
                            '—'
                          )}
                        </td>
                        <td>
                          {verdict.reasons.length ? (
                            <ul className="reason-list">
                              {verdict.reasons.slice(0, 4).map((r, i) => (
                                <li key={i}>{r}</li>
                              ))}
                            </ul>
                          ) : (
                            <span className="muted">—</span>
                          )}
                        </td>
                        <td className="muted">{formatWhen(q.createdAt)}</td>
                        <td className="row">
                          <button
                            type="button"
                            className="ghost"
                            disabled={busy}
                            onClick={() => void releaseQuarantineItem(q.id)}
                          >
                            {t('actions.release')}
                          </button>
                          <button
                            type="button"
                            className="ghost"
                            disabled={busy}
                            onClick={() => void deleteQuarantineItem(q.id)}
                          >
                            {t('actions.delete')}
                          </button>
                        </td>
                      </tr>
                    )
                  })}
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
                      const label = settingKeyLabel(t, field.key)
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
                                onChange={(e) => {
                                  setSettingsDirty(true)
                                  setSettings({ ...settings, [field.key]: e.target.checked ? 'true' : 'false' })
                                }}
                              />
                              <span>{boolOn ? t('overview.on') : t('overview.off')}</span>
                            </label>
                          ) : field.type === 'textarea' ? (
                            <textarea
                              id={`set-${field.key}`}
                              rows={3}
                              value={value}
                              onChange={(e) => {
                                setSettingsDirty(true)
                                setSettings({ ...settings, [field.key]: e.target.value })
                              }}
                            />
                          ) : (
                            <input
                              id={`set-${field.key}`}
                              type={field.type === 'number' ? 'number' : 'text'}
                              value={value}
                              onChange={(e) => {
                                setSettingsDirty(true)
                                setSettings({ ...settings, [field.key]: e.target.value })
                              }}
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
                {settingsSaved ? <span className="save-flash">{t('settings.saved')}</span> : null}
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
              <div className="page-head-meta">
                <span className="muted">
                  {audit.length ? t('audit.count', { n: audit.length }) : null}
                  {audit.length >= 200 ? ` · ${t('list.capped', { n: 200 })}` : ''}
                </span>
                <button type="button" className="ghost" onClick={() => void load()} disabled={busy}>
                  {t('actions.refresh')}
                </button>
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
                    <th>{t('audit.colDetail')}</th>
                  </tr>
                </thead>
                <tbody>
                  {audit.map((a) => (
                    <tr key={a.id ?? `${a.at}-${a.action}-${a.target}`}>
                      <td className="muted">{formatWhen(a.createdAt || a.at)}</td>
                      <td>{a.actor || '—'}</td>
                      <td>{a.action || '—'}</td>
                      <td>{a.target || '—'}</td>
                      <td className="cell-error">{a.detail || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!audit.length ? <p className="empty">{t('audit.empty')}</p> : null}
            </div>
          </>
        ) : null}
      </main>

      <DNSDrawer
        open={dnsOpen}
        onClose={() => setDnsOpen(false)}
        domain={dnsDomain}
        publicIP={posture?.ip}
      />
    </div>
  )
}
