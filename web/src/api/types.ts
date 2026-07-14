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

export type MessageSummary = {
  id: string
  uid: number
  subject: string
  from: MailAddress[]
  to?: MailAddress[]
  date: string
  flags?: string[]
  size?: number
}

export type MessageDetail = MessageSummary & {
  cc?: MailAddress[]
  text?: string
  html?: string
  rawSize?: number
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
  date: string
  unread: boolean
  starred: boolean
  attachments: { id: string; name: string; size: number }[]
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
    attachments: [],
  }
}

export function detailToUi(msg: MessageDetail, folder: string): UiMessage {
  const base = summaryToUi(msg, folder)
  const body = msg.text?.trim() || stripHtml(msg.html ?? '') || ''
  return {
    ...base,
    preview: body.slice(0, 140),
    body,
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
