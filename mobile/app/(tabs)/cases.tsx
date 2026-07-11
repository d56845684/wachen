/** 客訴案件 — HTML PAGES.cases：狀態分組 chips + 卡片列表 */
import React, { useState } from 'react';
import { FlatList, ScrollView, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { CaseListItem } from '@/components/CaseListItem';
import { PressableScale } from '@/components/PressableScale';
import { useCases } from '@/api/queries';
import { radius, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';
import { CASE_STATUS_LABEL, type CaseStatus } from '@/types/domain';

const GROUPS: { label: string; statuses: CaseStatus[] | null }[] = [
  { label: '全部', statuses: null },
  { label: '待分派', statuses: ['unassigned'] },
  { label: '進行中', statuses: ['open', 'in_progress', 'pending_customer', 'pending_review'] },
  { label: '已完成', statuses: ['done', 'closed'] },
];

export default function Cases() {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const { data: cases = [] } = useCases();
  const [group, setGroup] = useState(0);

  const g = GROUPS[group];
  const rows = g.statuses ? cases.filter((c) => g.statuses!.includes(c.cstatus)) : cases;

  return (
    <View style={{ flex: 1, backgroundColor: t.page, paddingTop: insets.top + space.md }}>
      <Text style={[type.display, { color: t.ink, paddingHorizontal: space.lg }]}>客訴案件</Text>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} style={{ flexGrow: 0, marginVertical: space.sm }} contentContainerStyle={{ gap: space.sm, paddingHorizontal: space.lg }}>
        {GROUPS.map((item, i) => {
          const on = i === group;
          const count = item.statuses ? cases.filter((c) => item.statuses!.includes(c.cstatus)).length : cases.length;
          return (
            <PressableScale
              key={item.label}
              onPress={() => setGroup(i)}
              style={{
                paddingHorizontal: 12, paddingVertical: 6, borderRadius: radius.pill,
                backgroundColor: on ? t.s1 : t.surface, borderWidth: 1, borderColor: on ? t.s1 : t.border,
              }}
            >
              <Text style={{ color: on ? '#fff' : t.ink2, fontSize: 12.5, fontWeight: '600' }}>
                {item.label} {count}
              </Text>
            </PressableScale>
          );
        })}
      </ScrollView>
      <FlatList
        data={rows}
        keyExtractor={(c) => c.id}
        renderItem={({ item }) => <CaseListItem item={item} />}
        contentContainerStyle={{ paddingBottom: 96 }}
        ListEmptyComponent={<Text style={{ color: t.muted, textAlign: 'center', marginTop: 40 }}>此狀態沒有案件</Text>}
      />
    </View>
  );
}
