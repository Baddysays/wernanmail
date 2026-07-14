import { useTranslation } from 'react-i18next'
import type { UiMessage } from '../../api/types'
import { formatBytes, formatMessageDate } from '../../utils/format'
import { useSettings } from '../../store/settings'
import styles from './ReadingPane.module.css'

type ReadingPaneProps = {
  message: UiMessage | null
  loading?: boolean
  onReply?: (message: UiMessage) => void
  onReplyAll?: (message: UiMessage) => void
  onForward?: (message: UiMessage) => void
  onTrash?: (message: UiMessage) => void
  onToggleStar?: (message: UiMessage) => void
}

export function ReadingPane({
  message,
  loading,
  onReply,
  onReplyAll,
  onForward,
  onTrash,
  onToggleStar,
}: ReadingPaneProps) {
  const { t } = useTranslation()
  const { language } = useSettings()

  if (loading && !message?.body) {
    return (
      <section className={styles.pane}>
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{t('common.loading')}</div>
        </div>
      </section>
    )
  }

  if (!message) {
    return (
      <section className={styles.pane}>
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{t('mail.emptySelection')}</div>
          <div>{t('mail.emptySelectionHint')}</div>
        </div>
      </section>
    )
  }

  const initials = (message.from.name || message.from.email || '?')
    .split(/\s+/)
    .map((part) => part[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()

  return (
    <section className={styles.pane}>
      <header className={styles.header}>
        <div>
          <h1 className={styles.subject}>
            {message.subject || t('mail.noSubject')}
          </h1>
          <span className={styles.folderTag}>{message.folder}</span>
        </div>
        <div className={styles.actions}>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.star')}
            title={t('mail.star')}
            onClick={() => onToggleStar?.(message)}
          >
            {message.starred ? '★' : '☆'}
          </button>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.reply')}
            onClick={() => onReply?.(message)}
          >
            ↩
          </button>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.forward')}
            onClick={() => onForward?.(message)}
          >
            ↪
          </button>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.trash')}
            title={t('mail.trash')}
            onClick={() => onTrash?.(message)}
          >
            ⌫
          </button>
        </div>
      </header>

      <div className={styles.senderRow}>
        <div className={styles.avatar} aria-hidden>
          {initials}
        </div>
        <div className={styles.senderMeta}>
          <span className={styles.senderName}>{message.from.name}</span>
          <span className={styles.senderEmail}>{`<${message.from.email}>`}</span>
          <span className={styles.recipient}>
            {t('mail.to')} {message.to.name || message.to.email}
          </span>
        </div>
        <time className={styles.date} dateTime={message.date}>
          {formatMessageDate(message.date, language)}
        </time>
      </div>

      <div className={styles.body}>{message.body || (loading ? t('common.loading') : '')}</div>

      {message.attachments.length > 0 ? (
        <div className={styles.attachments}>
          <h2 className={styles.attachmentsTitle}>
            {t('mail.attachments', { count: message.attachments.length })}
          </h2>
          <div className={styles.attachmentList}>
            {message.attachments.map((file) => (
              <div key={file.id} className={styles.attachment}>
                <div>
                  <div className={styles.attachmentName}>{file.name}</div>
                  <div className={styles.attachmentSize}>{formatBytes(file.size)}</div>
                </div>
              </div>
            ))}
          </div>
        </div>
      ) : null}

      <footer className={styles.footer}>
        <button type="button" className={styles.footerBtn} onClick={() => onReply?.(message)}>
          {t('mail.reply')}
        </button>
        <button type="button" className={styles.footerBtn} onClick={() => onReplyAll?.(message)}>
          {t('mail.replyAll')}
        </button>
        <button type="button" className={styles.footerBtn} onClick={() => onForward?.(message)}>
          {t('mail.forward')}
        </button>
        <button type="button" className={styles.footerBtn} onClick={() => onTrash?.(message)}>
          {t('mail.trash')}
        </button>
      </footer>
    </section>
  )
}
