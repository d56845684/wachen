/**
 * Motion tokens — 全 app 動畫只從這裡取值，不得散落 magic number。
 *
 * 規則來源：.claude/skills/emil-design-eng 與 .claude/skills/apple-design。
 * - 進出場一律 ease-out（強化曲線，非內建弱曲線）；絕不用 ease-in。
 * - UI 動畫 < 300ms；按壓回饋 100–160ms。
 * - 手勢驅動（sheet 拖曳、swipe）用 spring：預設 critically damped（無彈跳），
 *   只有帶動量的手勢（甩、拖放開）才允許 bounce ~0.15。
 * - 高頻操作（tab 切換、鍵盤觸發）不做動畫。
 * - 進場不得從 scale 0 開始（最低 0.95 + opacity 0）。
 * - 進與出走同一路徑（spatial consistency）。
 * - 尊重 useReducedMotion()：降級為純 opacity cross-fade。
 */
import { Easing } from 'react-native-reanimated';

export const duration = {
  press: 140,   // 按壓回饋
  fast: 180,    // tooltip / 小元件
  base: 220,    // dropdown、列表項進場
  sheet: 320,   // modal / bottom sheet
} as const;

/** 強 ease-out（cubic-bezier(0.23,1,0.32,1)）— 進出場預設 */
export const easeOut = Easing.bezier(0.23, 1, 0.32, 1);
/** 強 ease-in-out（cubic-bezier(0.77,0,0.175,1)）— 畫面上移動/變形 */
export const easeInOut = Easing.bezier(0.77, 0, 0.175, 1);
/** iOS drawer 曲線（cubic-bezier(0.32,0.72,0,1)）— sheet 非手勢收合 */
export const easeDrawer = Easing.bezier(0.32, 0.72, 0, 1);

/**
 * Spring 設定（Reanimated withSpring）。
 * 對應 apple-design 的 damping/response 表：
 *   預設 UI：damping 1.0（無過衝）、response ~0.35s
 *   sheet／帶動量手勢：damping ~0.8、response ~0.3s
 */
export const spring = {
  default: { damping: 30, stiffness: 300, mass: 1 },          // 臨界阻尼、無彈跳
  sheet: { damping: 26, stiffness: 320, mass: 1 },            // 輕微過衝，only after 手勢
  momentum: (velocity: number) => ({ damping: 24, stiffness: 300, mass: 1, velocity }),
} as const;

/** 進場初始狀態：不從 scale(0) 出現 */
export const enterFrom = { opacity: 0, scale: 0.97, translateY: 8 } as const;

/** 列表 stagger：30–80ms 之間，裝飾性、不得阻擋互動 */
export const staggerMs = 40;
