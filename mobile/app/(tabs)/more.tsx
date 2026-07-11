/** 更多 — 次要功能入口（依角色過濾）+ 角色切換（PoC demo） */
import React from 'react';
import { ScrollView, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useRouter } from 'expo-router';
import { PressableScale } from '@/components/PressableScale';
import { MORE_MENU, ROLES, allowedView, type RoleId } from '@/auth/roles';
import { useSession } from '@/auth/session';
import { radius, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';

export default function More() {
  const t = useTheme();
  const insets = useSafeAreaInsets();
  const router = useRouter();
  const { role, switchRole, signOut } = useSession();

  const visible = MORE_MENU.filter((m) => allowedView(role, m.id));
  const groups = [...new Set(visible.map((m) => m.group))];

  return (
    <ScrollView
      style={{ flex: 1, backgroundColor: t.page }}
      contentContainerStyle={{ paddingTop: insets.top + space.md, paddingBottom: 96, paddingHorizontal: space.lg, gap: space.md }}
    >
      <Text style={[type.display, { color: t.ink }]}>更多</Text>

      {groups.map((group) => (
        <View key={group} style={{ gap: space.sm }}>
          <Text style={[type.label, { color: t.muted }]}>{group}</Text>
          <View style={{ backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border, overflow: 'hidden' }}>
            {visible.filter((m) => m.group === group).map((m, i, arr) => (
              <PressableScale
                key={m.id}
                scaleTo={0.99}
                onPress={() => router.push(`/more/${m.id}`)}
                style={{
                  flexDirection: 'row', alignItems: 'center', gap: space.md, padding: space.md,
                  borderBottomWidth: i === arr.length - 1 ? 0 : 1, borderBottomColor: t.border,
                }}
              >
                <Text style={{ fontSize: 16 }}>{m.icon}</Text>
                <Text style={{ color: t.ink, fontSize: 14.5, flex: 1 }}>{m.title}</Text>
                <Text style={{ color: t.muted }}>›</Text>
              </PressableScale>
            ))}
          </View>
        </View>
      ))}

      <Text style={[type.label, { color: t.muted }]}>Demo 角色切換</Text>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.sm }}>
        {(Object.keys(ROLES) as RoleId[]).map((id) => (
          <PressableScale
            key={id}
            onPress={() => switchRole(id)}
            style={{
              paddingHorizontal: 12, paddingVertical: 7, borderRadius: radius.pill,
              backgroundColor: role.id === id ? t.s1 : t.surface,
              borderWidth: 1, borderColor: role.id === id ? t.s1 : t.border,
            }}
          >
            <Text style={{ color: role.id === id ? '#fff' : t.ink2, fontSize: 12.5, fontWeight: '600' }}>
              {ROLES[id].title}
            </Text>
          </PressableScale>
        ))}
      </View>

      <PressableScale
        onPress={() => { signOut(); router.replace('/login'); }}
        style={{ padding: space.md, alignItems: 'center' }}
      >
        <Text style={{ color: t.critical, fontWeight: '600' }}>登出</Text>
      </PressableScale>
    </ScrollView>
  );
}
