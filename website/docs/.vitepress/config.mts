import { defineConfig } from 'vitepress'
import { checkAnchors } from './anchors'

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: 'Voltaire',
  description:
    'TDP, fan curves, RGB lighting, and system control for the ASUS ROG Flow Z13 (2025) and OneXPlayer X2 Mini Pro on Linux.',
  // Served from https://dahui.github.io/voltaire/. Write internal links
  // without the base (/reference/commands); VitePress adds it.
  base: '/voltaire/',
  // /voltaire/installation rather than /voltaire/installation.html; GitHub
  // Pages serves the .html file for the extensionless path.
  cleanUrls: true,
  head: [['link', { rel: 'icon', type: 'image/svg+xml', href: '/voltaire/favicon.svg' }]],

  markdown: {
    // The pages use ::: info / tip / warning / danger; name them the way the
    // prose does.
    container: {
      infoLabel: 'Note',
      tipLabel: 'Tip',
      warningLabel: 'Caution',
      dangerLabel: 'Danger',
    },
  },

  // VitePress fails the build on a link to a missing page; this adds the
  // same check for #fragments, which it does not look at.
  buildEnd: checkAnchors,

  themeConfig: {
    outline: { level: [2, 3], label: 'On this page' },
    search: { provider: 'local' },
    socialLinks: [{ icon: 'github', link: 'https://github.com/dahui/voltaire' }],
    editLink: {
      pattern: 'https://github.com/dahui/voltaire/edit/main/website/docs/:path',
      text: 'Edit page',
    },

    // The table of contents, shown on every page including the overview at
    // the site root (there is no separate landing page).
    sidebar: [
      {
        text: 'Start Here',
        items: [
          { text: 'Overview', link: '/' },
          { text: 'Installation', link: '/installation' },
          { text: 'Quick Start', link: '/quick-start' },
          { text: 'Migrating from z13ctl', link: '/migrating-from-z13ctl' },
          { text: 'Using InputPlumber', link: '/inputplumber' },
        ],
      },
      {
        text: 'Devices',
        items: [
          { text: 'Supported devices', link: '/devices/' },
          { text: 'ASUS ROG Flow Z13 (2025)', link: '/devices/asus-rog-flow-z13-2025' },
          { text: 'OneXPlayer X2 Mini Pro', link: '/devices/onexplayer-x2-mini-pro' },
        ],
      },
      {
        text: 'The GUI',
        items: [
          { text: 'Overview', link: '/gui/' },
          { text: 'Quick Start', link: '/gui/quick-start' },
          { text: 'Configuration', link: '/gui/configuration' },
          { text: 'Theming', link: '/gui/theming' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Commands', link: '/reference/commands' },
          { text: 'Daemon & Socket Protocol', link: '/reference/daemon' },
          { text: 'Go API Module', link: '/reference/api' },
          { text: 'Go API Reference', link: '/reference/api-go' },
          { text: 'HID Protocol', link: '/reference/protocol' },
        ],
      },
      {
        text: 'Developers',
        items: [
          { text: 'Architecture', link: '/dev/architecture' },
          { text: 'Device Files', link: '/dev/device-files' },
          { text: 'Writing a Driver', link: '/dev/writing-a-driver' },
          { text: 'Plugins (Planned)', link: '/dev/plugins' },
        ],
      },
      {
        text: 'Project',
        items: [
          { text: 'Contributing', link: '/contributing' },
          { text: 'Z13 Hardware Smoke Test', link: '/dev/smoke-z13' },
        ],
      },
    ],
  },
})
