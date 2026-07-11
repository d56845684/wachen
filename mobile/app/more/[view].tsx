/**
 * 次要功能頁（SLA 監控 / 門市管理 / 原因分析 / 聲量 / AI 洞察 / 任務 / 成效 / 報表 / 規則 / 組織 / 來源）。
 * PoC 階段先做骨架＋各頁資料摘要；正式版逐頁拆檔實作（見 mobile/ARCHITECTURE.md 路線圖）。
 */
import React from 'react';
import { ScrollView, Text, View } from 'react-native';
import { Stack, useLocalSearchParams } from 'expo-router';
import { useFacets } from '@/api/queries';
import { MORE_MENU } from '@/auth/roles';
import { radius, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';

export default function MoreView() {
  const t = useTheme();
  const { view } = useLocalSearchParams<{ view: string }>();
  const { data: facets } = useFacets();
  const menu = MORE_MENU.find((m) => m.id === view);

  return (
    <>
      <Stack.Screen options={{ headerShown: true, title: menu?.title ?? '功能' }} />
      <ScrollView style={{ flex: 1, backgroundColor: t.page }} contentContainerStyle={{ padding: space.lg, gap: space.md }}>
        <Text style={[type.title, { color: t.ink }]}>{menu?.icon} {menu?.title}</Text>

        {view === 'stores' && facets ? (
          facets.stores.map((s) => (
            <View key={s.code} style={{ backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border, padding: space.md, gap: 4 }}>
              <Text style={{ color: t.ink, fontWeight: '700' }}>{s.store}</Text>
              <Text style={{ color: t.muted, fontSize: 12 }}>
                {s.brand_short} · {s.region} · 評分 {s.avg_rating} · 負評率 {s.neg_rate}% · SLA {s.sla_rate}%
              </Text>
            </View>
          ))
        ) : view === 'sources' && facets ? (
          facets.sources.map((s) => (
            <View key={s.name} style={{ backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border, padding: space.md, gap: 4 }}>
              <Text style={{ color: t.ink, fontWeight: '700' }}>{s.name}</Text>
              <Text style={{ color: t.muted, fontSize: 12 }}>{s.type} · {s.status} · {s.rows} rows</Text>
            </View>
          ))
        ) : view === 'tasks' && facets ? (
          facets.tasks.map((task) => (
            <View key={task.id} style={{ backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border, padding: space.md, gap: 4 }}>
              <Text style={{ color: t.ink, fontWeight: '700' }}>{task.name}</Text>
              <Text style={{ color: t.muted, fontSize: 12 }}>{task.store} · {task.owner} · {task.status} · {task.progress}%</Text>
              <View style={{ height: 6, borderRadius: 3, backgroundColor: t.grid, overflow: 'hidden' }}>
                <View style={{ width: `${task.progress}%`, height: '100%', backgroundColor: t.s2 }} />
              </View>
            </View>
          ))
        ) : (
          <View style={{ backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border, padding: space.xl, alignItems: 'center' }}>
            <Text style={{ color: t.muted, textAlign: 'center', lineHeight: 20 }}>
              此頁為第二階段範圍。{'\n'}資料模型與 API 已就緒，畫面尚未實作。
            </Text>
          </View>
        )}
      </ScrollView>
    </>
  );
}
