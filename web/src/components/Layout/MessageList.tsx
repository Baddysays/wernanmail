import { useTranslation } from 'react-i18next'
import type { FolderRole, UiMessage } from '../../api/types'
import { formatMessageDate } from '../../utils/format'
import { useSettings } from '../../store/settings'
import { IconMenu, IconPaperclip, IconRefresh, IconStar, IconTrash } from '../icons'
import styles from './MessageList.module.css'

type MessageListProps = {
  messages: UiMessage[]
  selectedId: string | null
  loading?: boolean
  folderRole?: FolderRole
  searchQuery?: string
  onSearchChange?: (q: string) => void
  onSelect: (id: string) => void
  onRefresh?: () => void
  onToggleStar?: (id: string) => void
  onTrashSelected?: () => void
  onCompose?: () => void
  onOpenFolders?: () => void
}

export function MessageList({
  messages,
  selectedId,
  loading,
  folderRole = 'other',
  searchQuery = '',
  onSearchChange,
  onSelect,
  onRefresh,
  onToggleStar,
  onTrashSelected,
  onCompose,
  onOpenFolders,
}: MessageListProps) {
  const { t } = useTranslation()
  const { language } = useSettings()
  const unreadCount = messages.filter((m) => m.unread).length
  const showRecipient = folderRole === 'sent' || folderRole === 'drafts'

  const emptyTitle =
    folderRole === 'spam'
      ? t('mail.emptySpam')
      : folderRole === 'sent'
        ? t('mail.emptySent')
        : folderRole === 'drafts'
          ? t('mail.emptyDrafts')
          : folderRole === 'trash'
            ? t('mail.emptyTrash')
            : t('mail.emptyInbox')
  const emptyHint =
    folderRole === 'spam'
      ? t('mail.emptySpamHint')
      : folderRole === 'sent'
        ? t('mail.emptySentHint')
        : folderRole === 'drafts'
          ? t('mail.emptyDraftsHint')
          : folderRole === 'trash'
            ? t('mail.emptyTrashHint')
            : t('mail.emptyInboxHint')

  return (
    <section className={styles.list}>
      <div className={styles.toolbar}>
        {onOpenFolders ? (
          <button
            type="button"
            className={`${styles.iconBtn} ${styles.mobileOnly}`}
            aria-label={t('nav.folders')}
            title={t('nav.folders')}
            onClick={onOpenFolders}
          >
            <IconMenu size={17} />
          </button>
        ) : null}
        {onCompose ? (
          <button
            type="button"
            className={`${styles.composeBtn} ${styles.mobileOnly}`}
            onClick={onCompose}
          >
            {t('nav.compose')}
          </button>
        ) : null}
        <button
          type="button"
          className={styles.iconBtn}
          aria-label={t('mail.refresh')}
          title={t('mail.refresh')}
          onClick={onRefresh}
        >
          <IconRefresh size={16} />
        </button>
        <button
          type="button"
          className={styles.iconBtn}
          aria-label={t('mail.trash')}
          title={t('mail.trash')}
          onClick={onTrashSelected}
          disabled={!selectedId}
        >
          <IconTrash size={16} />
        </button>
        {onSearchChange ? (
          <input
            id="mail-search"
            className={styles.search}
            type="search"
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            placeholder={t('mail.searchPlaceholder')}
            aria-label={t('mail.searchPlaceholder')}
          />
        ) : null}
        {messages.length > 0 && !loading ? (
          <span className={styles.toolbarMeta}>
            {unreadCount > 0
              ? t('mail.unread', { count: unreadCount })
              : t('mail.of', { from: 1, to: messages.length, total: messages.length })}
          </span>
        ) : null}
      </div>

      {loading && messages.length === 0 ? (
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{t('common.loading')}</div>
        </div>
      ) : messages.length === 0 ? (
        <div className={styles.empty}>
          <div className={styles.emptyTitle}>{emptyTitle}</div>
          <div>{emptyHint}</div>
        </div>
      ) : (
        <div
          className={`${styles.items} ${loading ? styles.itemsDim : ''}`}
          role="listbox"
          aria-label={t('nav.inbox')}
          aria-busy={loading || undefined}
        >
          {messages.map((message) => {
            const active = message.id === selectedId
            const primary = showRecipient
              ? message.to.name || message.to.email || message.from.name
              : message.from.name
            return (
              <div
                key={message.id}
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
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    onSelect(message.id)
                  }
                }}
                tabIndex={0}
              >
                <button
                  type="button"
                  className={`${styles.star} ${message.starred ? styles.starOn : ''}`}
                  aria-label={t('mail.star')}
                  onClick={(e) => {
                    e.stopPropagation()
                    onToggleStar?.(message.id)
                  }}
                >
                  <IconStar size={15} filled={message.starred} />
                </button>
                <div className={styles.main}>
                  <span className={styles.sender}>
                    {message.unread ? <span className={styles.unreadDot} aria-hidden /> : null}
                    {primary}
                  </span>
                  <span className={styles.subject}>
                    {message.subject || t('mail.noSubject')}
                  </span>
                  <span className={styles.preview}>{message.preview}</span>
                </div>
                <div className={styles.meta}>
                  <span>{formatMessageDate(message.date, language)}</span>
                  {message.hasAttachment || message.attachments.length > 0 ? (
                    <span className={styles.clip} aria-hidden>
                      <IconPaperclip size={13} />
                    </span>
                  ) : null}
                </div>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
