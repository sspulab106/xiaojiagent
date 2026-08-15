/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // 品牌（深靛蓝，在马卡龙背景上保持可读性）
        primary: { DEFAULT: '#4338CA', foreground: '#ffffff' },
        // 语义 token
        ink: '#111111',
        body: '#374151',
        muted: '#71717A',
        hairline: '#D4D4D8',
        surface: '#FFFFFF',
        well: '#F4F4F5',
        brand: '#4338CA',
        ok: '#16A34A',
        danger: '#DC2626',
        warn: '#F59E0B',
        // 马卡龙强调色（新粗野主义色板）
        mint: '#A3E635',
        lavender: '#C084FC',
        lemon: '#FDE047',
        peach: '#FDBA74',
        sage: '#86EFAC',
        // 重映射：灰色系 → 柔和中性
        slate: {
          50: '#F4F4F5',
          100: '#F4F4F5',
          200: '#E4E4E7',
          300: '#D4D4D8',
          400: '#A1A1AA',
          500: '#71717A',
          600: '#52525B',
          700: '#3F3F46',
          800: '#27272A',
          900: '#18181B',
        },
        // 重映射：蓝色系 → 靛蓝（与薰衣草紫协调）
        blue: {
          50: '#EEF2FF',
          100: '#E0E7FF',
          200: '#C7D2FE',
          300: '#A5B4FC',
          400: '#818CF8',
          500: '#6366F1',
          600: '#4F46E5',
          700: '#4338CA',
          800: '#3730A3',
          900: '#312E81',
        },
      },
      fontFamily: {
        sans: ['"IBM Plex Sans"', 'system-ui', '-apple-system', 'sans-serif'],
        mono: ['"IBM Plex Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      borderRadius: {
        sm: '8px',
        DEFAULT: '12px',
        md: '10px',
        lg: '14px',
        xl: '16px',
        '2xl': '20px',
        '3xl': '24px',
      },
      boxShadow: {
        // 新粗野主义：无模糊硬阴影
        hard: '4px 4px 0 0 #000',
        'hard-sm': '2px 2px 0 0 #000',
        'hard-md': '3px 3px 0 0 #000',
        'hard-lg': '6px 6px 0 0 #000',
        'hard-xl': '8px 8px 0 0 #000',
      },
      keyframes: {
        'pop-in': {
          '0%': { opacity: '0', transform: 'scale(0.94) translateY(10px)' },
          '100%': { opacity: '1', transform: 'scale(1) translateY(0)' },
        },
        'toast-in': {
          '0%': { opacity: '0', transform: 'translateY(14px) scale(0.95)' },
          '100%': { opacity: '1', transform: 'translateY(0) scale(1)' },
        },
        'wiggle': {
          '0%, 100%': { transform: 'rotate(-2deg)' },
          '50%': { transform: 'rotate(2deg)' },
        },
      },
      animation: {
        'pop-in': 'pop-in 0.18s ease-out',
        'toast-in': 'toast-in 0.22s ease-out',
        wiggle: 'wiggle 0.4s ease-in-out',
      },
    },
  },
  plugins: [],
}
