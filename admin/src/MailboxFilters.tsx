import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { api, asList } from './api'
import { Select } from './Select'
import type { AdminCreds, MailFilter } from './types'

type DraftFilter = MailFilter & { key: string }

let nextKey = 0

function draft(rule?: Partial<MailFilter>): DraftFilter {
  nextKey += 1
  return {
    key: `filter-${nextKey}`,
    enabled: rule?.enabled ?? true,
    priority: rule?.priority ?? 0,
    matchField: rule?.matchField ?? 'from',
    matchOp: rule?.matchOp ?? 'contains',
    matchValue: rule?.matchValue ?? '',
    action: rule?.action ?? 'fileinto',
    actionArg: rule?.actionArg ?? 'Inbox',
    id: rule?.id,
    mailboxId: rule?.mailboxId,
  }
}

export function MailboxFilters({
  mailboxId,
  creds,
}: {
  mailboxId: string | number
  creds: AdminCreds
}) {
  const { t } = useTranslation()
  const [rules, setRules] = useState<DraftFilter[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    let active = true
    setLoading(true)
    setError('')
    void api<MailFilter[]>(`/api/admin/mailboxes/${mailboxId}/filters`, creds)
      .then((data) => {
        if (active) setRules(asList(data).map((rule) => draft(rule)))
      })
      .catch((err: unknown) => {
        if (active) setError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [mailboxId, creds])

  function update(index: number, patch: Partial<MailFilter>) {
    setSaved(false)
    setRules((current) => current.map((rule, i) => (i === index ? { ...rule, ...patch } : rule)))
  }

  function move(index: number, delta: number) {
    const target = index + delta
    if (target < 0 || target >= rules.length) return
    setSaved(false)
    setRules((current) => {
      const next = [...current]
      const [item] = next.splice(index, 1)
      next.splice(target, 0, item!)
      return next
    })
  }

  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault()
    setSaving(true)
    setSaved(false)
    setError('')
    try {
      const payload: MailFilter[] = rules.map(({ key: _key, ...rule }, priority) => ({
        ...rule,
        priority,
        actionArg: rule.action === 'fileinto' ? rule.actionArg.trim() : '',
        matchValue: rule.matchValue.trim(),
      }))
      const stored = await api<MailFilter[]>(`/api/admin/mailboxes/${mailboxId}/filters`, {
        ...creds,
        method: 'PUT',
        body: payload,
      })
      setRules(asList(stored).map((rule) => draft(rule)))
      setSaved(true)
      window.setTimeout(() => setSaved(false), 1600)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <p className="muted">{t('filters.loading')}</p>

  return (
    <form className="filter-editor" onSubmit={save}>
      <div className="filter-intro">
        <div>
          <h4>{t('filters.title')}</h4>
          <p className="muted">{t('filters.hint')}</p>
        </div>
        <button
          type="button"
          className="ghost"
          disabled={saving || rules.length >= 100}
          onClick={() => setRules((current) => [...current, draft({ priority: current.length })])}
        >
          {t('filters.add')}
        </button>
      </div>

      {error ? <p className="filter-error" role="alert">{error}</p> : null}
      {!rules.length ? <p className="empty filter-empty">{t('filters.empty')}</p> : null}

      <div className="filter-list">
        {rules.map((rule, index) => (
          <fieldset className={`filter-rule${rule.enabled ? '' : ' disabled'}`} key={rule.key}>
            <legend>{t('filters.rule', { n: index + 1 })}</legend>
            <div className="filter-rule-head">
              <label className="check-row">
                <input
                  type="checkbox"
                  checked={rule.enabled}
                  onChange={(e) => update(index, { enabled: e.target.checked })}
                />
                <span>{t('filters.enabled')}</span>
              </label>
              <div className="filter-order">
                <button
                  type="button"
                  className="ghost"
                  aria-label={t('filters.moveUp')}
                  title={t('filters.moveUp')}
                  disabled={saving || index === 0}
                  onClick={() => move(index, -1)}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="ghost"
                  aria-label={t('filters.moveDown')}
                  title={t('filters.moveDown')}
                  disabled={saving || index === rules.length - 1}
                  onClick={() => move(index, 1)}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="ghost danger"
                  disabled={saving}
                  onClick={() => {
                    setSaved(false)
                    setRules((current) => current.filter((_, i) => i !== index))
                  }}
                >
                  {t('actions.delete')}
                </button>
              </div>
            </div>

            <div className="filter-fields">
              <div className="field">
                <label>{t('filters.field')}</label>
                <Select
                  value={rule.matchField}
                  onChange={(value) => update(index, { matchField: value })}
                  options={[
                    { value: 'from', label: t('filters.fields.from') },
                    { value: 'to', label: t('filters.fields.to') },
                    { value: 'subject', label: t('filters.fields.subject') },
                  ]}
                />
              </div>
              <div className="field">
                <label>{t('filters.operator')}</label>
                <Select
                  value={rule.matchOp}
                  onChange={(value) => update(index, { matchOp: value })}
                  options={[
                    { value: 'contains', label: t('filters.operators.contains') },
                    { value: 'equals', label: t('filters.operators.equals') },
                  ]}
                />
              </div>
              <div className="field filter-value">
                <label htmlFor={`filter-value-${rule.key}`}>{t('filters.value')}</label>
                <input
                  id={`filter-value-${rule.key}`}
                  value={rule.matchValue}
                  onChange={(e) => update(index, { matchValue: e.target.value })}
                  required
                />
              </div>
              <div className="field">
                <label>{t('filters.action')}</label>
                <Select
                  value={rule.action}
                  onChange={(value) => update(index, { action: value })}
                  options={[
                    { value: 'fileinto', label: t('filters.actions.fileinto') },
                    { value: 'reject', label: t('filters.actions.reject') },
                    { value: 'flag_spam', label: t('filters.actions.flagSpam') },
                  ]}
                />
              </div>
              {rule.action === 'fileinto' ? (
                <div className="field filter-folder">
                  <label htmlFor={`filter-folder-${rule.key}`}>{t('filters.folder')}</label>
                  <input
                    id={`filter-folder-${rule.key}`}
                    value={rule.actionArg}
                    onChange={(e) => update(index, { actionArg: e.target.value })}
                    pattern="[^/\\]+"
                    title={t('filters.folderHint')}
                    required
                  />
                </div>
              ) : null}
            </div>
          </fieldset>
        ))}
      </div>

      <div className="detail-actions filter-save">
        <button className="primary" type="submit" disabled={saving}>
          {saving ? t('filters.saving') : t('filters.save')}
        </button>
        {saved ? <span className="save-flash">{t('filters.saved')}</span> : null}
      </div>
    </form>
  )
}
