import { useTranslation } from 'react-i18next'
import type { Message } from '../../data/mockMail'
import { formatBytes, formatMessageDate } from '../../data/mockMail'
import { useSettings } from '../../store/settings'
import styles from './ReadingPane.module.css'

type ReadingPaneProps = {
  message: Message | null
}

export function ReadingPane({ message }: ReadingPaneProps) {
  const { t } = useTranslation()
  const { language } = useSettings()

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

  const initials = message.from.name
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
          <span className={styles.folderTag}>{t(`nav.${message.folder}`)}</span>
        </div>
        <div className={styles.actions}>
          <button type="button" className={styles.iconBtn} aria-label={t('mail.reply')}>
            ↩
          </button>
          <button type="button" className={styles.iconBtn} aria-label={t('mail.forward')}>
            ↪
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
            {t('mail.to')} {message.to.name}
          </span>
        </div>
        <time className={styles.date} dateTime={message.date}>
          {formatMessageDate(message.date, language)}
        </time>
      </div>

      <div className={styles.body}>{message.body}</div>

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
        <button type="button" className={styles.footerBtn}>
          {t('mail.reply')}
        </button>
        <button type="button" className={styles.footerBtn}>
          {t('mail.replyAll')}
        </button>
        <button type="button" className={styles.footerBtn}>
          {t('mail.forward')}
        </button>
      </footer>
    </section>
  )
}
