import { useEffect } from 'react'
import styles from './ToastHost.module.css'

export type ToastTone = 'success' | 'error' | 'info'

export type ToastItem = {
  id: string
  tone: ToastTone
  title: string
  detail?: string
  durationMs: number
  actionLabel?: string
  onAction?: () => void
}

type ToastHostProps = {
  toasts: ToastItem[]
  onDismiss: (id: string) => void
}

export function ToastHost({ toasts, onDismiss }: ToastHostProps) {
  return (
    <div className={styles.host} aria-live="polite" aria-relevant="additions">
      {toasts.map((toast) => (
        <ToastCard key={toast.id} toast={toast} onDismiss={onDismiss} />
      ))}
    </div>
  )
}

function ToastCard({
  toast,
  onDismiss,
}: {
  toast: ToastItem
  onDismiss: (id: string) => void
}) {
  useEffect(() => {
    const timer = window.setTimeout(() => onDismiss(toast.id), toast.durationMs)
    return () => window.clearTimeout(timer)
  }, [toast.id, toast.durationMs, onDismiss])

  return (
    <div className={`${styles.toast} ${styles[toast.tone]}`} role="status">
      <div className={styles.body}>
        <p className={styles.title}>{toast.title}</p>
        {toast.detail ? <p className={styles.detail}>{toast.detail}</p> : null}
      </div>
      {toast.actionLabel && toast.onAction ? (
        <button
          type="button"
          className={styles.action}
          onClick={() => {
            toast.onAction?.()
            onDismiss(toast.id)
          }}
        >
          {toast.actionLabel}
        </button>
      ) : null}
      <button
        type="button"
        className={styles.dismiss}
        aria-label="Dismiss"
        onClick={() => onDismiss(toast.id)}
      >
        ×
      </button>
    </div>
  )
}
