/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        macaron: {
          orange: 'rgb(var(--color-orange) / <alpha-value>)',
          peach: 'rgb(var(--color-peach) / <alpha-value>)',
          cream: 'rgb(var(--color-cream) / <alpha-value>)',
          rose: 'rgb(var(--color-rose) / <alpha-value>)',
          mint: 'rgb(var(--color-mint) / <alpha-value>)',
          lavender: 'rgb(var(--color-lavender) / <alpha-value>)',
          sky: 'rgb(var(--color-sky) / <alpha-value>)',
        },
      },
    },
  },
  plugins: [],
}