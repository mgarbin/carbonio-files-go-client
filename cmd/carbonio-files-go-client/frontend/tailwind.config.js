/** @type {import('tailwindcss').Config} */
export default {
  // Theme switching toggles a `.dark` class on <html> (see src/lib/theme.js)
  // instead of following the OS preference unconditionally, so the user's
  // choice (light / dark / system) sticks across launches.
  darkMode: "class",
  content: ["./index.html", "./src/**/*.{js,svelte}"],
  theme: {
    extend: {
      // Every semantic color resolves through a CSS custom property (see
      // src/app.css's `:root` / `.dark` blocks) instead of a fixed hex
      // value. Adding a new theme is then just a new class block that
      // redefines these variables - no Tailwind config or component
      // changes required. The `rgb(var(...) / <alpha>)` form keeps
      // Tailwind's opacity modifiers (e.g. `bg-brand/10`) working.
      colors: {
        brand: {
          DEFAULT: withOpacity("--color-brand"),
          dark: withOpacity("--color-brand-dark"),
        },
        surface: withOpacity("--color-surface"),
        bg: withOpacity("--color-bg"),
        border: withOpacity("--color-border"),
        text: withOpacity("--color-text"),
        muted: withOpacity("--color-muted"),
        danger: {
          DEFAULT: withOpacity("--color-danger"),
          bg: withOpacity("--color-danger-bg"),
          border: withOpacity("--color-danger-border"),
        },
        success: {
          DEFAULT: withOpacity("--color-success"),
          bg: withOpacity("--color-success-bg"),
          border: withOpacity("--color-success-border"),
        },
        warning: {
          DEFAULT: withOpacity("--color-warning"),
          bg: withOpacity("--color-warning-bg"),
          border: withOpacity("--color-warning-border"),
        },
      },
      borderRadius: {
        DEFAULT: "var(--radius)",
      },
      fontFamily: {
        sans: ["-apple-system", "Segoe UI", "Roboto", "Helvetica", "Arial", "sans-serif"],
      },
    },
  },
  plugins: [],
};

function withOpacity(variable) {
  return ({ opacityValue }) =>
    opacityValue === undefined ? `rgb(var(${variable}))` : `rgb(var(${variable}) / ${opacityValue})`;
}
