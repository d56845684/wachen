import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Stack } from 'expo-router';
import { GestureHandlerRootView } from 'react-native-gesture-handler';
import { StatusBar } from 'expo-status-bar';
import { useTheme } from '@/theme/useTheme';

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

export default function RootLayout() {
  const t = useTheme();
  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <QueryClientProvider client={queryClient}>
        <StatusBar style="auto" />
        <Stack
          screenOptions={{
            headerShown: false,
            contentStyle: { backgroundColor: t.page },
          }}
        >
          <Stack.Screen name="login" options={{ animation: 'fade' }} />
          <Stack.Screen name="(tabs)" />
          {/*
            案件詳情：HTML 的右側 drawer 在行動端改為 bottom sheet 型 modal。
            spatial consistency（apple-design §7）：由下進、往下出，同一路徑。
          */}
          <Stack.Screen
            name="case/[id]"
            options={{ presentation: 'modal', animation: 'slide_from_bottom', gestureEnabled: true }}
          />
          <Stack.Screen name="store/[code]" options={{ animation: 'slide_from_right' }} />
          <Stack.Screen name="more/[view]" options={{ animation: 'slide_from_right' }} />
        </Stack>
      </QueryClientProvider>
    </GestureHandlerRootView>
  );
}
