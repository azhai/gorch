import m from 'mithril'

export interface TextFieldAttrs {
  label?: string
  hint?: string
  value: string
  placeholder?: string
  type?: string
  class?: string
  maxlength?: number
  oninput: (value: string) => void
}

export interface SelectFieldAttrs {
  label?: string
  hint?: string
  value: string
  options: { value: string; label: string }[]
  class?: string
  ariaLabel?: string
  onchange: (value: string) => void
}

function label(hint: string | undefined, text: string | undefined) {
  if (!text && !hint) return null
  return (
    <label class="md-label">
      {text}
      {hint && <span class="md-label__hint"> ({hint})</span>}
    </label>
  )
}

/** Labelled single-line input used throughout the config editor. */
export const TextField: m.Component<TextFieldAttrs> = {
  view(vnode) {
    const a = vnode.attrs
    return (
      <div>
        {label(a.hint, a.label)}
        <input
          class={a.class ?? 'md-field'}
          type={a.type ?? 'text'}
          value={a.value}
          placeholder={a.placeholder}
          maxlength={a.maxlength}
          oninput={(e: Event) => a.oninput((e.target as HTMLInputElement).value)}
        />
      </div>
    )
  },
}

/** Labelled <select> with the same visual language as TextField. */
export const SelectField: m.Component<SelectFieldAttrs> = {
  view(vnode) {
    const a = vnode.attrs
    return (
      <div>
        {label(a.hint, a.label)}
        <select
          class={a.class ?? 'md-field'}
          aria-label={a.ariaLabel}
          value={a.value}
          onchange={(e: Event) => a.onchange((e.target as HTMLSelectElement).value)}
        >
          {a.options.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </div>
    )
  },
}
