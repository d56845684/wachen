/**
 * 水平長條圖 — 對應 HTML 的 barChart()。純 View 實作（無需 svg）。
 * 功能性圖表不做進場動畫（emil：banking app 的 functional graph 不該動）。
 */
import React from 'react';
import { Text, View } from 'react-native';
import { useTheme } from '@/theme/useTheme';

export interface BarRow { name: string; value: number; color?: string }

export function BarChart({ rows, showPct = true, total }: { rows: BarRow[]; showPct?: boolean; total?: number }) {
  const t = useTheme();
  const max = Math.max(...rows.map((r) => r.value), 1);
  const tot = total ?? rows.reduce((a, r) => a + r.value, 0);
  return (
    <View style={{ gap: 8 }}>
      {rows.map((r) => (
        <View key={r.name} style={{ flexDirection: 'row', alignItems: 'center', gap: 8 }}>
          <Text numberOfLines={1} style={{ width: 88, fontSize: 12, color: t.ink2 }}>{r.name}</Text>
          <View style={{ flex: 1, height: 8, borderRadius: 4, backgroundColor: t.grid, overflow: 'hidden' }}>
            <View style={{ width: `${(r.value / max) * 100}%`, height: '100%', borderRadius: 4, backgroundColor: r.color ?? t.s1 }} />
          </View>
          <Text style={{ fontSize: 12, color: t.ink, fontWeight: '600', minWidth: 56, textAlign: 'right', fontVariant: ['tabular-nums'] }}>
            {r.value}{showPct && tot ? ` · ${Math.round((r.value / tot) * 100)}%` : ''}
          </Text>
        </View>
      ))}
    </View>
  );
}
