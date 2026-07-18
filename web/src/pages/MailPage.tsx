import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ApiError,
  attachmentToBase64,
  fetchFolders,
  fetchMessage,
  fetchMessages,
  moveMessage,
  searchMessages,
  trashMessage,
  updateMessageFlags,
} from '../api/client'
import {
  detailToUi,
  folderRole,
  summaryToUi,
  type Folder,
  type UiMessage,
} from '../api/types'
import { ComposeDialog, type ComposeDraft } from '../components/Compose/ComposeDialog'
import { Sidebar } from '../components/Layout/Sidebar'
import { MessageList } from '../components/Layout/MessageList'
import { ReadingPane } from '../components/Layout/ReadingPane'
import { useToast } from '../components/Toast/ToastContext'
import styles from './MailPage.module.css'

const ROLE_ORDER = ['inbox', 'sent', 'drafts', 'archive', 'spam', 'trash'] as const
const POLL_MS = 35_000
const PAGE_SIZE = 50

function withRePrefix(subject: string, prefix: 'Re' | 'Fwd') {
  const trimmed = subject.trim()
  if (!trimmed) return `${prefix}:`
  const re = new RegExp(`^${prefix}:\\s*`, 'i')
  if (re.test(trimmed)) return trimmed
  return `${prefix}: ${trimmed}`
}

function quoteBody(message: UiMessage) {
  const lines = (message.body || '').split('\n').map((line) => `> ${line}`)
  return `\n\nOn ${message.date}, ${message.from.name || message.from.email} wrote:\n${lines.join('\n')}`
}

export function MailPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { pushToast } = useToast()
  const [folders, setFolders] = useState<Folder[]>([])
  const [folderName, setFolderName] = useState('INBOX')
  const [messages, setMessages] = useState<UiMessage[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [selected, setSelected] = useState<UiMessage | null>(null)
  const [loadingList, setLoadingList] = useState(true)
  const [loadingMsg, setLoadingMsg] = useState(false)
  const [composeOpen, setComposeOpen] = useState(false)
  const [composeDraft, setComposeDraft] = useState<ComposeDraft | null>(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [searching, setSearching] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const [hasMore, setHasMore] = useState(false)
  const [online, setOnline] = useState(
    typeof navigator === 'undefined' ? true : navigator.onLine,
  )
  const [foldersOpen, setFoldersOpen] = useState(false)

  const messagesRef = useRef(messages)
  const folderNameRef = useRef(folderName)
  const knownIdsRef = useRef<Set<string>>(new Set())
  const pollReadyRef = useRef(false)
  const listOffsetRef = useRef(0)

  useEffect(() => {
    messagesRef.current = messages
  }, [messages])

  useEffect(() => {
    folderNameRef.current = folderName
    pollReadyRef.current = false
    knownIdsRef.current = new Set()
    listOffsetRef.current = 0
    setHasMore(false)
  }, [folderName])

  useEffect(() => {
    const on = () => setOnline(true)
    const off = () => setOnline(false)
    window.addEventListener('online', on)
    window.addEventListener('offline', off)
    return () => {
      window.removeEventListener('online', on)
      window.removeEventListener('offline', off)
    }
  }, [])

  const sidebarFolders = useMemo(() => {
    const ranked = [...folders].sort((a, b) => {
      const ra = ROLE_ORDER.indexOf(folderRole(a) as (typeof ROLE_ORDER)[number])
      const rb = ROLE_ORDER.indexOf(folderRole(b) as (typeof ROLE_ORDER)[number])
      const ia = ra === -1 ? 99 : ra
      const ib = rb === -1 ? 99 : rb
      if (ia !== ib) return ia - ib
      return a.name.localeCompare(b.name)
    })
    const seen = new Set<string>()
    const primary: Folder[] = []
    const custom: Folder[] = []
    for (const f of ranked) {
      const role = folderRole(f)
      if (role === 'other') {
        custom.push(f)
        continue
      }
      if (seen.has(role)) continue
      seen.add(role)
      primary.push(f)
    }
    if (primary.length === 0 && folders.length > 0) {
      return folders.slice(0, 12)
    }
    return [...primary, ...custom]
  }, [folders])

  const folderByRole = useCallback(
    (role: (typeof ROLE_ORDER)[number]) =>
      folders.find((f) => folderRole(f) === role)?.name,
    [folders],
  )

  const activeRole = useMemo(() => {
    const f = folders.find((x) => x.name === folderName)
    return f ? folderRole(f) : 'other'
  }, [folders, folderName])

  const handleAuthError = useCallback(
    (err: unknown) => {
      if (err instanceof ApiError && (err.status === 401 || err.code.startsWith('mail.session'))) {
        navigate('/login')
        return true
      }
      return false
    },
    [navigate],
  )

  const openCompose = useCallback((draft?: ComposeDraft) => {
    setComposeDraft(draft ?? null)
    setComposeOpen(true)
  }, [])

  const closeCompose = useCallback(() => {
    setComposeOpen(false)
    setComposeDraft(null)
  }, [])

  const loadFolders = useCallback(async () => {
    try {
      const list = await fetchFolders()
      setFolders(list)
      setFolderName((prev) => {
        if (prev) return prev
        const inbox =
          list.find((f) => folderRole(f) === 'inbox') ??
          list.find((f) => f.name.toUpperCase() === 'INBOX') ??
          list[0]
        return inbox?.name ?? 'INBOX'
      })
    } catch (err) {
      if (handleAuthError(err)) return
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }, [handleAuthError, pushToast, t])

  const applyMessageList = useCallback(
    (list: UiMessage[], announceNew: boolean) => {
      const ids = new Set(list.map((m) => m.id))
      if (announceNew && pollReadyRef.current) {
        const fresh = list.filter((m) => !knownIdsRef.current.has(m.id) && m.unread)
        if (fresh.length === 1) {
          const msg = fresh[0]
          pushToast({
            tone: 'info',
            title: t('mail.newMail'),
            detail: msg.subject || msg.from.name || msg.from.email,
            durationMs: 5500,
          })
        } else if (fresh.length > 1) {
          pushToast({
            tone: 'info',
            title: t('mail.newMailCount', { count: fresh.length }),
            durationMs: 5500,
          })
        }
      }
      knownIdsRef.current = ids
      pollReadyRef.current = true
      setMessages(list)
      setSelectedId((prev) => {
        if (prev && list.some((m) => m.id === prev)) return prev
        // Desktop: keep a selection when possible. Mobile: stay on list until tap.
        if (typeof window !== 'undefined' && window.matchMedia('(max-width: 960px)').matches) {
          return null
        }
        return prev ?? list[0]?.id ?? null
      })
    },
    [pushToast, t],
  )

  const loadMessages = useCallback(
    async (folder: string, opts?: { silent?: boolean; announceNew?: boolean }) => {
      if (!opts?.silent) setLoadingList(true)
      try {
        const list = await fetchMessages(folder, PAGE_SIZE, 0)
        const prevById = new Map(messagesRef.current.map((m) => [m.id, m]))
        const ui = list.map((m) => {
          const row = summaryToUi(m, folder)
          const prev = prevById.get(m.id)
          if (prev?.preview && !row.preview) row.preview = prev.preview
          return row
        })
        listOffsetRef.current = ui.length
        setHasMore(list.length >= PAGE_SIZE)
        applyMessageList(ui, Boolean(opts?.announceNew))
        if (!opts?.silent) setSelected(null)
      } catch (err) {
        if (handleAuthError(err)) return
        if (!opts?.silent) {
          pushToast({ tone: 'error', title: t('errors.generic') })
          setMessages([])
          setSelectedId(null)
          setSelected(null)
          setHasMore(false)
        }
      } finally {
        if (!opts?.silent) setLoadingList(false)
      }
    },
    [applyMessageList, handleAuthError, pushToast, t],
  )

  const loadMoreMessages = useCallback(async () => {
    if (loadingMore || searching || searchQuery.trim()) return
    const folder = folderNameRef.current
    if (!folder) return
    setLoadingMore(true)
    try {
      const offset = listOffsetRef.current
      const list = await fetchMessages(folder, PAGE_SIZE, offset)
      const ui = list.map((m) => summaryToUi(m, folder))
      listOffsetRef.current = offset + ui.length
      setHasMore(list.length >= PAGE_SIZE)
      setMessages((prev) => {
        const seen = new Set(prev.map((m) => m.id))
        return [...prev, ...ui.filter((m) => !seen.has(m.id))]
      })
    } catch (err) {
      if (handleAuthError(err)) return
      pushToast({ tone: 'error', title: t('errors.generic') })
    } finally {
      setLoadingMore(false)
    }
  }, [handleAuthError, loadingMore, pushToast, searchQuery, searching, t])

  useEffect(() => {
    void loadFolders()
  }, [loadFolders])

  useEffect(() => {
    if (!folderName) return
    void loadMessages(folderName)
  }, [folderName, loadMessages])

  useEffect(() => {
    const q = searchQuery.trim()
    if (!q) {
      setSearching(false)
      return
    }
    let cancelled = false
    const timer = window.setTimeout(() => {
      setSearching(true)
      void searchMessages(folderName, q, 50)
        .then((list) => {
          if (cancelled) return
          setHasMore(false)
          setMessages(list.map((m) => summaryToUi(m, folderName)))
        })
        .catch((err) => {
          if (cancelled) return
          if (handleAuthError(err)) return
          pushToast({ tone: 'error', title: t('errors.generic') })
        })
        .finally(() => {
          if (!cancelled) setSearching(false)
        })
    }, 320)
    return () => {
      cancelled = true
      window.clearTimeout(timer)
    }
  }, [searchQuery, folderName, handleAuthError, pushToast, t])

  useEffect(() => {
    if (!selectedId) {
      setSelected(null)
      return
    }
    let cancelled = false
    setLoadingMsg(true)
    void fetchMessage(selectedId, folderName)
      .then((detail) => {
        if (cancelled) return
        const ui = detailToUi(detail, folderName)
        const wasUnread = messagesRef.current.find((m) => m.id === selectedId)?.unread
        setSelected(ui)
        setMessages((prev) =>
          prev.map((m) =>
            m.id === selectedId
              ? { ...m, unread: false, preview: ui.preview || m.preview, cc: ui.cc }
              : m,
          ),
        )
        if (wasUnread) {
          setFolders((prev) =>
            prev.map((f) => {
              if (f.name !== folderName || !f.unseen) return f
              return { ...f, unseen: Math.max(0, (f.unseen ?? 0) - 1) }
            }),
          )
        }
      })
      .catch((err) => {
        if (cancelled) return
        if (handleAuthError(err)) return
        pushToast({ tone: 'error', title: t('errors.generic') })
        setSelected(null)
      })
      .finally(() => {
        if (!cancelled) setLoadingMsg(false)
      })
    return () => {
      cancelled = true
    }
  }, [selectedId, folderName, handleAuthError])

  // Background refresh: folders + current mailbox.
  useEffect(() => {
    const tick = () => {
      if (document.visibilityState === 'hidden') return
      void loadFolders()
      const folder = folderNameRef.current
      if (folder) void loadMessages(folder, { silent: true, announceNew: true })
    }
    const id = window.setInterval(tick, POLL_MS)
    const onVis = () => {
      if (document.visibilityState === 'visible') tick()
    }
    document.addEventListener('visibilitychange', onVis)
    return () => {
      window.clearInterval(id)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [loadFolders, loadMessages])

  const filteredMessages = useMemo(() => messages, [messages])

  function replyHeaders(message: UiMessage) {
    const mid = message.messageId?.trim()
    if (!mid) return {}
    const normalized = mid.startsWith('<') ? mid : `<${mid}>`
    return { inReplyTo: normalized, references: normalized }
  }

  function handleReply(message: UiMessage) {
    openCompose({
      to: message.from.email,
      subject: withRePrefix(message.subject || '', 'Re'),
      body: quoteBody(message),
      ...replyHeaders(message),
    })
  }

  function handleReplyAll(message: UiMessage) {
    const from = message.from.email.toLowerCase()
    const recipients = [
      message.to.email,
      ...message.cc.map((c) => c.email),
    ]
      .map((e) => e.trim())
      .filter((e) => e && e.toLowerCase() !== from)
    const cc = [...new Set(recipients)].join(', ')
    openCompose({
      to: message.from.email,
      cc,
      subject: withRePrefix(message.subject || '', 'Re'),
      body: quoteBody(message),
      ...replyHeaders(message),
    })
  }

  async function handleForward(message: UiMessage) {
    const draft: ComposeDraft = {
      subject: withRePrefix(message.subject || '', 'Fwd'),
      body: `\n\n---------- Forwarded message ----------\nFrom: ${message.from.name || message.from.email} <${message.from.email}>\nDate: ${message.date}\nSubject: ${message.subject || ''}\nTo: ${message.to.email}\n\n${message.body || ''}`,
    }
    if (message.attachments.length > 0) {
      try {
        const attachments = await Promise.all(
          message.attachments.map(async (a) => {
            const { content, contentType } = await attachmentToBase64(
              message.id,
              message.folder || folderName,
              a.id,
            )
            return {
              filename: a.name,
              contentType: a.contentType || contentType,
              content,
            }
          }),
        )
        draft.attachments = attachments
      } catch (err) {
        if (handleAuthError(err)) return
        pushToast({ tone: 'error', title: t('mail.downloadFailed') })
      }
    }
    openCompose(draft)
  }

  async function handleTrash(message: UiMessage) {
    const fromFolder = message.folder || folderName
    const snapshot = message
    // Optimistic remove — feels instant; undo restores from Trash.
    setMessages((prev) => prev.filter((m) => m.id !== message.id))
    if (selectedId === message.id) {
      setSelectedId(null)
      setSelected(null)
    }
    try {
      const result = await trashMessage(message.id, fromFolder)
      const dest = (result.fromFolder || fromFolder).toLowerCase()
      const canUndo =
        Boolean(result.trashId) &&
        Boolean(result.trashFolder) &&
        result.trashFolder.toLowerCase() !== dest

      if (canUndo) {
        pushToast({
          tone: 'success',
          title: t('mail.trashed'),
          actionLabel: t('mail.undo'),
          durationMs: 6000,
          onAction: () => {
            void (async () => {
              try {
                await moveMessage(result.trashId, result.trashFolder, result.fromFolder || fromFolder)
                pushToast({ tone: 'success', title: t('mail.restored') })
                await loadMessages(folderName)
                void loadFolders()
              } catch (err) {
                if (handleAuthError(err)) return
                pushToast({ tone: 'error', title: t('errors.generic') })
              }
            })()
          },
        })
      } else {
        pushToast({ tone: 'success', title: t('mail.trashed') })
      }
      void loadFolders()
    } catch (err) {
      if (handleAuthError(err)) return
      // Roll back optimistic remove
      setMessages((prev) => {
        if (prev.some((m) => m.id === snapshot.id)) return prev
        return [snapshot, ...prev]
      })
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }

  async function handleMove(message: UiMessage, toFolder: string, successKey: string) {
    const fromFolder = message.folder || folderName
    if (!toFolder || toFolder === fromFolder) return
    const snapshot = message
    setMessages((prev) => prev.filter((m) => m.id !== message.id))
    if (selectedId === message.id) {
      setSelectedId(null)
      setSelected(null)
    }
    try {
      await moveMessage(message.id, fromFolder, toFolder)
      pushToast({ tone: 'success', title: t(successKey) })
      void loadFolders()
    } catch (err) {
      if (handleAuthError(err)) return
      setMessages((prev) => {
        if (prev.some((m) => m.id === snapshot.id)) return prev
        return [snapshot, ...prev]
      })
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }

  async function handleMarkUnread(message: UiMessage) {
    try {
      await updateMessageFlags(message.id, message.folder || folderName, {
        remove: ['\\Seen'],
      })
      setMessages((prev) =>
        prev.map((m) => (m.id === message.id ? { ...m, unread: true } : m)),
      )
      setSelected((cur) => (cur?.id === message.id ? { ...cur, unread: true } : cur))
      setFolders((prev) =>
        prev.map((f) => {
          if (f.name !== (message.folder || folderName)) return f
          return { ...f, unseen: (f.unseen ?? 0) + 1 }
        }),
      )
    } catch (err) {
      if (handleAuthError(err)) return
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }

  async function handleEditDraft(message: UiMessage) {
    const draft: ComposeDraft = {
      to: message.to.email,
      cc: message.cc.map((c) => c.email).filter(Boolean).join(', '),
      subject: message.subject || '',
      body: message.body || '',
      html: message.html,
      replaceDraftId: message.id,
      replaceDraftFolder: message.folder || folderName,
    }
    if (message.attachments.length > 0) {
      try {
        draft.attachments = await Promise.all(
          message.attachments.map(async (a) => {
            const { content, contentType } = await attachmentToBase64(
              message.id,
              message.folder || folderName,
              a.id,
            )
            return {
              filename: a.name,
              contentType: a.contentType || contentType,
              content,
            }
          }),
        )
      } catch (err) {
        if (handleAuthError(err)) return
        pushToast({ tone: 'error', title: t('mail.downloadFailed') })
      }
    }
    openCompose(draft)
  }

  async function discardReplacedDraft(info: {
    replaceDraftId?: string
    replaceDraftFolder?: string
  }) {
    if (!info.replaceDraftId || !info.replaceDraftFolder) return
    try {
      await trashMessage(info.replaceDraftId, info.replaceDraftFolder)
    } catch {
      /* best-effort cleanup of previous draft copy */
    }
  }

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null
      const typing =
        target &&
        (target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.isContentEditable)
      if (composeOpen) return
      if (typing && e.key !== 'Escape') {
        if (e.key === 'Escape') (target as HTMLElement).blur()
        return
      }

      if (e.key === '/' && !typing) {
        e.preventDefault()
        document.getElementById('mail-search')?.focus()
        return
      }
      if (e.key === 'c' && !e.metaKey && !e.ctrlKey && !typing) {
        e.preventDefault()
        openCompose()
        return
      }
      if (e.key === 'r' && selected && !typing && activeRole !== 'drafts') {
        e.preventDefault()
        handleReply(selected)
        return
      }
      if (e.key === 'e' && selected && !typing && activeRole === 'drafts') {
        e.preventDefault()
        void handleEditDraft(selected)
        return
      }
      if (e.key === 'e' && selected && !typing && activeRole !== 'drafts') {
        const archive = folderByRole('archive')
        if (archive) {
          e.preventDefault()
          void handleMove(selected, archive, 'mail.archived')
        }
        return
      }
      if (e.key === '!' && selected && !typing) {
        const spam = folderByRole('spam')
        if (spam) {
          e.preventDefault()
          void handleMove(selected, spam, 'mail.spammed')
        }
        return
      }
      if (e.key === 'u' && selected && !typing) {
        e.preventDefault()
        void handleMarkUnread(selected)
        return
      }
      if ((e.key === 'j' || e.key === 'k') && !typing) {
        e.preventDefault()
        const list = filteredMessages
        if (!list.length) return
        const idx = list.findIndex((m) => m.id === selectedId)
        const next =
          e.key === 'j'
            ? list[Math.min(list.length - 1, Math.max(0, idx) + 1)]
            : list[Math.max(0, (idx < 0 ? 0 : idx) - 1)]
        if (next) setSelectedId(next.id)
        return
      }
      if ((e.key === 'Delete' || e.key === '#') && selected && !typing) {
        e.preventDefault()
        void handleTrash(selected)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [activeRole, composeOpen, filteredMessages, folderByRole, openCompose, selected, selectedId, folderName])

  async function handleToggleStar(message: UiMessage) {
    const next = !message.starred
    try {
      await updateMessageFlags(message.id, message.folder || folderName, {
        add: next ? ['\\Flagged'] : [],
        remove: next ? [] : ['\\Flagged'],
      })
      setMessages((prev) =>
        prev.map((m) => (m.id === message.id ? { ...m, starred: next } : m)),
      )
      setSelected((cur) => (cur?.id === message.id ? { ...cur, starred: next } : cur))
    } catch (err) {
      if (handleAuthError(err)) return
      pushToast({ tone: 'error', title: t('errors.generic') })
    }
  }

  return (
    <div
      className={`${styles.page} ${selectedId ? styles.pageReadMode : styles.pageListMode}`}
    >
      {!online ? (
        <div
          style={{
            position: 'absolute',
            top: 0,
            left: 0,
            right: 0,
            zIndex: 5,
            padding: '0.4rem 1rem',
            textAlign: 'center',
            fontSize: '0.85rem',
            background: 'color-mix(in srgb, #c44 18%, var(--bg-elevated))',
            color: 'var(--text-primary)',
          }}
        >
          {t('mail.offline')}
        </div>
      ) : null}
      <div className={`${styles.navColumn} ${foldersOpen ? styles.navOpen : ''}`}>
        <Sidebar
          folders={sidebarFolders}
          activeFolder={folderName}
          onSelectFolder={(name) => {
            setSelectedId(null)
            setSelected(null)
            setSearchQuery('')
            setFolderName(name)
            setFoldersOpen(false)
          }}
          onCompose={() => {
            setFoldersOpen(false)
            openCompose()
          }}
        />
      </div>
      {foldersOpen ? (
        <button
          type="button"
          className={styles.drawerBackdrop}
          aria-label={t('common.close')}
          onClick={() => setFoldersOpen(false)}
        />
      ) : null}
      <div className={styles.listColumn}>
        <MessageList
          messages={filteredMessages}
          selectedId={selectedId}
          loading={loadingList || searching}
          loadingMore={loadingMore}
          hasMore={hasMore && !searchQuery.trim()}
          folderRole={activeRole}
          searchQuery={searchQuery}
          onSearchChange={(q) => {
            setSearchQuery(q)
            if (!q.trim()) void loadMessages(folderName)
          }}
          onSelect={setSelectedId}
          onOpen={(id) => {
            const msg = messages.find((m) => m.id === id) ?? selected
            if (activeRole === 'drafts' && msg) void handleEditDraft(msg)
            else setSelectedId(id)
          }}
          onLoadMore={() => void loadMoreMessages()}
          onCompose={() => openCompose()}
          onOpenFolders={() => setFoldersOpen(true)}
          onRefresh={() => {
            setSearchQuery('')
            void loadMessages(folderName)
            void loadFolders()
          }}
          onToggleStar={(id) => {
            const msg = messages.find((m) => m.id === id)
            if (msg) void handleToggleStar(msg)
          }}
          onTrashSelected={() => {
            const msg = messages.find((m) => m.id === selectedId) ?? selected
            if (msg) void handleTrash(msg)
          }}
        />
      </div>
      <div className={styles.readColumn}>
        <ReadingPane
          message={selected}
          loading={loadingMsg}
          isDraft={activeRole === 'drafts'}
          canArchive={Boolean(folderByRole('archive')) && activeRole !== 'archive'}
          canSpam={Boolean(folderByRole('spam')) && activeRole !== 'spam'}
          onBack={() => {
            setSelectedId(null)
            setSelected(null)
          }}
          onReply={handleReply}
          onReplyAll={handleReplyAll}
          onForward={(m) => void handleForward(m)}
          onTrash={handleTrash}
          onArchive={(m) => {
            const dest = folderByRole('archive')
            if (dest) void handleMove(m, dest, 'mail.archived')
          }}
          onSpam={(m) => {
            const dest = folderByRole('spam')
            if (dest) void handleMove(m, dest, 'mail.spammed')
          }}
          onEditDraft={(m) => void handleEditDraft(m)}
          onMarkUnread={(m) => void handleMarkUnread(m)}
          onToggleStar={handleToggleStar}
          onDownloadError={() =>
            pushToast({ tone: 'error', title: t('mail.downloadFailed') })
          }
        />
      </div>
      <ComposeDialog
        open={composeOpen}
        draft={composeDraft}
        onClose={closeCompose}
        onDraftSaved={(info) => {
          if (info.silent) {
            void loadFolders()
            return
          }
          pushToast({
            tone: 'success',
            title: t('compose.draftSaved'),
            actionLabel: t('compose.viewDraft'),
            onAction: () => {
              const drafts = sidebarFolders.find((f) => folderRole(f) === 'drafts')
              if (drafts) {
                setFolderName(drafts.name)
                void loadMessages(drafts.name)
              }
            },
          })
          void loadFolders()
          if (activeRole === 'drafts') void loadMessages(folderName)
        }}
        onSent={(info) => {
          void (async () => {
            await discardReplacedDraft(info)
            pushToast({
              tone: 'success',
              title: t('mail.sent'),
              detail: t('mail.sentSaved'),
              durationMs: 6500,
            })
            void loadFolders()
            const sent = folderByRole('sent')
            if (sent && folderName === sent) void loadMessages(folderName)
            if (activeRole === 'drafts') void loadMessages(folderName)
          })()
        }}
      />
    </div>
  )
}
