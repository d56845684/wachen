/**
 * 底部分頁 — HTML 的側欄選單在行動端收斂為 5 tabs：
 * 總覽 / 負評 / 案件 / 通知 / 更多（次要功能進「更多」）。
 * tab 切換是高頻操作 → 不做切換動畫（emil：100+ 次/天的操作不動畫）。
 */
import React from 'react';
import { Text } from 'react-native';
import { Redirect, Tabs } from 'expo-router';
import { useSession } from '@/auth/session';
import { useCases, useFacets } from '@/api/queries';
import { useTheme } from '@/theme/useTheme';

function Icon({ glyph, focused }: { glyph: string; focused: boolean }) {
  return <Text style={{ fontSize: 20, opacity: focused ? 1 : 0.55 }}>{glyph}</Text>;
}

export default function TabsLayout() {
  const t = useTheme();
  const token = useSession((s) => s.token);
  const { data: cases } = useCases();
  const { data: facets } = useFacets();

  if (!token) return <Redirect href="/login" />;

  const openCount = cases?.filter((c) => ['unassigned', 'open'].includes(c.cstatus)).length ?? 0;
  const unread = facets?.notifications.filter((n) => !n.read).length ?? 0;

  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        animation: 'none',
        tabBarActiveTintColor: t.s1,
        tabBarInactiveTintColor: t.muted,
        tabBarStyle: { backgroundColor: t.tabBar, borderTopColor: t.border, position: 'absolute' },
        tabBarBadgeStyle: { backgroundColor: t.critical, fontSize: 10 },
      }}
    >
      <Tabs.Screen name="index" options={{ title: '總覽', tabBarIcon: (p) => <Icon glyph="🏠" focused={p.focused} /> }} />
      <Tabs.Screen name="reviews" options={{ title: '負評', tabBarIcon: (p) => <Icon glyph="💬" focused={p.focused} /> }} />
      <Tabs.Screen
        name="cases"
        options={{
          title: '案件',
          tabBarIcon: (p) => <Icon glyph="📋" focused={p.focused} />,
          tabBarBadge: openCount > 0 ? openCount : undefined,
        }}
      />
      <Tabs.Screen
        name="notifications"
        options={{
          title: '通知',
          tabBarIcon: (p) => <Icon glyph="🔔" focused={p.focused} />,
          tabBarBadge: unread > 0 ? unread : undefined,
        }}
      />
      <Tabs.Screen name="more" options={{ title: '更多', tabBarIcon: (p) => <Icon glyph="☰" focused={p.focused} /> }} />
    </Tabs>
  );
}
