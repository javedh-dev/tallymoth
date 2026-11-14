import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

// GitHub Pages serves a project site under /<repo>/. Override with DOCS_BASE=/ for a custom domain.
const base = process.env.DOCS_BASE ?? '/tallymoth/'

export default withMermaid(defineConfig({
  title: 'tallymoth',
  description: 'A tiny multi-tenant event service: append events, query aggregates. One static binary, SQLite storage.',
  base,
  cleanUrls: true,
  lastUpdated: true,
  appearance: 'dark',
  head: [['link', { rel: 'icon', type: 'image/svg+xml', href: `${base}logo.svg` }]],
  // CHANGELOG.md lives at the repo root, outside the site
  ignoreDeadLinks: [/CHANGELOG/],
  themeConfig: {
    logo: '/logo.svg',
    siteTitle: 'tallymoth',
    nav: [
      { text: 'Quick start', link: '/quick-start' },
      { text: 'Send events', link: '/send-events', activeMatch: '/send-events|/authentication|/signing' },
      { text: 'Query', link: '/query', activeMatch: '/query|/grafana' },
      { text: 'Run it', link: '/configuration', activeMatch: '/configuration|/docker|/docker-compose|/maintenance|/performance|/data-controls' },
      { text: 'Reference', link: '/errors', activeMatch: '/errors|/cli' },
      { text: 'Developers', link: '/architecture', activeMatch: '/architecture|/testing|/releasing' },
    ],
    sidebar: [
      {
        text: 'Getting started',
        items: [
          { text: 'Quick start', link: '/quick-start' },
          { text: 'Concepts', link: '/concepts' },
        ],
      },
      {
        text: 'Sending events',
        items: [
          { text: 'Send events', link: '/send-events' },
          { text: 'Authentication', link: '/authentication' },
          { text: 'Signing requests', link: '/signing' },
        ],
      },
      {
        text: 'Reading data',
        items: [
          { text: 'Query API', link: '/query' },
          { text: 'Grafana', link: '/grafana' },
        ],
      },
      {
        text: 'Running tallymoth',
        items: [
          { text: 'Configuration', link: '/configuration' },
          { text: 'Docker Compose guide', link: '/docker-compose' },
          { text: 'Docker and reverse proxy', link: '/docker' },
          { text: 'Upgrades and backups', link: '/maintenance' },
          { text: 'Performance', link: '/performance' },
          { text: 'Data controls', link: '/data-controls' },
        ],
      },
      {
        text: 'For developers',
        items: [
          { text: 'Architecture', link: '/architecture' },
          { text: 'Testing and benchmarks', link: '/testing' },
          { text: 'Releasing', link: '/releasing' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Error codes', link: '/errors' },
          { text: 'Command line', link: '/cli' },
          { text: 'Changelog', link: 'https://github.com/javedh-dev/tallymoth/blob/main/CHANGELOG.md' },
        ],
      },
    ],
    socialLinks: [{ icon: 'github', link: 'https://github.com/javedh-dev/tallymoth' }],
    search: { provider: 'local' },
    editLink: { pattern: 'https://github.com/javedh-dev/tallymoth/edit/main/docs/:path', text: 'Edit this page' },
    outline: [2, 3],
    footer: { message: 'Released under the MIT License.', copyright: 'tallymoth' },
  },
  // JetBrains Mono, which the theme waits for before drawing (see theme/index.ts)
  mermaid: { fontFamily: 'JetBrains Mono, ui-monospace, monospace', fontSize: 13, look: 'neo', flowchart: { padding: 16, nodeSpacing: 36, rankSpacing: 52, curve: 'basis' }, sequence: { actorMargin: 56, messageMargin: 32 } },
  vite: { optimizeDeps: { include: ['mermaid'] } },
}))
