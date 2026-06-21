/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        macaron: {
          orange: '#F5C6A0',
          peach: '#FDDCB5',
          cream: '#FFF5E6',
          rose: '#F8D4D4',
          mint: '#D4F0E8',
          lavender: '#E4D8F0',
          sky: '#D4E8F8',
        },
      },
    },
  },
  plugins: [],
}