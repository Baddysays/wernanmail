import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { downloadAttachment } from '../../api/client'
import type { UiMessage } from '../../api/types'
import { formatBytes, formatMessageDate } from '../../utils/format'
import { useSettings } from '../../store/settings'
import {
  IconDownload,
  IconForward,
  IconPaperclip,
  IconReply,
  IconReplyAll,
  IconStar,
  IconTrash,
} from '../icons'
import styles from './ReadingPane.module.css'

type ReadingPaneProps = {
  message: UiMessage | null
  loading?: boolean
  onBack?: () => void
  onReply?: (message: UiMessage) => void
  onReplyAll?: (message: UiMessage) => void
  onForward?: (message: UiMessage) => void
  onTrash?: (message: UiMessage) => void
  onToggleStar?: (message: UiMessage) => void
  onDownloadError?: () => void
}

function hasRemoteImages(html: string): boolean {
  return /<img\b[^>]*\bsrc\s*=\s*["']https?:\/\//i.test(html)
}

function blockRemoteImages(html: string): string {
  return html.replace(
    /(<img\b[^>]*\bsrc\s*=\s*)(["'])(https?:\/\/[^"']+)\2/gi,
    '$1$2data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==$2 data-wernan-blocked=$2$3$2',
  )
}

function wrapHtmlDocument(html: string, allowRemote: boolean): string {
  const body = allowRemote ? html : blockRemoteImages(html)
  return `<!DOCTYPE html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><base target="_blank" rel="noopener noreferrer"><style>
html,body{margin:0;padding:0;background:transparent;color:#1a1a1a;font:15px/1.55 Georgia,"Times New Roman",serif;word-wrap:break-word;}
img{max-width:100%;height:auto;}
a{color:#0b57d0;}
pre,code{font-family:ui-monospace,Consolas,monospace;font-size:0.92em;white-space:pre-wrap;}
blockquote{margin:0.5em 0;padding-left:0.85em;border-left:3px solid #ccc;color:#555;}
</style></head><body>${body}</body></html>`
}

export function ReadingPane({
  message,
  loading,
  onBack,
  onReply,
  onReplyAll,
  onForward,
  onTrash,
  onToggleStar,
  onDownloadError,
}: ReadingPaneProps) {
  const { t } = useTranslation()
  const { language } = useSettings()
  const [allowRemote, setAllowRemote] = useState(false)
  const frameRef = useRef<HTMLIFrameElement>(null)

  useEffect(() => {
    setAllowRemote(false)
  }, [message?.id])

  function resizeFrame() {
    const frame = frameRef.current
    const doc = frame?.contentDocument
    if (!frame || !doc?.body) return
    const h = Math.max(doc.body.scrollHeight, doc.documentElement.scrollHeight, 48)
    frame.style.height = `${h + 8}px`
  }

  useEffect(() => {
    const id = window.setTimeout(resizeFrame, 50)
    return () => window.clearTimeout(id)
  }, [message?.id, message?.html, allowRemote])

  if (loading && !message?.body && !message?.html) {
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

  const html = message.html?.trim() || ''
  const showHtml = Boolean(html)
  const remoteBlocked = showHtml && !allowRemote && hasRemoteImages(html)

  async function handleDownload(file: { id: string; name: string }) {
    try {
      await downloadAttachment(message!.id, message!.folder, file.id, file.name)
    } catch {
      onDownloadError?.()
    }
  }

  return (
    <section className={styles.pane}>
      <header className={styles.header}>
        <div className={styles.headerMain}>
          {onBack ? (
            <button type="button" className={styles.backBtn} onClick={onBack}>
              ← {t('mail.backToList')}
            </button>
          ) : null}
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
            <IconStar size={17} filled={message.starred} />
          </button>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.reply')}
            title={t('mail.reply')}
            onClick={() => onReply?.(message)}
          >
            <IconReply size={17} />
          </button>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.replyAll')}
            title={t('mail.replyAll')}
            onClick={() => onReplyAll?.(message)}
          >
            <IconReplyAll size={17} />
          </button>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.forward')}
            title={t('mail.forward')}
            onClick={() => onForward?.(message)}
          >
            <IconForward size={17} />
          </button>
          <button
            type="button"
            className={styles.iconBtn}
            aria-label={t('mail.trash')}
            title={t('mail.trash')}
            onClick={() => onTrash?.(message)}
          >
            <IconTrash size={17} />
          </button>
        </div>
      </header>

      <div className={styles.scroll}>
        <div className={styles.senderRow}>
          <div className={styles.avatar} aria-hidden>
            {initials}
          </div>
          <div className={styles.senderMeta}>
            <span className={styles.senderName}>
              {message.from.name || message.from.email}
            </span>
            <span className={styles.senderEmail}>{message.from.email}</span>
            <span className={styles.recipient}>
              {t('mail.to')} {message.to.name || message.to.email}
            </span>
          </div>
          <time className={styles.date} dateTime={message.date}>
            {formatMessageDate(message.date, language)}
          </time>
        </div>

        {remoteBlocked ? (
          <div className={styles.privacyBar}>
            <span>{t('mail.imagesBlocked')}</span>
            <button type="button" className={styles.privacyBtn} onClick={() => setAllowRemote(true)}>
              {t('mail.showImages')}
            </button>
          </div>
        ) : null}

        {showHtml ? (
          <iframe
            ref={frameRef}
            className={styles.htmlFrame}
            title={t('mail.messageBody')}
            sandbox="allow-popups allow-popups-to-escape-sandbox"
            srcDoc={wrapHtmlDocument(html, allowRemote)}
            onLoad={resizeFrame}
          />
        ) : (
          <div className={styles.body}>{message.body || (loading ? t('common.loading') : '')}</div>
        )}

        {message.attachments.length > 0 ? (
          <div className={styles.attachments}>
            <h2 className={styles.attachmentsTitle}>
              {t('mail.attachments', { count: message.attachments.length })}
            </h2>
            <div className={styles.attachmentList}>
              {message.attachments.map((file) => (
                <button
                  key={file.id}
                  type="button"
                  className={styles.attachment}
                  onClick={() => void handleDownload(file)}
                >
                  <span className={styles.attachmentIcon} aria-hidden>
                    <IconPaperclip size={15} />
                  </span>
                  <span className={styles.attachmentMeta}>
                    <span className={styles.attachmentName}>{file.name}</span>
                    <span className={styles.attachmentSize}>{formatBytes(file.size)}</span>
                  </span>
                  <span className={styles.attachmentDl} aria-hidden>
                    <IconDownload size={15} />
                  </span>
                </button>
              ))}
            </div>
          </div>
        ) : null}
      </div>

      <footer className={styles.footer}>
        <button type="button" className={styles.footerBtn} onClick={() => onReply?.(message)}>
          <IconReply size={15} />
          {t('mail.reply')}
        </button>
        <button type="button" className={styles.footerBtn} onClick={() => onReplyAll?.(message)}>
          <IconReplyAll size={15} />
          {t('mail.replyAll')}
        </button>
        <button type="button" className={styles.footerBtn} onClick={() => onForward?.(message)}>
          <IconForward size={15} />
          {t('mail.forward')}
        </button>
        <button
          type="button"
          className={`${styles.footerBtn} ${styles.footerDanger}`}
          onClick={() => onTrash?.(message)}
        >
          <IconTrash size={15} />
          {t('mail.trash')}
        </button>
      </footer>
    </section>
  )
}
