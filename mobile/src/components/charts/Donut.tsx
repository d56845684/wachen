/** 甜甜圈圖 — 對應 HTML 的 donut()（conic-gradient → svg arc） */
import React from 'react';
import { Text, View } from 'react-native';
import Svg, { Circle } from 'react-native-svg';
import { useTheme } from '@/theme/useTheme';

export interface DonutRow { name: string; value: number; color: string }

export function Donut({ rows, centerLabel, centerSub, size = 140 }: {
  rows: DonutRow[]; centerLabel: string; centerSub?: string; size?: number;
}) {
  const t = useTheme();
  const total = rows.reduce((a, r) => a + r.value, 0) || 1;
  const stroke = 18;
  const r = (size - stroke) / 2;
  const circ = 2 * Math.PI * r;
  let offset = 0;

  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 16 }}>
      <View style={{ width: size, height: size }}>
        <Svg width={size} height={size} style={{ transform: [{ rotate: '-90deg' }] }}>
          {rows.map((row) => {
            const frac = row.value / total;
            const el = (
              <Circle
                key={row.name}
                cx={size / 2} cy={size / 2} r={r}
                stroke={row.color} strokeWidth={stroke} fill="none"
                strokeDasharray={`${frac * circ} ${circ}`}
                strokeDashoffset={-offset * circ}
              />
            );
            offset += frac;
            return el;
          })}
        </Svg>
        <View style={{ position: 'absolute', inset: 0, alignItems: 'center', justifyContent: 'center' }}>
          <Text style={{ fontSize: 20, fontWeight: '800', color: t.ink }}>{centerLabel}</Text>
          {centerSub ? <Text style={{ fontSize: 10, color: t.muted }}>{centerSub}</Text> : null}
        </View>
      </View>
      <View style={{ flex: 1, gap: 4 }}>
        {rows.map((row) => (
          <View key={row.name} style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
            <View style={{ width: 8, height: 8, borderRadius: 2, backgroundColor: row.color }} />
            <Text style={{ fontSize: 12, color: t.ink2, flex: 1 }} numberOfLines={1}>{row.name}</Text>
            <Text style={{ fontSize: 12, color: t.muted, fontVariant: ['tabular-nums'] }}>
              {row.value} · {Math.round((row.value / total) * 100)}%
            </Text>
          </View>
        ))}
      </View>
    </View>
  );
}
