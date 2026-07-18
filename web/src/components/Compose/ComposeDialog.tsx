import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { ApiError, saveDraft, sendMessage } from '../../api/client'
import { formatBytes } from '../../utils/format'
import styles from './ComposeDialog.module.css'

export type ComposeAttachment = {
  filename: string
  contentType: string
  content: string
}

export type ComposeDraft = {
  to?: string
  cc?: string
  bcc?: string
  subject?: string
  body?: string
  html?: string
  inReplyTo?: string
  references?: string
  attachments?: ComposeAttachment[]
  /** When set, a successful save/send should delete this previous draft UID. */
  replaceDraftId?: string
  replaceDraftFolder?: string
}

type ComposeDialogProps = {
  open: boolean
  draft?: ComposeDraft | null
  onClose: () => void
  onSent?: (info: {
    to: string[]
    replaceDraftId?: string
    replaceDraftFolder?: string
  }) => void
  onDraftSaved?: (info: {
    id?: string
    folder?: string
    silent?: boolean
  }) => void
}

type PendingFile =
  | { id: string; kind: 'file'; file: File }
  | { id: string; kind: 'ready'; att: ComposeAttachment; size: number }

const MAX_FILE_BYTES = 15 * 1024 * 1024
const MAX_TOTAL_BYTES = 25 * 1024 * 1024
const AUTOSAVE_MS = 2500

function splitAddresses(raw: string): string[] {
  return raw
    .split(/[,;\s]+/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function plainToHtml(text: string): string {
  if (!text) return ''
  return escapeHtml(text).replace(/\n/g, '<br>')
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk))
  }
  return btoa(binary)
}

async function fileToAttachment(file: File) {
  const buf = await file.arrayBuffer()
  return {
    filename: file.name,
    contentType: file.type || 'application/octet-stream',
    content: bytesToBase64(new Uint8Array(buf)),
  }
}

function estimateB64Size(b64: string) {
  return Math.floor((b64.length * 3) / 4)
}

export function ComposeDialog({ open, draft, onClose, onSent, onDraftSaved }: ComposeDialogProps) {
  const { t } = useTranslation()
  const titleId = useId()
  const toRef = useRef<HTMLInputElement>(null)
  const editorRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
  const [to, setTo] = useState('')
  const [cc, setCc] = useState('')
  const [bcc, setBcc] = useState('')
  const [showCc, setShowCc] = useState(false)
  const [showBcc, setShowBcc] = useState(false)
  const [subject, setSubject] = useState('')
  const [files, setFiles] = useState<PendingFile[]>([])
  const [status, setStatus] = useState<'idle' | 'sending' | 'saving'>('idle')
  const [autosaveHint, setAutosaveHint] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [draftId, setDraftId] = useState<string | undefined>()
  const [draftFolder, setDraftFolder] = useState<string | undefined>()
  const busy = status !== 'idle'
  const draftMetaRef = useRef({ id: undefined as string | undefined, folder: undefined as string | undefined })
  const savingRef = useRef(false)
  const openRef = useRef(open)
  const inReplyTo = draft?.inReplyTo
  const references = draft?.references

  useEffect(() => {
    openRef.current = open
  }, [open])

  useEffect(() => {
    draftMetaRef.current = { id: draftId, folder: draftFolder }
  }, [draftId, draftFolder])

  useEffect(() => {
    if (!open) return
    setTo(draft?.to ?? '')
    setCc(draft?.cc ?? '')
    setBcc(draft?.bcc ?? '')
    setShowCc(Boolean(draft?.cc))
    setShowBcc(Boolean(draft?.bcc))
    setSubject(draft?.subject ?? '')
    setFiles(
      (draft?.attachments ?? []).map((att, i) => ({
        id: `ready-${i}-${att.filename}`,
        kind: 'ready' as const,
        att,
        size: estimateB64Size(att.content),
      })),
    )
    setDraftId(draft?.replaceDraftId)
    setDraftFolder(draft?.replaceDraftFolder)
    setError(null)
    setStatus('idle')
    setAutosaveHint('idle')
    setDirty(false)
    const timer = window.setTimeout(() => {
      const el = editorRef.current
      if (el) {
        el.innerHTML = draft?.html?.trim()
          ? draft.html
          : plainToHtml(draft?.body ?? '')
      }
      toRef.current?.focus()
    }, 40)
    return () => window.clearTimeout(timer)
  }, [open, draft])

  function markDirty() {
    setDirty(true)
    setAutosaveHint((h) => (h === 'saved' ? 'idle' : h))
  }

  function readEditor() {
    const editor = editorRef.current
    const htmlRaw = (editor?.innerHTML ?? '').trim()
    const text = (editor?.innerText ?? '').replace(/\u00a0/g, ' ').trim()
    const html =
      htmlRaw && htmlRaw !== '<br>' && htmlRaw !== '<div><br></div>'
        ? htmlRaw
        : undefined
    return { text, html }
  }

  function hasMeaningfulContent(
    toVal: string,
    ccVal: string,
    bccVal: string,
    subjectVal: string,
    text: string,
    fileCount: number,
  ) {
    return Boolean(
      splitAddresses(toVal).length ||
        splitAddresses(ccVal).length ||
        splitAddresses(bccVal).length ||
        subjectVal.trim() ||
        text.trim() ||
        fileCount > 0,
    )
  }

  async function buildPayload(requireTo: boolean) {
    const recipients = splitAddresses(to)
    if (requireTo && recipients.length === 0) {
      setError(t('compose.toRequired'))
      return null
    }
    const { text, html } = readEditor()
    const attachments: ComposeAttachment[] = []
    for (const item of files) {
      if (item.kind === 'ready') attachments.push(item.att)
      else attachments.push(await fileToAttachment(item.file))
    }
    return {
      to: recipients,
      cc: showCc ? splitAddresses(cc) : [],
      bcc: showBcc ? splitAddresses(bcc) : [],
      subject: subject.trim(),
      text: text || '',
      html,
      attachments: attachments.length ? attachments : undefined,
      inReplyTo,
      references,
    }
  }

  async function persistDraft(opts: { silent: boolean; closeAfter?: boolean }) {
    if (savingRef.current || status === 'sending') return false
    const { text } = readEditor()
    if (
      !hasMeaningfulContent(to, showCc ? cc : '', showBcc ? bcc : '', subject, text, files.length)
    ) {
      return false
    }
    savingRef.current = true
    if (opts.silent) setAutosaveHint('saving')
    else {
      setError(null)
      setStatus('saving')
    }
    try {
      const payload = await buildPayload(false)
      if (!payload) return false
      const meta = draftMetaRef.current
      const result = await saveDraft({
        ...payload,
        replaceId: meta.id,
        replaceFolder: meta.folder,
      })
      if (result.id) {
        setDraftId(result.id)
        draftMetaRef.current.id = result.id
      }
      if (result.folder) {
        setDraftFolder(result.folder)
        draftMetaRef.current.folder = result.folder
      }
      setDirty(false)
      setAutosaveHint('saved')
      onDraftSaved?.({
        id: result.id || meta.id,
        folder: result.folder || meta.folder,
        silent: opts.silent,
      })
      if (opts.closeAfter) onClose()
      return true
    } catch (err) {
      if (opts.silent) {
        setAutosaveHint('error')
      } else if (err instanceof ApiError) {
        setError(
          t(`errors.codes.${err.code}`, {
            defaultValue: t('compose.draftFailed'),
          }),
        )
      } else {
        setError(t('errors.network'))
      }
      return false
    } finally {
      savingRef.current = false
      if (!opts.silent) setStatus('idle')
    }
  }

  async function requestClose() {
    if (busy || savingRef.current) return
    if (dirty) {
      const { text } = readEditor()
      if (hasMeaningfulContent(to, showCc ? cc : '', showBcc ? bcc : '', subject, text, files.length)) {
        await persistDraft({ silent: true, closeAfter: true })
        return
      }
    }
    onClose()
  }

  useEffect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') void requestClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, busy, dirty, to, cc, bcc, subject, files])

  useEffect(() => {
    if (!open || !dirty || busy) return
    const timer = window.setTimeout(() => {
      if (!openRef.current || savingRef.current) return
      void persistDraft({ silent: true })
    }, AUTOSAVE_MS)
    return () => window.clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- debounce on content edits
  }, [open, dirty, to, cc, bcc, subject, files, busy])

  if (!open) return null

  function exec(cmd: string, value?: string) {
    editorRef.current?.focus()
    document.execCommand(cmd, false, value)
    markDirty()
  }

  function addFiles(list: FileList | null) {
    if (!list?.length) return
    setError(null)
    const next = [...files]
    let total = next.reduce(
      (sum, f) => sum + (f.kind === 'file' ? f.file.size : f.size),
      0,
    )
    for (const file of Array.from(list)) {
      if (file.size > MAX_FILE_BYTES) {
        setError(t('compose.fileTooLarge', { name: file.name }))
        continue
      }
      if (total + file.size > MAX_TOTAL_BYTES) {
        setError(t('compose.filesTooLarge'))
        break
      }
      total += file.size
      next.push({
        id: `${file.name}-${file.size}-${file.lastModified}-${Math.random()}`,
        kind: 'file',
        file,
      })
    }
    setFiles(next)
    markDirty()
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    setStatus('sending')
    try {
      const payload = await buildPayload(true)
      if (!payload) {
        setStatus('idle')
        return
      }
      await sendMessage(payload)
      setDirty(false)
      const meta = draftMetaRef.current
      onSent?.({
        to: payload.to,
        replaceDraftId: meta.id,
        replaceDraftFolder: meta.folder,
      })
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
      setStatus('idle')
    }
  }

  async function handleSaveDraft() {
    await persistDraft({ silent: false, closeAfter: true })
  }

  const autosaveLabel =
    autosaveHint === 'saving'
      ? t('compose.autosaving')
      : autosaveHint === 'saved'
        ? t('compose.autosaved')
        : autosaveHint === 'error'
          ? t('compose.autosaveFailed')
          : null

  return (
    <div className={styles.backdrop} role="presentation" onClick={() => void requestClose()}>
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
            onClick={() => void requestClose()}
            aria-label={t('common.close')}
            disabled={busy}
          >
            ×
          </button>
        </header>

        <form className={styles.form} onSubmit={(e) => void handleSubmit(e)}>
          <div className={styles.row}>
            <label className={styles.label} htmlFor="compose-to">
              {t('compose.to')}
            </label>
            <input
              ref={toRef}
              id="compose-to"
              className={styles.input}
              value={to}
              onChange={(e) => {
                setTo(e.target.value)
                markDirty()
              }}
              placeholder={t('compose.toPlaceholder')}
              autoComplete="email"
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
            {!showBcc ? (
              <button
                type="button"
                className={styles.ccToggle}
                onClick={() => setShowBcc(true)}
              >
                {t('compose.bcc')}
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
                onChange={(e) => {
                  setCc(e.target.value)
                  markDirty()
                }}
                placeholder={t('compose.ccPlaceholder')}
                autoComplete="email"
              />
            </div>
          ) : null}

          {showBcc ? (
            <div className={styles.row}>
              <label className={styles.label} htmlFor="compose-bcc">
                {t('compose.bcc')}
              </label>
              <input
                id="compose-bcc"
                className={styles.input}
                value={bcc}
                onChange={(e) => {
                  setBcc(e.target.value)
                  markDirty()
                }}
                placeholder={t('compose.bccPlaceholder')}
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
              onChange={(e) => {
                setSubject(e.target.value)
                markDirty()
              }}
              placeholder={t('compose.subjectPlaceholder')}
            />
          </div>

          <div className={styles.toolbar} role="toolbar" aria-label={t('compose.formatting')}>
            <button
              type="button"
              className={styles.toolBtn}
              onClick={() => exec('bold')}
              title={t('compose.bold')}
            >
              <strong>B</strong>
            </button>
            <button
              type="button"
              className={styles.toolBtn}
              onClick={() => exec('italic')}
              title={t('compose.italic')}
            >
              <em>I</em>
            </button>
            <button
              type="button"
              className={styles.toolBtn}
              onClick={() => exec('underline')}
              title={t('compose.underline')}
            >
              <span style={{ textDecoration: 'underline' }}>U</span>
            </button>
            <button
              type="button"
              className={styles.toolBtn}
              onClick={() => exec('insertUnorderedList')}
              title={t('compose.list')}
            >
              •
            </button>
            <button
              type="button"
              className={styles.toolBtn}
              onClick={() => {
                const url = window.prompt(t('compose.linkPrompt'))
                if (url) exec('createLink', url)
              }}
              title={t('compose.link')}
            >
              🔗
            </button>
            <span className={styles.toolSpacer} />
            <button
              type="button"
              className={styles.toolBtn}
              onClick={() => fileInputRef.current?.click()}
              title={t('compose.attach')}
              disabled={busy}
            >
              📎 {t('compose.attach')}
            </button>
            <input
              ref={fileInputRef}
              type="file"
              multiple
              className={styles.fileInput}
              onChange={(e) => addFiles(e.target.files)}
            />
          </div>

          <div
            ref={editorRef}
            className={styles.editor}
            contentEditable={!busy}
            role="textbox"
            aria-multiline="true"
            aria-label={t('compose.bodyPlaceholder')}
            data-placeholder={t('compose.bodyPlaceholder')}
            onInput={() => {
              setError(null)
              markDirty()
            }}
          />

          {files.length > 0 ? (
            <ul className={styles.fileList}>
              {files.map((item) => {
                const name = item.kind === 'file' ? item.file.name : item.att.filename
                const size = item.kind === 'file' ? item.file.size : item.size
                return (
                  <li key={item.id} className={styles.fileItem}>
                    <span className={styles.fileName}>{name}</span>
                    <span className={styles.fileSize}>{formatBytes(size)}</span>
                    <button
                      type="button"
                      className={styles.fileRemove}
                      onClick={() => {
                        setFiles((prev) => prev.filter((f) => f.id !== item.id))
                        markDirty()
                      }}
                      aria-label={t('compose.removeFile', { name })}
                      disabled={busy}
                    >
                      ×
                    </button>
                  </li>
                )
              })}
            </ul>
          ) : null}

          {error ? <p className={styles.error}>{error}</p> : null}

          <footer className={styles.footer}>
            <span
              className={`${styles.autosave} ${autosaveHint === 'error' ? styles.autosaveError : ''}`}
              aria-live="polite"
            >
              {autosaveLabel}
            </span>
            <button
              type="button"
              className={styles.secondary}
              onClick={() => void requestClose()}
              disabled={busy}
            >
              {t('common.close')}
            </button>
            <button
              type="button"
              className={styles.secondary}
              onClick={() => void handleSaveDraft()}
              disabled={busy}
            >
              {status === 'saving' ? t('compose.saving') : t('compose.saveDraft')}
            </button>
            <button type="submit" className={styles.primary} disabled={busy}>
              {status === 'sending' ? t('compose.sending') : t('compose.send')}
            </button>
          </footer>
        </form>
      </div>
    </div>
  )
}
