import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { ApiError, sendMessage } from '../../api/client'
import styles from './ComposeDialog.module.css'

export type ComposeDraft = {
  to?: string
  cc?: string
  subject?: string
  body?: string
}

type ComposeDialogProps = {
  open: boolean
  draft?: ComposeDraft | null
  onClose: () => void
  onSent?: () => void
}

function splitAddresses(raw: string): string[] {
  return raw
    .split(/[,;\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

export function ComposeDialog({ open, draft, onClose, onSent }: ComposeDialogProps) {
  const { t } = useTranslation()
  const titleId = useId()
  const toRef = useRef<HTMLInputElement>(null)
  const [to, setTo] = useState('')
  const [cc, setCc] = useState('')
  const [showCc, setShowCc] = useState(false)
  const [subject, setSubject] = useState('')
  const [body, setBody] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    setTo(draft?.to ?? '')
    setCc(draft?.cc ?? '')
    setShowCc(Boolean(draft?.cc))
    setSubject(draft?.subject ?? '')
    setBody(draft?.body ?? '')
    setError(null)
    setSending(false)
    const timer = window.setTimeout(() => toRef.current?.focus(), 40)
    return () => window.clearTimeout(timer)
  }, [open, draft])

  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && !sending) onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, sending, onClose])

  if (!open) return null

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    const recipients = splitAddresses(to)
    if (recipients.length === 0) {
      setError(t('compose.toRequired'))
      return
    }
    setSending(true)
    try {
      await sendMessage({
        to: recipients,
        cc: showCc ? splitAddresses(cc) : [],
        subject: subject.trim(),
        text: body,
      })
      onSent?.()
      onClose()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(
          t(`errors.codes.${err.code}`, {
            defaultValue: t('compose.sendFailed'),
          }),
        )
      } else {
        setError(t('errors.network'))
      }
    } finally {
      setSending(false)
    }
  }

  return (
    <div className={styles.backdrop} role="presentation" onClick={onClose}>
      <div
        className={styles.dialog}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onClick={(e) => e.stopPropagation()}
      >
        <header className={styles.header}>
          <h2 id={titleId} className={styles.title}>
            {t('compose.title')}
          </h2>
          <button
            type="button"
            className={styles.iconBtn}
            onClick={onClose}
            aria-label={t('common.close')}
            disabled={sending}
          >
            ×
          </button>
        </header>

        <form className={styles.form} onSubmit={handleSubmit}>
          <div className={styles.row}>
            <label className={styles.label} htmlFor="compose-to">
              {t('compose.to')}
            </label>
            <input
              ref={toRef}
              id="compose-to"
              className={styles.input}
              value={to}
              onChange={(e) => setTo(e.target.value)}
              placeholder={t('compose.toPlaceholder')}
              autoComplete="email"
              required
            />
            {!showCc ? (
              <button
                type="button"
                className={styles.ccToggle}
                onClick={() => setShowCc(true)}
              >
                {t('compose.cc')}
              </button>
            ) : null}
          </div>

          {showCc ? (
            <div className={styles.row}>
              <label className={styles.label} htmlFor="compose-cc">
                {t('compose.cc')}
              </label>
              <input
                id="compose-cc"
                className={styles.input}
                value={cc}
                onChange={(e) => setCc(e.target.value)}
                placeholder={t('compose.ccPlaceholder')}
                autoComplete="email"
              />
            </div>
          ) : null}

          <div className={styles.row}>
            <label className={styles.label} htmlFor="compose-subject">
              {t('compose.subject')}
            </label>
            <input
              id="compose-subject"
              className={styles.input}
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              placeholder={t('compose.subjectPlaceholder')}
            />
          </div>

          <textarea
            className={styles.body}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder={t('compose.bodyPlaceholder')}
            rows={12}
          />

          {error ? <p className={styles.error}>{error}</p> : null}

          <footer className={styles.footer}>
            <button
              type="button"
              className={styles.secondary}
              onClick={onClose}
              disabled={sending}
            >
              {t('common.cancel')}
            </button>
            <button type="submit" className={styles.primary} disabled={sending}>
              {sending ? t('compose.sending') : t('compose.send')}
            </button>
          </footer>
        </form>
      </div>
    </div>
  )
}
