/**
 * 總覽儀表板 — 依角色 scope 自動調整（HTML 的 PAGES.dashboard / PAGES.store 合併）：
 * hq/region 看群體 KPI + 趨勢 + 區域熱點；store 只剩本店資料（scope 已裁切）。
 */
import React from 'react';
import { ScrollView, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { KpiCard } from '@/components/KpiCard';
import { BarChart } from '@/components/charts/BarChart';
import { Donut } from '@/components/charts/Donut';
import { LineChart } from '@/components/charts/LineChart';
import { useCases, useFacets } from '@/api/queries';
import { useSession } from '@/auth/session';
import { seriesColors, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';
import { RISK_LABEL, SENT_LABEL, isActive } from '@/types/domain';
import { useRouter } from 'expo-router';

export default function Dashboard() {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const router = useRouter();
  const role = useSession((s) => s.role);
  const { data: cases = [] } = useCases();
  const { data: facets } = useFacets();

  const neg = cases.filter((c) => c.sentiment === 'negative').length;
  const highOpen = cases.filter((c) => c.risk_level === 'high' && isActive(c)).length;
  const overdue = cases.filter((c) => isActive(c) && Date.parse(c.sla_due_at) < Date.now()).length;
  const avg = cases.length ? (cases.reduce((a, c) => a + (c.rating || 0), 0) / cases.length).toFixed(2) : '—';
  const kpi = facets?.agg.kpis;
  const colors = seriesColors(t);

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: t.page }}
      contentContainerStyle={{ paddingTop: insets.top + space.md, paddingBottom: 96, gap: space.md }}
    >
      <View style={{ paddingHorizontal: space.lg }}>
        <Text style={[type.display, { color: t.ink }]}>總覽</Text>
        <Text style={{ color: t.muted, fontSize: 12, marginTop: 2 }}>
          🔐 {role.title} · {role.scopeLabel ?? '本店'} · 可見案件 {cases.length} 筆{role.pii ? '' : ' · 個資已遮罩'}
        </Text>
      </View>

      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.sm, paddingHorizontal: space.lg }}>
        <KpiCard value={avg} label="平均評分" delta={0.3} sub="較改善前 4.1" synthetic />
        <KpiCard value={neg} label="負評數" tone="alarm" sub={`負評率 ${cases.length ? Math.round((neg / cases.length) * 100) : 0}%`} onPress={() => router.push('/reviews')} />
        <KpiCard value={cases.length} label="客訴案件數" onPress={() => router.push('/cases')} />
        {kpi ? <KpiCard value={kpi.sla_rate} unit="%" label="SLA 達成率" tone={kpi.sla_rate < 80 ? 'warn' : 'default'} synthetic onPress={() => router.push('/more/sla')} /> : null}
        <KpiCard value={highOpen} label="高風險未結" tone="alarm" />
        <KpiCard value={overdue} label="SLA 逾期" tone="alarm" />
      </View>

      {facets ? (
        <View style={{ gap: space.md, paddingHorizontal: space.lg }}>
          <Card title="負評趨勢（月）">
            <LineChart
              labels={facets.agg.monthly.map((m) => m.month.slice(2))}
              series={[
                { label: '評論量', color: t.s1, data: facets.agg.monthly.map((m) => m.reviews) },
                { label: '負評量', color: t.critical, data: facets.agg.monthly.map((m) => m.neg) },
              ]}
            />
          </Card>
          <Card title="負評原因分布">
            <Donut
              rows={facets.agg.category.slice(0, 7).map(([name, value], i) => ({ name, value, color: colors[i % 8] }))}
              centerLabel={String(facets.agg.category.reduce((a, [, v]) => a + v, 0))}
              centerSub="問題標記"
            />
          </Card>
          <Card title="風險等級分布">
            <BarChart
              rows={(['high', 'medium', 'low'] as const).map((k) => ({
                name: RISK_LABEL[k],
                value: cases.filter((c) => c.risk_level === k).length,
                color: { high: t.critical, medium: t.warning, low: t.good }[k],
              }))}
            />
          </Card>
          <Card title="情緒傾向">
            <BarChart
              rows={(['negative', 'neutral', 'positive'] as const).map((k) => ({
                name: SENT_LABEL[k],
                value: cases.filter((c) => c.sentiment === k).length,
                color: { negative: t.critical, neutral: t.muted, positive: t.good }[k],
              }))}
            />
          </Card>
        </View>
      ) : null}
    </ScrollView>
  );
}

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  const t = useTheme();
  return (
    <View style={{ backgroundColor: t.surface, borderRadius: 14, borderWidth: 1, borderColor: t.border, padding: space.lg, gap: space.md }}>
      <Text style={[type.heading, { color: t.ink }]}>{title}</Text>
      {children}
    </View>
  );
}
