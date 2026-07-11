/** 門市詳情 — HTML openStore() 的行動端版：本店 KPI + 近期案件 */
import React from 'react';
import { FlatList, Text, View } from 'react-native';
import { Stack, useLocalSearchParams } from 'expo-router';
import { CaseListItem } from '@/components/CaseListItem';
import { KpiCard } from '@/components/KpiCard';
import { useCases, useFacets } from '@/api/queries';
import { space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';

export default function StoreDetail() {
  const t = useTheme();
  const { code } = useLocalSearchParams<{ code: string }>();
  const { data: facets } = useFacets();
  const { data: cases = [] } = useCases();

  const store = facets?.stores.find((s) => s.code === code);
  const rows = cases
    .filter((c) => c.store_code === code)
    .sort((a, b) => b.posted_at.localeCompare(a.posted_at));

  return (
    <>
      <Stack.Screen options={{ headerShown: true, title: store?.store ?? '門市' }} />
      <View style={{ flex: 1, backgroundColor: t.page }}>
        {store ? (
          <View style={{ padding: space.lg, gap: space.sm }}>
            <Text style={[type.title, { color: t.ink }]}>{store.store}</Text>
            <Text style={{ color: t.muted, fontSize: 12 }}>
              {store.brand} · {store.region} · 店經理 {store.manager}
            </Text>
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.sm }}>
              <KpiCard value={store.avg_rating} label="平均評分" delta={store.trend} />
              <KpiCard value={store.neg} label="負評數" tone="alarm" sub={`負評率 ${store.neg_rate}%`} />
              <KpiCard value={store.sla_rate} unit="%" label="SLA 達成" />
            </View>
          </View>
        ) : null}
        <FlatList
          data={rows}
          keyExtractor={(c) => c.id}
          renderItem={({ item }) => <CaseListItem item={item} />}
          contentContainerStyle={{ paddingBottom: 48 }}
        />
      </View>
    </>
  );
}
