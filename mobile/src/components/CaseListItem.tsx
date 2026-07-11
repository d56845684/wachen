/**
 * 案件/負評列表項 — HTML 的 table row 在行動端改為卡片列。
 * 點擊推入 /case/[id]（進出同路徑：由右進、往右出）。
 */
import React from 'react';
import { Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { PressableScale } from './PressableScale';
import { RiskBadge, StatusPill } from './Badge';
import { SlaCountdown } from './SlaCountdown';
import { radius, space } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';
import type { Case } from '@/types/domain';

export function CaseListItem({ item }: { item: Case }) {
  const t = useTheme();
  const router = useRouter();
  return (
    <PressableScale
      scaleTo={0.98}
      onPress={() => router.push(`/case/${item.id}`)}
      style={{
        backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border,
        padding: space.md, marginHorizontal: space.lg, marginBottom: space.sm, gap: 6,
      }}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
        <Text style={{ color: t.warning, fontSize: 12 }}>
          {'★'.repeat(item.rating)}<Text style={{ color: t.grid }}>{'★'.repeat(5 - item.rating)}</Text>
        </Text>
        <Text style={{ color: t.muted, fontSize: 11 }}>{item.platform} · {item.posted_at.slice(0, 10)}</Text>
        <View style={{ flex: 1 }} />
        <SlaCountdown item={item} />
      </View>
      <Text numberOfLines={2} style={{ color: t.ink, fontSize: 14, lineHeight: 20, fontWeight: '600' }}>
        {item.summary}
      </Text>
      <Text style={{ color: t.muted, fontSize: 12 }} numberOfLines={1}>
        {item.brand_short} · {item.store.replace(item.brand, '').replace(/^[ -]+/, '')}
      </Text>
      <View style={{ flexDirection: 'row', gap: 6, alignItems: 'center' }}>
        <RiskBadge level={item.risk_level} />
        <StatusPill status={item.cstatus} />
        {item.categories[0] ? <Text style={{ color: t.ink2, fontSize: 11 }}>{item.categories[0]}</Text> : null}
      </View>
    </PressableScale>
  );
}
