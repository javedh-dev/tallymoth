import DefaultTheme from 'vitepress/theme'
import '@fontsource/noto-sans/400.css'
import '@fontsource/noto-sans/500.css'
import '@fontsource/noto-sans/600.css'
import '@fontsource/noto-sans/700.css'
import '@fontsource/jetbrains-mono/400.css'
import '@fontsource/jetbrains-mono/500.css'
import '@fontsource/jetbrains-mono/700.css'
import './custom.css'
import { enableDiagramZoom } from './diagram-zoom'

export default {
  extends: DefaultTheme,
  // Diagrams are measured when they are drawn. If their font is not loaded yet they are measured in a fallback font,
  // and the text is clipped once the real one arrives. So wait for it (at most 2 s) before the page is shown.
  async enhanceApp() {
    if (typeof document === 'undefined') return
    enableDiagramZoom()
    if (!document.fonts) return
    const wait = Promise.all(['400 13px JetBrains Mono', '500 13px JetBrains Mono', '400 16px Noto Sans', '600 16px Noto Sans'].map((f) => document.fonts.load(f)))
    await Promise.race([wait, new Promise((r) => setTimeout(r, 2000))])
  },
}
