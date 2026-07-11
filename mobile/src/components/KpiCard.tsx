import React from 'react';
import { Text, View } from 'react-native';
import { PressableScale } from './PressableScale';
import { radius, space, type Palette } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';

export interface KpiCardProps {
  value: string | number;
  unit?: string;
  label: string;
  sub?: string;
  delta?: number;
  deltaUnit?: string;
  tone?: 'default' | 'alarm' | 'warn';
  synthetic?: boolean;   // PoC 模擬值標記
  onPress?: () => void;
}

export function KpiCard({ value, unit, label, sub, delta, deltaUnit = '', tone = 'default', synthetic, onPress }: KpiCardProps) {
  const t = useTheme();
  const valueColor = tone === 'alarm' ? t.critical : tone === 'warn' ? t.serious : t.ink;
  const body = (
    <View style={cardStyle(t)}>
      <View style={{ flexDirection: 'row', alignItems: 'baseline', gap: 4 }}>
        <Text style={{ fontSize: 24, fontWeight: '800', letterSpacing: -0.5, color: valueColor }}>{value}</Text>
        {unit ? <Text style={{ fontSize: 13, color: t.muted }}>{unit}</Text> : null}
        {synthetic ? <Text style={{ fontSize: 9, color: t.muted, borderWidth: 1, borderColor: t.border, borderRadius: 4, paddingHorizontal: 3 }}>模擬</Text> : null}
      </View>
      <Text style={{ fontSize: 12, color: t.ink2, marginTop: 2 }}>{label}</Text>
      {delta != null ? (
        <Text style={{ fontSize: 11, fontWeight: '700', marginTop: 2, color: delta >= 0 ? t.good : t.critical }}>
          {delta >= 0 ? '▲' : '▼'} {Math.abs(delta)}{deltaUnit}
        </Text>
      ) : null}
      {sub ? <Text style={{ fontSize: 11, color: t.muted, marginTop: 2 }}>{sub}</Text> : null}
    </View>
  );
  if (!onPress) return body;
  return <PressableScale onPress={onPress}>{body}</PressableScale>;
}

const cardStyle = (t: Palette) => ({
  backgroundColor: t.surface,
  borderRadius: radius.lg,
  borderWidth: 1,
  borderColor: t.border,
  padding: space.md,
  minWidth: 150,
  flexGrow: 1,
});
