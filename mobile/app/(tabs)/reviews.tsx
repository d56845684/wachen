/** 負評管理 — HTML PAGES.reviews：搜尋 + 快速 filter chips + 卡片列表 */
import React from 'react';
import { FlatList, Text, TextInput, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { CaseListItem } from '@/components/CaseListItem';
import { PressableScale } from '@/components/PressableScale';
import { useCases } from '@/api/queries';
import { applyFilters, useFilters } from '@/state/filters';
import { radius, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';
import type { RiskLevel } from '@/types/domain';

const RISK_CHIPS: { label: string; value: RiskLevel | '' }[] = [
  { label: '全部', value: '' },
  { label: '高風險', value: 'high' },
  { label: '中風險', value: 'medium' },
  { label: '低風險', value: 'low' },
];

export default function Reviews() {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const { data: cases = [], isLoading } = useCases();
  const filters = useFilters();
  const rows = applyFilters(cases, filters);

  return (
    <View style={{ flex: 1, backgroundColor: t.page, paddingTop: insets.top + space.md }}>
      <View style={{ paddingHorizontal: space.lg, gap: space.sm }}>
        <Text style={[type.display, { color: t.ink }]}>負評管理</Text>
        <TextInput
          placeholder="搜尋門市 / 關鍵字 / 顧客"
          placeholderTextColor={t.muted}
          value={filters.q}
          onChangeText={(v) => filters.set('q', v)}
          style={{
            borderWidth: 1, borderColor: t.border, borderRadius: radius.md,
            padding: 10, color: t.ink, backgroundColor: t.surface,
          }}
        />
        <View style={{ flexDirection: 'row', gap: space.sm, marginBottom: space.sm }}>
          {RISK_CHIPS.map((chip) => {
            const on = filters.risk === chip.value;
            return (
              <PressableScale
                key={chip.label}
                onPress={() => filters.set('risk', chip.value)}
                style={{
                  paddingHorizontal: 12, paddingVertical: 6, borderRadius: radius.pill,
                  backgroundColor: on ? t.s1 : t.surface,
                  borderWidth: 1, borderColor: on ? t.s1 : t.border,
                }}
              >
                <Text style={{ color: on ? '#fff' : t.ink2, fontSize: 12.5, fontWeight: '600' }}>{chip.label}</Text>
              </PressableScale>
            );
          })}
        </View>
      </View>
      <FlatList
        data={rows}
        keyExtractor={(c) => c.id}
        renderItem={({ item }) => <CaseListItem item={item} />}
        contentContainerStyle={{ paddingBottom: 96 }}
        ListHeaderComponent={
          <Text style={{ color: t.muted, fontSize: 12, paddingHorizontal: space.lg, paddingBottom: space.sm }}>
            共 {rows.length} 則評論
          </Text>
        }
        ListEmptyComponent={
          <Text style={{ color: t.muted, textAlign: 'center', marginTop: 40 }}>
            {isLoading ? '載入中…' : '沒有符合條件的評論'}
          </Text>
        }
      />
    </View>
  );
}
