import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { ToastHost, type ToastItem, type ToastTone } from './ToastHost'

type PushToastInput = {
  tone?: ToastTone
  title: string
  detail?: string
  durationMs?: number
  actionLabel?: string
  onAction?: () => void
}

type ToastContextValue = {
  pushToast: (input: PushToastInput) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

let toastSeq = 0

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastItem[]>([])

  const dismiss = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const pushToast = useCallback((input: PushToastInput) => {
    const id = `toast-${++toastSeq}`
    const durationMs = input.durationMs ?? (input.tone === 'error' ? 6500 : 4200)
    setToasts((prev) => [
      ...prev.slice(-4),
      {
        id,
        tone: input.tone ?? 'info',
        title: input.title,
        detail: input.detail,
        durationMs,
        actionLabel: input.actionLabel,
        onAction: input.onAction,
      },
    ])
  }, [])

  const value = useMemo(() => ({ pushToast }), [pushToast])

  return (
    <ToastContext.Provider value={value}>
      {children}
      <ToastHost toasts={toasts} onDismiss={dismiss} />
    </ToastContext.Provider>
  )
}

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) {
    throw new Error('useToast must be used within ToastProvider')
  }
  return ctx
}
