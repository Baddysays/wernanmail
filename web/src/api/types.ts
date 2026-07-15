export type MailAddress = {
  name?: string
  address: string
}

export type Folder = {
  name: string
  delimiter?: string
  attributes?: string[]
  unseen?: number
  messages?: number
}

export type AttachmentMeta = {
  id: string
  filename: string
  contentType: string
  size: number
}

export type MessageSummary = {
  id: string
  uid: number
  subject: string
  from: MailAddress[]
  to?: MailAddress[]
  date: string
  flags?: string[]
  size?: number
  hasAttachment?: boolean
  messageId?: string
}

export type MessageDetail = MessageSummary & {
  cc?: MailAddress[]
  text?: string
  html?: string
  rawSize?: number
  attachments?: AttachmentMeta[]
}

/** UI row shape used by list + reading pane */
export type UiMessage = {
  id: string
  folder: string
  from: { name: string; email: string }
  to: { name: string; email: string }
  subject: string
  preview: string
  body: string
  html?: string
  messageId?: string
  cc: { name: string; email: string }[]
  date: string
  unread: boolean
  starred: boolean
  hasAttachment?: boolean
  attachments: { id: string; name: string; size: number; contentType?: string }[]
}

export type FolderRole =
  | 'inbox'
  | 'sent'
  | 'drafts'
  | 'archive'
  | 'spam'
  | 'trash'
  | 'starred'
  | 'other'

export function folderRole(folder: Folder): FolderRole {
  const attrs = (folder.attributes ?? []).map((a) => a.toLowerCase())
  if (attrs.some((a) => a.includes('inbox')) || folder.name.toUpperCase() === 'INBOX') {
    return 'inbox'
  }
  if (attrs.some((a) => a.includes('sent'))) return 'sent'
  if (attrs.some((a) => a.includes('draft'))) return 'drafts'
  if (attrs.some((a) => a.includes('archive') || a.includes('all'))) return 'archive'
  if (attrs.some((a) => a.includes('junk') || a.includes('spam'))) return 'spam'
  if (attrs.some((a) => a.includes('trash') || a.includes('bin'))) return 'trash'
  const n = folder.name.toLowerCase()
  if (n.includes('sent')) return 'sent'
  if (n.includes('draft')) return 'drafts'
  if (n.includes('spam') || n.includes('junk')) return 'spam'
  if (n.includes('trash') || n.includes('deleted')) return 'trash'
  if (n.includes('archive')) return 'archive'
  return 'other'
}

export function displayName(addrs: MailAddress[] | undefined): { name: string; email: string } {
  const a = addrs?.[0]
  if (!a) return { name: '', email: '' }
  return {
    name: a.name?.trim() || a.address,
    email: a.address,
  }
}

export function summaryToUi(msg: MessageSummary, folder: string): UiMessage {
  const from = displayName(msg.from)
  const to = displayName(msg.to)
  const flagged = (msg.flags ?? []).some((f) => f.toLowerCase().includes('flagged'))
  const seen = (msg.flags ?? []).some((f) => f.toLowerCase().includes('\\seen') || f === '\\Seen')
  return {
    id: msg.id,
    folder,
    from,
    to,
    subject: msg.subject,
    preview: '',
    body: '',
    date: msg.date,
    unread: !seen,
    starred: flagged,
    hasAttachment: Boolean(msg.hasAttachment),
    cc: [],
    attachments: [],
  }
}

export function detailToUi(msg: MessageDetail, folder: string): UiMessage {
  const base = summaryToUi(msg, folder)
  const text = msg.text?.trim() || ''
  const html = msg.html?.trim() || ''
  const body = text || stripHtml(html) || ''
  return {
    ...base,
    preview: body.slice(0, 140),
    body,
    html: html || undefined,
    messageId: msg.messageId,
    cc: (msg.cc ?? []).map((a) => ({
      name: a.name?.trim() || a.address,
      email: a.address,
    })),
    hasAttachment: (msg.attachments?.length ?? 0) > 0 || Boolean(msg.hasAttachment),
    attachments: (msg.attachments ?? []).map((a) => ({
      id: a.id,
      name: a.filename,
      size: a.size,
      contentType: a.contentType,
    })),
  }
}

function stripHtml(html: string): string {
  return html
    .replace(/<style[\s\S]*?<\/style>/gi, '')
    .replace(/<script[\s\S]*?<\/script>/gi, '')
    .replace(/<[^>]+>/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}
