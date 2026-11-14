// Click a diagram to see it large in a centred popup. Esc, the close button or a click outside closes it.
// Diagrams are drawn after the page loads and again on theme change, so this listens on the document
// instead of attaching to each one.

function open(source: Element) {
  const svg = source.querySelector('svg')
  if (!svg) return

  const overlay = document.createElement('div')
  overlay.className = 'tm-zoom'
  overlay.setAttribute('role', 'dialog')
  overlay.setAttribute('aria-modal', 'true')
  overlay.setAttribute('aria-label', 'Diagram, enlarged')

  const card = document.createElement('div')
  card.className = 'tm-zoom-card'
  const close = document.createElement('button')
  close.className = 'tm-zoom-close'
  close.type = 'button'
  close.setAttribute('aria-label', 'Close')
  close.textContent = '\u00d7'

  const copy = svg.cloneNode(true) as SVGElement
  copy.removeAttribute('width')
  copy.removeAttribute('height')
  copy.style.maxWidth = 'none'
  copy.style.width = '100%'
  copy.style.height = 'auto'
  copy.style.maxHeight = 'calc(100vh - 150px)' // fit the screen; the drawing keeps its proportions
  // the .mermaid class carries the diagram styles (colours, fonts), so the copy needs it too
  const holder = document.createElement('div')
  holder.className = 'mermaid tm-zoom-diagram'
  holder.append(copy)
  card.append(close, holder)
  overlay.append(card)

  const previouslyFocused = document.activeElement as HTMLElement | null
  const shut = () => {
    overlay.remove()
    document.body.classList.remove('tm-zoom-open')
    document.removeEventListener('keydown', onKey, true)
    previouslyFocused?.focus?.()
  }
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); shut() }
    if (e.key === 'Tab') { e.preventDefault(); close.focus() } // only one thing to focus
  }
  overlay.addEventListener('click', (e) => { if (e.target === overlay) shut() })
  close.addEventListener('click', shut)
  document.addEventListener('keydown', onKey, true)

  document.body.append(overlay)
  document.body.classList.add('tm-zoom-open')
  close.focus()
}

export function enableDiagramZoom() {
  document.addEventListener('click', (e) => {
    const el = (e.target as Element).closest('.mermaid')
    if (el && !el.closest('.tm-zoom') && el.querySelector('svg')) open(el)
  })
  document.addEventListener('keydown', (e) => {
    if ((e.key === 'Enter' || e.key === ' ') && (e.target as Element).classList?.contains('mermaid')) {
      e.preventDefault()
      open(e.target as Element)
    }
  })
  // make each diagram reachable and announced as a button once it has been drawn
  const label = (root: ParentNode) =>
    root.querySelectorAll?.('.mermaid').forEach((d) => {
      if (d.getAttribute('role') || d.closest('.tm-zoom')) return
      d.setAttribute('role', 'button')
      d.setAttribute('tabindex', '0')
      d.setAttribute('aria-label', 'Diagram. Press Enter to enlarge.')
    })
  new MutationObserver(() => label(document)).observe(document.body, { childList: true, subtree: true })
  label(document)
}
