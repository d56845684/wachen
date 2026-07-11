/**
 * 登入 — 對應 HTML #login。
 * PoC：角色下拉 + demo 登入；正式版走 SSO / POST /api/v1/login。
 */
import React, { useState } from 'react';
import { KeyboardAvoidingView, Platform, Text, TextInput, View } from 'react-native';
import { useRouter } from 'expo-router';
import { PressableScale } from '@/components/PressableScale';
import { useLogin } from '@/api/queries';
import { useSession } from '@/auth/session';
import { ROLES, type RoleId } from '@/auth/roles';
import { radius, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';

export default function Login() {
  const t = useTheme();
  const router = useRouter();
  const login = useLogin();
  const signIn = useSession((s) => s.signIn);
  const [email, setEmail] = useState('demo@wacity.example');
  const [roleId, setRoleId] = useState<RoleId>('hq');

  const submit = async () => {
    // mock 模式回固定 token；正式模式失敗會 throw 並顯示 inline error
    const { token } = await login.mutateAsync({ email, password: 'demo' });
    signIn(token, roleId);
    router.replace('/(tabs)');
  };

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      style={{ flex: 1, backgroundColor: '#141a24', justifyContent: 'center', padding: space.xl }}
    >
      <View style={{ backgroundColor: t.surface, borderRadius: radius.xl, padding: 28, gap: 6 }}>
        <Text style={[type.title, { color: t.ink }]}>瓦 顧客體驗中台</Text>
        <Text style={{ color: t.muted, fontSize: 13, marginBottom: 12 }}>Wacity CX Hub · 登入</Text>

        <Text style={{ color: t.ink2, fontSize: 12, fontWeight: '600' }}>Email</Text>
        <TextInput
          value={email}
          onChangeText={setEmail}
          autoCapitalize="none"
          keyboardType="email-address"
          style={{
            borderWidth: 1, borderColor: t.border, borderRadius: radius.md,
            padding: 10, color: t.ink, backgroundColor: t.surface2, marginBottom: 10,
          }}
        />

        <Text style={{ color: t.ink2, fontSize: 12, fontWeight: '600', marginBottom: 6 }}>Demo 角色</Text>
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: 8, marginBottom: 14 }}>
          {(Object.keys(ROLES) as RoleId[]).map((id) => (
            <PressableScale
              key={id}
              onPress={() => setRoleId(id)}
              style={{
                paddingHorizontal: 12, paddingVertical: 7, borderRadius: radius.pill,
                backgroundColor: roleId === id ? t.s1 : t.surface2,
                borderWidth: 1, borderColor: roleId === id ? t.s1 : t.border,
              }}
            >
              <Text style={{ color: roleId === id ? '#fff' : t.ink2, fontSize: 12.5, fontWeight: '600' }}>
                {ROLES[id].title}
              </Text>
            </PressableScale>
          ))}
        </View>

        {login.isError ? (
          <Text style={{ color: t.critical, fontSize: 12, marginBottom: 6 }}>登入失敗，請再試一次</Text>
        ) : null}

        <PressableScale
          onPress={submit}
          disabled={login.isPending}
          style={{ backgroundColor: t.ink, borderRadius: radius.md, padding: 13, alignItems: 'center', opacity: login.isPending ? 0.6 : 1 }}
        >
          <Text style={{ color: t.page, fontWeight: '700', fontSize: 15 }}>
            {login.isPending ? '登入中…' : '登入'}
          </Text>
        </PressableScale>
      </View>
    </KeyboardAvoidingView>
  );
}
