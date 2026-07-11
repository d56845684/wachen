/** 通知中心 — HTML PAGES.notifications */
import React from 'react';
import { FlatList, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { PressableScale } from '@/components/PressableScale';
import { useFacets } from '@/api/queries';
import { radius, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';

const LEVEL_ICON = { critical: '🚨', warning: '⚠️', serious: '🔶', good: '✅' } as const;

export default function Notifications() {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const router = useRouter();
  const { data: facets } = useFacets();
  const items = facets?.notifications ?? [];

  return (
    <View style={{ flex: 1, backgroundColor: t.page, paddingTop: insets.top + space.md }}>
      <Text style={[type.display, { color: t.ink, paddingHorizontal: space.lg, marginBottom: space.sm }]}>通知中心</Text>
      <FlatList
        data={items}
        keyExtractor={(n) => n.id}
        contentContainerStyle={{ paddingBottom: 96 }}
        renderItem={({ item }) => (
          <PressableScale
            scaleTo={0.98}
            disabled={!item.case_id}
            onPress={() => item.case_id && router.push(`/case/${item.case_id}`)}
            style={{
              flexDirection: 'row', gap: space.md, alignItems: 'flex-start',
              backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border,
              padding: space.md, marginHorizontal: space.lg, marginBottom: space.sm,
              opacity: item.read ? 0.55 : 1,
            }}
          >
            <Text style={{ fontSize: 18 }}>{LEVEL_ICON[item.level] ?? '💡'}</Text>
            <View style={{ flex: 1 }}>
              <Text style={{ color: t.ink, fontWeight: '700', fontSize: 14 }}>{item.title}</Text>
              <Text style={{ color: t.ink2, fontSize: 12.5, lineHeight: 18, marginTop: 2 }}>{item.body}</Text>
              <Text style={{ color: t.muted, fontSize: 11, marginTop: 4 }}>{item.channel} · {item.time}</Text>
            </View>
          </PressableScale>
        )}
      />
    </View>
  );
}
