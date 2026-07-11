/** 折線圖 — 對應 HTML 的 lineChart()（svg polyline + 格線 + 圖例） */
import React from 'react';
import { Text, View, useWindowDimensions } from 'react-native';
import Svg, { Circle, Line, Polyline, Text as SvgText } from 'react-native-svg';
import { useTheme } from '@/theme/useTheme';

export interface LineSeries { label: string; color: string; data: number[] }

export function LineChart({ series, labels, height = 180 }: { series: LineSeries[]; labels: string[]; height?: number }) {
  const t = useTheme();
  const { width } = useWindowDimensions();
  const W = width - 64;
  const pl = 34, pr = 8, pt = 10, pb = 22;
  const n = labels.length;
  const all = series.flatMap((s) => s.data);
  const max = Math.max(...all, 1);
  const min = Math.min(0, ...all);
  const X = (i: number) => pl + (W - pl - pr) * (n <= 1 ? 0 : i / (n - 1));
  const Y = (v: number) => pt + (height - pt - pb) * (1 - (v - min) / (max - min || 1));

  return (
    <View>
      <Svg width={W} height={height}>
        {[0, 0.25, 0.5, 0.75, 1].map((f) => {
          const y = pt + (height - pt - pb) * f;
          return (
            <React.Fragment key={f}>
              <Line x1={pl} y1={y} x2={W - pr} y2={y} stroke={t.grid} strokeWidth={1} />
              <SvgText x={2} y={y + 3} fontSize={9} fill={t.muted}>{Math.round(max - (max - min) * f)}</SvgText>
            </React.Fragment>
          );
        })}
        {series.map((s) => (
          <React.Fragment key={s.label}>
            <Polyline
              fill="none" stroke={s.color} strokeWidth={2}
              points={s.data.map((v, i) => `${X(i)},${Y(v)}`).join(' ')}
            />
            {s.data.map((v, i) => (
              <Circle key={i} cx={X(i)} cy={Y(v)} r={2.5} fill={s.color} />
            ))}
          </React.Fragment>
        ))}
        {labels.map((l, i) =>
          n > 8 && i % 2 ? null : (
            <SvgText key={i} x={X(i)} y={height - 6} fontSize={9} fill={t.muted} textAnchor="middle">{l}</SvgText>
          ),
        )}
      </Svg>
      {series.length > 1 ? (
        <View style={{ flexDirection: 'row', gap: 14, marginTop: 4 }}>
          {series.map((s) => (
            <View key={s.label} style={{ flexDirection: 'row', alignItems: 'center', gap: 5 }}>
              <View style={{ width: 8, height: 8, borderRadius: 2, backgroundColor: s.color }} />
              <Text style={{ fontSize: 11, color: t.ink2 }}>{s.label}</Text>
            </View>
          ))}
        </View>
      ) : null}
    </View>
  );
}
