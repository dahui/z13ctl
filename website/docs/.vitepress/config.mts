import { defineConfig } from 'vitepress'
import { checkAnchors } from './anchors'

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: 'Voltaire',
  description:
    'Power limits, fan curves, RGB lighting, charge limit and more for the ASUS ROG Flow Z13 (2025), the ASUS ROG Xbox Ally X and the OneXPlayer X2 Mini Pro on Linux.',
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
        text: 'Introduction',
        items: [{ text: 'Overview', link: '/' }],
      },
      {
        text: 'Supported Devices',
        items: [
          { text: 'Compatibility', link: '/devices/' },
          { text: 'ASUS ROG Flow Z13 (2025)', link: '/devices/asus-rog-flow-z13-2025' },
          { text: 'ASUS ROG Xbox Ally X', link: '/devices/asus-rog-xbox-ally-x' },
          { text: 'OneXPlayer X2 Mini Pro', link: '/devices/onexplayer-x2-mini-pro' },
        ],
      },
      {
        text: 'Getting Started',
        items: [
          { text: 'Installation', link: '/installation' },
          { text: 'Quick Start', link: '/quick-start' },
          { text: 'Upgrading from z13ctl', link: '/migrating-from-z13ctl' },
        ],
      },
      {
        text: 'User Guide',
        items: [
          { text: 'The GUI', link: '/guide/gui' },
          { text: 'Profiles and Power', link: '/guide/profiles-and-power' },
          { text: 'Fans', link: '/guide/fans' },
          { text: 'Lighting', link: '/guide/lighting' },
          { text: 'Battery', link: '/guide/battery' },
          { text: 'Buttons and Controllers', link: '/guide/buttons-and-controllers' },
          { text: 'Steam Gaming Mode', link: '/guide/gaming-mode' },
          { text: 'Monitoring', link: '/guide/monitoring' },
          { text: 'Customizing the GUI', link: '/guide/gui-configuration' },
          { text: 'Themes', link: '/guide/themes' },
          { text: 'Troubleshooting', link: '/troubleshooting' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Commands', link: '/reference/commands' },
          { text: 'Configuration Files', link: '/reference/configuration' },
          { text: 'The Daemon', link: '/reference/daemon' },
        ],
      },
      {
        text: 'Developer Guide',
        collapsed: true,
        items: [
          { text: 'Contributing', link: '/dev/contributing' },
          { text: 'Architecture', link: '/dev/architecture' },
          { text: 'Device Files', link: '/dev/device-files' },
          { text: 'Writing a Driver', link: '/dev/writing-a-driver' },
          { text: 'Socket Protocol', link: '/dev/socket-protocol' },
          { text: 'Go API Module', link: '/dev/go-api' },
          { text: 'Go API Reference', link: '/dev/api-go' },
          { text: 'HID Protocol', link: '/dev/hid-protocol' },
          { text: 'Plugins (Planned)', link: '/dev/plugins' },
        ],
      },
    ],
  },
})
