import { useTranslation } from 'react-i18next'
import type { FolderRole, UiMessage } from '../../api/types'
import { formatMessageDate } from '../../utils/format'
import { useSettings } from '../../store/settings'
import styles from './MessageList.module.css'

type MessageListProps = {
  messages: UiMessage[]
  selectedId: string | null
  loading?: boolean
  folderRole?: FolderRole
  onSelect: (id: string) => void
  onRefresh?: () => void
  onToggleStar?: (id: string) => void
  onTrashSelected?: () => void
}

export function MessageList({
  messages,
  selectedId,
  loading,
  folderRole = 'other',
  onSelect,
  onRefresh,
  onToggleStar,
  onTrashSelected,
}: MessageListProps) {
  const { t } = useTranslation()
  const { language } = useSettings()
  const unreadCount = messages.filter((m) => m.unread).length

  if (loading) {
    return (
      <section className={styles.list}>
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{t('common.loading')}</div>
        </div>
      </section>
    )
  }

  const emptyTitle =
    folderRole === 'spam' ? t('mail.emptySpam') : t('mail.emptyInbox')
  const emptyHint =
    folderRole === 'spam' ? t('mail.emptySpamHint') : t('mail.emptyInboxHint')

  return (
    <section className={styles.list}>
      <div className={styles.toolbar}>
        <button
          type="button"
          className={styles.iconBtn}
          aria-label={t('mail.refresh')}
          title={t('mail.refresh')}
          onClick={onRefresh}
        >
          ↻
        </button>
        <button
          type="button"
          className={styles.iconBtn}
          aria-label={t('mail.trash')}
          title={t('mail.trash')}
          onClick={onTrashSelected}
          disabled={!selectedId}
        >
          ⌫
        </button>
        {messages.length > 0 ? (
          <span className={styles.toolbarMeta}>
            {unreadCount > 0
              ? t('mail.unread', { count: unreadCount })
              : t('mail.of', { from: 1, to: messages.length, total: messages.length })}
          </span>
        ) : null}
      </div>

      {messages.length === 0 ? (
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{emptyTitle}</div>
          <div>{emptyHint}</div>
        </div>
      ) : (
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
                  role="presentation"
                  onClick={(e) => {
                    e.stopPropagation()
                    onToggleStar?.(message.id)
                  }}
                >
                  {message.starred ? '★' : '☆'}
                </span>
                <div className={styles.main}>
                  <span className={styles.sender}>
                    {message.unread ? <span className={styles.unreadDot} aria-hidden /> : null}
                    {message.from.name}
                  </span>
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
      )}
    </section>
  )
}
