import { useState, type FocusEvent } from 'react'

import type { SelectOption } from './controlOptions'

type CustomSelectProps<T extends string> = {
  value: T
  options: SelectOption<T>[]
  disabled: boolean
  label: string
  onChange: (value: T) => void
}

export function CustomSelect<T extends string>({
  value,
  options,
  disabled,
  label,
  onChange,
}: CustomSelectProps<T>) {
  const [isOpen, setIsOpen] = useState(false)
  const selectedOption = options.find((option) => option.value === value)

  const handleBlur = (event: FocusEvent<HTMLDivElement>) => {
    if (!event.currentTarget.contains(event.relatedTarget)) {
      setIsOpen(false)
    }
  }

  return (
    <div className="custom-select" data-open={isOpen} onBlur={handleBlur}>
      <button
        type="button"
        className="custom-select-trigger"
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        disabled={disabled}
        onClick={() => setIsOpen((current) => !current)}
      >
        <span>{selectedOption?.label ?? label}</span>
        <span className="custom-select-chevron" aria-hidden="true" />
      </button>
      {isOpen && !disabled ? (
        <div className="custom-select-menu" role="listbox" aria-label={label}>
          {options.map((option) => (
            <button
              key={option.value}
              type="button"
              role="option"
              aria-selected={option.value === value}
              className="custom-select-option"
              onClick={() => {
                onChange(option.value)
                setIsOpen(false)
              }}
            >
              {option.label}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
