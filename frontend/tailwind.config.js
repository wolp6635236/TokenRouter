// 边线透明度与 /70 等修饰符相乘，避免修饰符把低对比边框重新提亮。
const darkEdgeColor = (opacity) => ({ opacityValue = '1' }) =>
  `rgb(252 252 254 / calc(${opacity} * ${opacityValue}))`

// 深色边线独立于文字和表面色阶，border、divide、ring 共用同一强度。
const darkEdges = {
  400: darkEdgeColor(0.3),
  500: darkEdgeColor(0.125),
  600: darkEdgeColor(0.078),
  700: darkEdgeColor(0.04),
  800: darkEdgeColor(0.031),
  900: darkEdgeColor(0.02)
}

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    // 语义圆角档位:紧凑元素、控件、内容表面、桌面弹窗。
    // 数值统一由 style.css 的 :root --radius-* 变量定义,raw CSS 与工具类同源。
    // 旧尺度 key(sm/md/lg/xl…)已删除,check:ui 门禁阻止其复活。
    borderRadius: {
      none: '0px',
      compact: 'var(--radius-compact)',
      control: 'var(--radius-control)',
      surface: 'var(--radius-surface)',
      dialog: 'var(--radius-dialog)',
      full: '9999px'
    },
    extend: {
      borderColor: { dark: darkEdges },
      divideColor: { dark: darkEdges },
      ringColor: { dark: darkEdges },
      opacity: {
        6: '0.06',
        8: '0.08'
      },
      // 浮层层级语义档:数值唯一来源是 style.css :root 的 --z-* 变量,这里只做 var() 引用。
      // --z-tour(driver.js 外部约束)有意不暴露为工具类,防止业务代码依附第三方层级。
      zIndex: {
        'chart-tooltip': 'var(--z-chart-tooltip)',
        'sidebar-overlay': 'var(--z-sidebar-overlay)',
        sidebar: 'var(--z-sidebar)',
        header: 'var(--z-header)',
        modal: 'var(--z-modal)',
        'modal-nested': 'var(--z-modal-nested)',
        tooltip: 'var(--z-tooltip)',
        announcement: 'var(--z-announcement)',
        'announcement-raised': 'var(--z-announcement-raised)',
        'announcement-top': 'var(--z-announcement-top)',
        'menu-overlay': 'var(--z-menu-overlay)',
        toast: 'var(--z-toast)',
        'action-menu': 'var(--z-action-menu)',
        'teleport-tooltip': 'var(--z-teleport-tooltip)',
        'help-tooltip': 'var(--z-help-tooltip)',
        'teleport-dropdown': 'var(--z-teleport-dropdown)'
      },
      // 浮层面板最大高度三档,数值同样由 style.css :root 变量承载。
      maxHeight: {
        'menu-sm': 'var(--max-h-menu-sm)',
        menu: 'var(--max-h-menu)',
        panel: 'var(--max-h-panel)'
      },
      colors: {
        // 主色调 - Blue Archive 蓝白主题
        primary: {
          50: '#F6FCFF',
          100: '#DDF4FC',
          200: '#BFEAFF',
          300: '#8BDDF8',
          400: '#4DD4F6',
          500: '#00D2FF',
          600: '#12A7E8',
          700: '#0B8FD8',
          800: '#176F9E',
          900: '#2D4F68',
          950: '#071A2A'
        },
        // 辅助色 - 冰白到品牌深蓝
        accent: {
          50: '#FFFFFF',
          100: '#EAF8FE',
          200: '#CDEFFD',
          300: '#9BDEFA',
          400: '#5BCDF2',
          500: '#1598D8',
          600: '#0B8FD8',
          700: '#176F9E',
          800: '#2D4F68',
          900: '#21465E',
          950: '#071A2A'
        },
        // 覆盖默认 gray/slate:Tailwind 默认值偏蓝(#1f2937/#0f172a 等),深色模式下会残留蓝调
        // 统一映射到中性 zinc 色相,与 dark 色阶同一体系
        gray: {
          50: '#FAFAFA',
          100: '#F4F4F5',
          200: '#E4E4E7',
          300: '#D4D4D8',
          400: '#A1A1AA',
          500: '#71717A',
          600: '#52525B',
          700: '#3F3F46',
          800: '#27272A',
          900: '#18181B',
          950: '#09090B'
        },
        slate: {
          50: '#FAFAFA',
          100: '#F4F4F5',
          200: '#E4E4E7',
          300: '#D4D4D8',
          400: '#A1A1AA',
          500: '#71717A',
          600: '#52525B',
          700: '#3F3F46',
          800: '#27272A',
          900: '#18181B',
          950: '#09090B'
        },
        // @project-doc docs/architecture/frontend_ui_conventions.md#dark_colors
        // 深色文字与表面色阶；边框强度由 darkEdges 单独定义。
        dark: {
          50: '#FAFAFA', // 标题与强调文字
          100: '#DEE0E2', // 正文文字、侧栏导航与顶栏图标默认色
          200: '#D4D4D8', // 次强文字、占位文字底色
          300: '#A1A1AA', // 次要文字
          400: '#8B8B94', // 辅助文字、表头文字
          500: '#5F5F67', // 图标、禁用文字
          600: '#3D3D42', // 较强中性填充
          700: '#27272A', // 中性填充：chip、禁用控件
          800: '#17171A', // 弱填充：表格行 hover、嵌套面板
          900: '#0F0F10', // 卡片、弹窗、下拉面板
          950: '#141416' // 控件底：输入框、次级按钮、Tab 轨道、行内代码
        }
      },
      fontFamily: {
        // 英文用 OpenRouter 的开源字体 Plus Jakarta Sans。
        // 中文在苹果设备上用系统自带的苹方，其他设备用字形接近的思源黑体网页字体。
        sans: [
          '"Plus Jakarta Sans Variable"',
          '"PingFang SC"',
          '"Noto Sans SC Variable"',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: [
          '"Geist Mono Variable"',
          'ui-monospace',
          'SFMono-Regular',
          'Menlo',
          'Monaco',
          'Consolas',
          'monospace'
        ]
      },
      boxShadow: {
        // 普通控件和结构表面只用边框分层；浮层、弹窗和品牌发光仍保留较强投影。
        DEFAULT: 'none',
        sm: 'none',
        glass: 'none',
        'glass-sm': 'none',
        glow: '0 0 20px rgba(0, 210, 255, 0.28)',
        'glow-lg': '0 0 40px rgba(18, 167, 232, 0.35)',
        card: 'none',
        'card-hover': 'none',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #00D2FF 0%, #0B8FD8 100%)',
        'gradient-dark': 'linear-gradient(135deg, #17171A 0%, #0A0A0B 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(0, 210, 255, 0.14) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(139, 221, 248, 0.12) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(18, 167, 232, 0.1) 0px, transparent 50%)'
      },
      // 动效变量由 style.css 持有，工具类只负责引用。
      transitionDuration: {
        DEFAULT: 'var(--motion-fast)',
        fast: 'var(--motion-fast)',
        normal: 'var(--motion-normal)',
        layout: 'var(--motion-layout)',
      },
      transitionTimingFunction: {
        DEFAULT: 'var(--motion-ease)',
        standard: 'var(--motion-ease)',
        exit: 'var(--motion-ease-exit)',
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 20px rgba(0, 210, 255, 0.28)' },
          '100%': { boxShadow: '0 0 30px rgba(18, 167, 232, 0.4)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      }
    }
  },
  plugins: []
}
