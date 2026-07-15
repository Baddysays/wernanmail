import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'

export type SelectOption<T extends string | number = string> = {
  value: T
  label: string
}

type SelectProps<T extends string | number = string> = {
  value: T | ''
  onChange: (value: T) => void
  options: SelectOption<T>[]
  placeholder?: string
  disabled?: boolean
  className?: string
  id?: string
  'aria-label'?: string
}

export function Select<T extends string | number = string>({
  value,
  onChange,
  options,
  placeholder,
  disabled,
  className = '',
  id,
  'aria-label': ariaLabel,
}: SelectProps<T>) {
  const [open, setOpen] = useState(false)
  const [activeIdx, setActiveIdx] = useState(-1)
  const rootRef = useRef<HTMLDivElement>(null)
  const listRef = useRef<HTMLUListElement>(null)
  const listId = useId()
  const selected = options.find((o) => String(o.value) === String(value))
  const label = selected?.label ?? placeholder ?? ''
  const selectedIdx = options.findIndex((o) => String(o.value) === String(value))

  useEffect(() => {
    if (!open) return
    setActiveIdx(selectedIdx >= 0 ? selectedIdx : 0)
    listRef.current?.focus()
    function onDoc(e: MouseEvent) {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open, selectedIdx])

  function commit(idx: number) {
    const opt = options[idx]
    if (!opt) return
    onChange(opt.value)
    setOpen(false)
  }

  function move(delta: number) {
    setActiveIdx((i) => {
      const start = i < 0 ? (selectedIdx >= 0 ? selectedIdx : 0) : i
      const next = Math.min(options.length - 1, Math.max(0, start + delta))
      return next
    })
  }

  function onTriggerKey(e: KeyboardEvent<HTMLButtonElement>) {
    if (disabled) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (!open) {
        setOpen(true)
        return
      }
      move(1)
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (!open) {
        setOpen(true)
        return
      }
      move(-1)
      return
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      if (!open) {
        setOpen(true)
        return
      }
      commit(activeIdx >= 0 ? activeIdx : selectedIdx)
      return
    }
    if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  function onListKey(e: KeyboardEvent<HTMLUListElement>) {
    if (e.key === 'Escape') {
      e.preventDefault()
      setOpen(false)
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      move(1)
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      move(-1)
    }
    if (e.key === 'Home') {
      e.preventDefault()
      setActiveIdx(0)
    }
    if (e.key === 'End') {
      e.preventDefault()
      setActiveIdx(options.length - 1)
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      commit(activeIdx)
    }
  }

  const activeId = activeIdx >= 0 ? `${listId}-opt-${activeIdx}` : undefined

  return (
    <div className={`wm-select ${className} ${open ? 'open' : ''} ${disabled ? 'disabled' : ''}`} ref={rootRef}>
      <button
        type="button"
        id={id}
        className="wm-select-trigger"
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={open ? activeId : undefined}
        aria-label={ariaLabel}
        onClick={() => !disabled && setOpen((v) => !v)}
        onKeyDown={onTriggerKey}
      >
        <span className={`wm-select-value ${selected ? '' : 'placeholder'}`}>{label}</span>
        <span className="wm-select-chevron" aria-hidden />
      </button>
      {open ? (
        <ul
          className="wm-select-menu"
          role="listbox"
          id={listId}
          tabIndex={-1}
          ref={listRef}
          onKeyDown={onListKey}
        >
          {options.map((o, idx) => {
            const active = String(o.value) === String(value)
            const focused = idx === activeIdx
            return (
              <li key={String(o.value)} role="presentation">
                <button
                  type="button"
                  id={`${listId}-opt-${idx}`}
                  role="option"
                  aria-selected={active}
                  className={`wm-select-option ${active ? 'active' : ''} ${focused ? 'focused' : ''}`}
                  onMouseEnter={() => setActiveIdx(idx)}
                  onClick={() => commit(idx)}
                >
                  {o.label}
                </button>
              </li>
            )
          })}
        </ul>
      ) : null}
    </div>
  )
}
