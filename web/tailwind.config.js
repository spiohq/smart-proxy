/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{html,js,svelte,ts}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Surface hierarchy (Obsidian tonal layering)
        'surface-container-lowest': '#0e0e13',
        'surface': '#131318',
        'surface-dim': '#131318',
        'surface-container-low': '#1c1b21',
        'surface-container': '#201f25',
        'surface-container-high': '#2a292f',
        'surface-container-highest': '#35343a',
        'surface-bright': '#3a383f',
        'surface-variant': '#35343a',
        'surface-tint': '#ffb687',

        // On-surface text
        'on-background': '#e5e1e9',
        'on-surface': '#e5e1e9',
        'on-surface-variant': '#d8c2b7',
        'inverse-surface': '#e5e1e9',
        'inverse-on-surface': '#313036',

        // Primary (Amber/Copper)
        'primary': '#ffb687',
        'primary-container': '#d98f5d',
        'primary-fixed': '#ffdbc7',
        'primary-fixed-dim': '#ffb687',
        'on-primary': '#512400',
        'on-primary-container': '#5b2900',
        'on-primary-fixed': '#311300',
        'on-primary-fixed-variant': '#6f380d',
        'inverse-primary': '#8b4f23',

        // Secondary (Muted Teal)
        'secondary': '#8ad1e3',
        'secondary-container': '#00606f',
        'secondary-fixed': '#a7eeff',
        'secondary-fixed-dim': '#8ad1e3',
        'on-secondary': '#00363f',
        'on-secondary-container': '#90d8ea',
        'on-secondary-fixed': '#001f25',
        'on-secondary-fixed-variant': '#004e5b',

        // Tertiary
        'tertiary': '#80d3e1',
        'tertiary-container': '#57acb9',
        'tertiary-fixed': '#9cf0fd',
        'tertiary-fixed-dim': '#80d3e1',
        'on-tertiary': '#00363d',
        'on-tertiary-container': '#003d44',
        'on-tertiary-fixed': '#001f24',
        'on-tertiary-fixed-variant': '#004f57',

        // Error
        'error': '#ffb4ab',
        'error-container': '#93000a',
        'on-error': '#690005',
        'on-error-container': '#ffdad6',

        // Borders
        'outline': '#a08d82',
        'outline-variant': '#53443b',

        // Background alias
        'background': '#131318',
      },
      borderRadius: {
        DEFAULT: '0.125rem',
        'lg': '0.25rem',
        'xl': '0.5rem',
        'full': '0.75rem',
      },
      fontFamily: {
        headline: ['Manrope', 'sans-serif'],
        body: ['Inter', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
        label: ['Space Grotesk', 'sans-serif'],
        mono: ['JetBrains Mono', 'Fira Code', 'monospace'],
      }
    }
  },
  plugins: []
};
