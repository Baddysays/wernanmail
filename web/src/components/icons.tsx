import type { ReactNode } from 'react'

/** Shared stroke icons — 24×24 viewBox, size via width/height props. */

type IconProps = {
  size?: number
  className?: string
}

function Svg({ size = 18, className, children }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      className={className}
      aria-hidden
    >
      {children}
    </svg>
  )
}

const stroke = {
  stroke: 'currentColor',
  strokeWidth: 1.7,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

export function IconRefresh({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path d="M20 12a8 8 0 1 1-2.2-5.4" {...stroke} />
      <path d="M20 4v5h-5" {...stroke} />
    </Svg>
  )
}

export function IconTrash({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path d="M4 7h16M9 7V5h6v2M8 7l.7 12h6.6L16 7" {...stroke} />
    </Svg>
  )
}

export function IconMenu({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path d="M4 7h16M4 12h16M4 17h16" {...stroke} />
    </Svg>
  )
}

export function IconStar({ size, className, filled }: IconProps & { filled?: boolean }) {
  return (
    <Svg size={size} className={className}>
      <path
        d="M12 3.5l2.4 4.9 5.4.8-3.9 3.8.9 5.4L12 16l-4.8 2.4.9-5.4L4.2 9.2l5.4-.8L12 3.5z"
        stroke="currentColor"
        strokeWidth="1.7"
        strokeLinejoin="round"
        fill={filled ? 'currentColor' : 'none'}
      />
    </Svg>
  )
}

export function IconReply({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path d="M9 14L4 9l5-5" {...stroke} />
      <path d="M4 9h10a6 6 0 0 1 6 6v2" {...stroke} />
    </Svg>
  )
}

export function IconReplyAll({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path d="M11 14L6 9l5-5" {...stroke} />
      <path d="M7 14L2 9l5-5" {...stroke} />
      <path d="M6 9h8a6 6 0 0 1 6 6v2" {...stroke} />
    </Svg>
  )
}

export function IconForward({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path d="M15 14l5-5-5-5" {...stroke} />
      <path d="M20 9H10a6 6 0 0 0-6 6v2" {...stroke} />
    </Svg>
  )
}

export function IconPaperclip({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path
        d="M15.5 7.5l-6.8 6.8a2.5 2.5 0 0 0 3.5 3.5l7.2-7.2a4 4 0 0 0-5.7-5.7L6 12.6"
        {...stroke}
      />
    </Svg>
  )
}

export function IconDownload({ size, className }: IconProps) {
  return (
    <Svg size={size} className={className}>
      <path d="M12 4v11M8 11l4 4 4-4M5 19h14" {...stroke} />
    </Svg>
  )
}
