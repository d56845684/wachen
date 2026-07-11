/**
 * 設計 tokens — 由 瓦城顧客體驗中台.html 的 CSS variables 移植。
 * light / dark 兩組，經 useTheme() 依系統外觀取用。
 */
export type Palette = typeof light;

export const light = {
  page: '#f4f4f2',
  surface: '#fcfcfb',
  surface2: '#ffffff',
  raised: '#ffffff',
  ink: '#0b0b0b',
  ink2: '#52514e',
  muted: '#898781',
  grid: '#e6e5df',
  baseline: '#c3c2b7',
  border: 'rgba(11,11,11,0.10)',
  good: '#0ca30c',
  warning: '#fab219',
  serious: '#ec835a',
  critical: '#d03b3b',
  // 類別色（chart series）
  s1: '#2a78d6', s2: '#1baf7a', s3: '#eda100', s4: '#008300',
  s5: '#4a3aa7', s6: '#e34948', s7: '#e87ba4', s8: '#eb6834',
  // 順序色（單色階）
  seq: ['#cde2fb', '#9ec5f4', '#5598e7', '#2a78d6', '#184f95'],
  tabBar: 'rgba(252,252,251,0.82)', // 半透明 chrome，內容從底下滑過（apple-design §12）
};

export const dark: Palette = {
  page: '#0d0d0d',
  surface: '#1a1a19',
  surface2: '#201f1e',
  raised: '#232220',
  ink: '#ffffff',
  ink2: '#c3c2b7',
  muted: '#898781',
  grid: '#2c2c2a',
  baseline: '#383835',
  border: 'rgba(255,255,255,0.10)',
  good: '#0ca30c',
  warning: '#fab219',
  serious: '#ec835a',
  critical: '#d03b3b',
  s1: '#3987e5', s2: '#199e70', s3: '#c98500', s4: '#008300',
  s5: '#9085e9', s6: '#e66767', s7: '#d55181', s8: '#d95926',
  seq: ['#184f95', '#256abf', '#3987e5', '#6da7ec', '#9ec5f4'],
  tabBar: 'rgba(26,26,25,0.82)',
};

export const seriesColors = (p: Palette) => [p.s1, p.s2, p.s3, p.s4, p.s5, p.s6, p.s7, p.s8];

/** 間距採 4pt 網格；字級對應 HTML 的層級但按行動端可讀性微調 */
export const space = { xs: 4, sm: 8, md: 12, lg: 16, xl: 20, xxl: 28 } as const;

export const radius = { sm: 8, md: 10, lg: 14, xl: 18, pill: 999 } as const;

/**
 * Typography — apple-design §15：
 * 大字負 tracking、緊 leading；內文 tracking 0、leading 1.5。
 * 字級用相對倍率配合系統字體縮放（Dynamic Type）。
 */
export const type = {
  display: { fontSize: 26, lineHeight: 30, letterSpacing: -0.5, fontWeight: '800' as const },
  title:   { fontSize: 19, lineHeight: 24, letterSpacing: -0.3, fontWeight: '700' as const },
  heading: { fontSize: 15, lineHeight: 20, letterSpacing: -0.1, fontWeight: '700' as const },
  body:    { fontSize: 14.5, lineHeight: 22, letterSpacing: 0, fontWeight: '400' as const },
  caption: { fontSize: 12, lineHeight: 16, letterSpacing: 0, fontWeight: '400' as const },
  label:   { fontSize: 10.5, lineHeight: 14, letterSpacing: 1, fontWeight: '700' as const, textTransform: 'uppercase' as const },
} as const;
