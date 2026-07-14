import { useCallback, useEffect, useMemo, useState } from 'react'

const TABS = [
  ['dashboard', 'Dashboard'],
  ['domains', 'Domains'],
  ['queue', 'Queue'],
  ['quarantine', 'Quarantine'],
  ['settings', 'Settings'],
  ['audit', 'Audit'],
]

function authHeader(user, pass) {
  return 'Basic ' + btoa(`${user}:${pass}`)
}

async function api(path, { user, pass, method = 'GET', body } = {}) {
  const res = await fetch(path, {
    method,
    headers: {
      Authorization: authHeader(user, pass),
      ...(body ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  })
  const json = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(json?.error?.message || json?.error?.code || res.statusText)
  }
  return json.data
}

export function App() {
  const [creds, setCreds] = useState(() => {
    try {
      return JSON.parse(sessionStorage.getItem('wm_admin') || 'null')
    } catch {
      return null
    }
  })
  const [tab, setTab] = useState('dashboard')
  const [error, setError] = useState('')
  const [dash, setDash] = useState(null)
  const [domains, setDomains] = useState([])
  const [mailboxes, setMailboxes] = useState([])
  const [selectedDomain, setSelectedDomain] = useState(null)
  const [queue, setQueue] = useState([])
  const [quarantine, setQuarantine] = useState([])
  const [settings, setSettings] = useState({})
  const [audit, setAudit] = useState([])
  const [dkimInfo, setDkimInfo] = useState(null)
  const [form, setForm] = useState({ domain: '', localPart: '', password: '' })
  const [login, setLogin] = useState({ username: 'admin', password: '' })

  const authed = Boolean(creds?.user && creds?.pass)

  const load = useCallback(async () => {
    if (!authed) return
    setError('')
    try {
      if (tab === 'dashboard') setDash(await api('/api/admin/dashboard', creds))
      if (tab === 'domains') setDomains(await api('/api/admin/domains', creds))
      if (tab === 'queue') setQueue(await api('/api/admin/queue', creds))
      if (tab === 'quarantine') setQuarantine(await api('/api/admin/quarantine', creds))
      if (tab === 'settings') setSettings(await api('/api/admin/settings', creds))
      if (tab === 'audit') setAudit(await api('/api/admin/audit', creds))
    } catch (e) {
      setError(e.message)
    }
  }, [authed, creds, tab])

  useEffect(() => {
    void load()
  }, [load])

  async function doLogin(e) {
    e.preventDefault()
    setError('')
    try {
      await api('/api/admin/login', {
        method: 'POST',
        body: login,
        user: login.username,
        pass: login.password,
      })
      const next = { user: login.username, pass: login.password }
      sessionStorage.setItem('wm_admin', JSON.stringify(next))
      setCreds(next)
    } catch (err) {
      setError(err.message)
    }
  }

  async function createDomain(e) {
    e.preventDefault()
    await api('/api/admin/domains', { ...creds, method: 'POST', body: { name: form.domain } })
    setForm((f) => ({ ...f, domain: '' }))
    setDomains(await api('/api/admin/domains', creds))
  }

  async function openDomain(d) {
    setSelectedDomain(d)
    setMailboxes(await api(`/api/admin/domains/${d.id}/mailboxes`, creds))
    setDkimInfo(null)
  }

  async function createMailbox(e) {
    e.preventDefault()
    if (!selectedDomain) return
    await api(`/api/admin/domains/${selectedDomain.id}/mailboxes`, {
      ...creds,
      method: 'POST',
      body: { localPart: form.localPart, password: form.password },
    })
    setForm((f) => ({ ...f, localPart: '', password: '' }))
    setMailboxes(await api(`/api/admin/domains/${selectedDomain.id}/mailboxes`, creds))
  }

  async function genDkim() {
    if (!selectedDomain) return
    const info = await api(`/api/admin/domains/${selectedDomain.id}/dkim`, { ...creds, method: 'POST' })
    setDkimInfo(info)
  }

  async function saveSettings(e) {
    e.preventDefault()
    const next = await api('/api/admin/settings', { ...creds, method: 'PUT', body: settings })
    setSettings(next)
  }

  const title = useMemo(() => TABS.find(([id]) => id === tab)?.[1] || 'Admin', [tab])

  if (!authed) {
    return (
      <div className="app login panel">
        <h1 className="brand">Wernanmail</h1>
        <p className="sub">Admin console</p>
        <form onSubmit={doLogin}>
          <div className="row">
            <input
              placeholder="Username"
              value={login.username}
              onChange={(e) => setLogin({ ...login, username: e.target.value })}
            />
          </div>
          <div className="row">
            <input
              type="password"
              placeholder="Password"
              value={login.password}
              onChange={(e) => setLogin({ ...login, password: e.target.value })}
            />
          </div>
          {error ? <p className="err">{error}</p> : null}
          <button className="primary" type="submit">
            Sign in
          </button>
        </form>
      </div>
    )
  }

  return (
    <div className="app">
      <h1 className="brand">Wernanmail</h1>
      <p className="sub">Mail server admin — {title}</p>
      <nav className="nav">
        {TABS.map(([id, label]) => (
          <button key={id} type="button" className={tab === id ? 'active' : ''} onClick={() => setTab(id)}>
            {label}
          </button>
        ))}
        <button
          type="button"
          className="ghost"
          onClick={() => {
            sessionStorage.removeItem('wm_admin')
            setCreds(null)
          }}
        >
          Log out
        </button>
      </nav>
      {error ? <p className="err">{error}</p> : null}

      {tab === 'dashboard' && dash ? (
        <div className="panel grid">
          <div className="stat">
            <strong>{dash.queuePending}</strong>
            Queue pending
          </div>
          <div className="stat">
            <strong>{dash.queueDead}</strong>
            Dead letters
          </div>
          <div className="stat">
            <strong>{dash.quarantine}</strong>
            Quarantine
          </div>
          <div className="stat">
            <strong>{dash.domains}</strong>
            Domains
          </div>
        </div>
      ) : null}

      {tab === 'domains' ? (
        <div className="panel">
          <form className="row" onSubmit={createDomain}>
            <input
              placeholder="example.com"
              value={form.domain}
              onChange={(e) => setForm({ ...form, domain: e.target.value })}
              required
            />
            <button className="primary" type="submit">
              Add domain
            </button>
          </form>
          <table>
            <thead>
              <tr>
                <th>Domain</th>
                <th>DKIM</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {domains.map((d) => (
                <tr key={d.ID || d.id}>
                  <td>{d.Name || d.name}</td>
                  <td>{(d.DKIMPublic || d.dkimPublic) ? 'yes' : '—'}</td>
                  <td>
                    <button type="button" className="ghost" onClick={() => openDomain({ id: d.ID || d.id, name: d.Name || d.name })}>
                      Open
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {selectedDomain ? (
            <div style={{ marginTop: '1.25rem' }}>
              <h3>{selectedDomain.name}</h3>
              <div className="row">
                <button type="button" className="primary" onClick={genDkim}>
                  Generate DKIM
                </button>
              </div>
              {dkimInfo ? (
                <pre className="dns">
                  {dkimInfo.dnsName} TXT{'\n'}
                  {dkimInfo.dnsValue}
                </pre>
              ) : null}
              <form className="row" onSubmit={createMailbox}>
                <input
                  placeholder="local part"
                  value={form.localPart}
                  onChange={(e) => setForm({ ...form, localPart: e.target.value })}
                  required
                />
                <input
                  type="password"
                  placeholder="password"
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  required
                />
                <button className="primary" type="submit">
                  Add mailbox
                </button>
              </form>
              <table>
                <thead>
                  <tr>
                    <th>Mailbox</th>
                    <th>Quota</th>
                  </tr>
                </thead>
                <tbody>
                  {mailboxes.map((m) => (
                    <tr key={m.id}>
                      <td>
                        {m.localPart}@{selectedDomain.name}
                      </td>
                      <td>{m.quotaBytes || '∞'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      ) : null}

      {tab === 'queue' ? (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Kind</th>
                <th>Attempts</th>
                <th>Error</th>
              </tr>
            </thead>
            <tbody>
              {queue.map((j) => (
                <tr key={j.ID || j.id}>
                  <td>{j.ID || j.id}</td>
                  <td>{j.Kind || j.kind}</td>
                  <td>
                    {j.Attempts || j.attempts}/{j.MaxAttempts || j.maxAttempts}
                  </td>
                  <td>{j.LastError || j.lastError || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {tab === 'quarantine' ? (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>From</th>
                <th>Subject</th>
                <th>Verdict</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {quarantine.map((q) => (
                <tr key={q.ID || q.id}>
                  <td>{q.FromAddr || q.fromAddr}</td>
                  <td>{q.Subject || q.subject}</td>
                  <td>
                    <code style={{ fontSize: '0.75rem' }}>{q.VerdictJSON || q.verdictJSON}</code>
                  </td>
                  <td className="row">
                    <button
                      type="button"
                      className="ghost"
                      onClick={async () => {
                        await api(`/api/admin/quarantine/${q.ID || q.id}/release`, { ...creds, method: 'POST' })
                        setQuarantine(await api('/api/admin/quarantine', creds))
                      }}
                    >
                      Release
                    </button>
                    <button
                      type="button"
                      className="ghost"
                      onClick={async () => {
                        await api(`/api/admin/quarantine/${q.ID || q.id}/delete`, { ...creds, method: 'POST' })
                        setQuarantine(await api('/api/admin/quarantine', creds))
                      }}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {tab === 'settings' ? (
        <form className="panel" onSubmit={saveSettings}>
          {Object.keys(settings)
            .sort()
            .map((key) => (
              <div className="row" key={key}>
                <label style={{ minWidth: 220 }}>{key}</label>
                <input
                  style={{ flex: 1 }}
                  value={settings[key] ?? ''}
                  onChange={(e) => setSettings({ ...settings, [key]: e.target.value })}
                />
              </div>
            ))}
          <button className="primary" type="submit">
            Save settings
          </button>
        </form>
      ) : null}

      {tab === 'audit' ? (
        <div className="panel">
          <table>
            <thead>
              <tr>
                <th>When</th>
                <th>Actor</th>
                <th>Action</th>
                <th>Target</th>
              </tr>
            </thead>
            <tbody>
              {audit.map((a) => (
                <tr key={a.ID || a.id}>
                  <td>{a.CreatedAt || a.createdAt}</td>
                  <td>{a.Actor || a.actor}</td>
                  <td>{a.Action || a.action}</td>
                  <td>{a.Target || a.target}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </div>
  )
}
