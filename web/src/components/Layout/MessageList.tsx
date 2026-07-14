import { useTranslation } from 'react-i18next'
import type { UiMessage } from '../../api/types'
import { formatMessageDate } from '../../utils/format'
import { useSettings } from '../../store/settings'
import styles from './MessageList.module.css'

type MessageListProps = {
  messages: UiMessage[]
  selectedId: string | null
  loading?: boolean
  onSelect: (id: string) => void
  onRefresh?: () => void
}

export function MessageList({
  messages,
  selectedId,
  loading,
  onSelect,
  onRefresh,
}: MessageListProps) {
  const { t } = useTranslation()
  const { language } = useSettings()

  if (loading) {
    return (
      <section className={styles.list}>
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{t('common.loading')}</div>
        </div>
      </section>
    )
  }

  if (messages.length === 0) {
    return (
      <section className={styles.list}>
        <div className={styles.toolbar}>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.refresh')}
            onClick={onRefresh}
          >
            ↻
          </button>
        </div>
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{t('mail.emptyInbox')}</div>
          <div>{t('mail.emptyInboxHint')}</div>
        </div>
      </section>
    )
  }

  return (
    <section className={styles.list}>
      <div className={styles.toolbar}>
        <button
          type="button"
          className={styles.iconBtn}
          aria-label={t('mail.refresh')}
          onClick={onRefresh}
        >
          ↻
        </button>
        <span className={styles.toolbarMeta}>
          {t('mail.of', { from: 1, to: messages.length, total: messages.length })}
        </span>
      </div>

      <div className={styles.items} role="listbox" aria-label={t('nav.inbox')}>
        {messages.map((message) => {
          const active = message.id === selectedId
          return (
            <button
              key={message.id}
              type="button"
              role="option"
              aria-selected={active}
              className={[
                styles.item,
                active ? styles.itemActive : '',
                message.unread ? styles.itemUnread : '',
              ]
                .filter(Boolean)
                .join(' ')}
              onClick={() => onSelect(message.id)}
            >
              <span
                className={`${styles.star} ${message.starred ? styles.starOn : ''}`}
                aria-hidden
              >
                ★
              </span>
              <div className={styles.main}>
                <span className={styles.sender}>{message.from.name}</span>
                <span className={styles.subject}>
                  {message.subject || t('mail.noSubject')}
                </span>
                <span className={styles.preview}>{message.preview}</span>
              </div>
              <div className={styles.meta}>
                <span>{formatMessageDate(message.date, language)}</span>
                {message.attachments.length > 0 ? <span>📎</span> : null}
              </div>
            </button>
          )
        })}
      </div>
    </section>
  )
}
