/**
 * 案件詳情 — HTML 的右側 drawer（renderCaseDrawer）改為 bottom-sheet modal。
 * 手勢下滑可關閉（native modal gesture，spring、可中斷）。
 * 狀態按鈕只呈現狀態機允許的轉移；變更為 optimistic update + 即時回饋。
 */
import React from 'react';
import { ScrollView, Text, View } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import * as Haptics from 'expo-haptics';
import { PressableScale } from '@/components/PressableScale';
import { RiskBadge, StatusPill } from '@/components/Badge';
import { SlaCountdown } from '@/components/SlaCountdown';
import { useCaseDetail, useUpdateStatus } from '@/api/queries';
import { CASE_TRANSITIONS } from '@/domain/caseMachine';
import { maskPii } from '@/auth/roles';
import { useSession } from '@/auth/session';
import { radius, space, type } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';

export default function CaseDetail() {
  const t = useTheme();
  const { id } = useLocalSearchParams<{ id: string }>();
  const role = useSession((s) => s.role);
  const { data: c } = useCaseDetail(id);
  const update = useUpdateStatus();

  if (!c) {
    return (
      <View style={{ flex: 1, backgroundColor: t.page, alignItems: 'center', justifyContent: 'center' }}>
        <Text style={{ color: t.muted }}>載入中…</Text>
      </View>
    );
  }

  const transitions = CASE_TRANSITIONS[c.cstatus] ?? [];

  return (
    <ScrollView style={{ flex: 1, backgroundColor: t.page }} contentContainerStyle={{ padding: space.lg, gap: space.md, paddingBottom: 48 }}>
      {/* sheet grabber */}
      <View style={{ alignSelf: 'center', width: 36, height: 4, borderRadius: 2, backgroundColor: t.baseline, marginBottom: 4 }} />

      <View style={{ flexDirection: 'row', alignItems: 'center', gap: space.sm }}>
        <Text style={[type.title, { color: t.ink, flex: 1 }]}>C-{c.id.slice(0, 6).toUpperCase()}</Text>
        <RiskBadge level={c.risk_level} />
        <StatusPill status={c.cstatus} />
      </View>

      <Section title="評論內容">
        <Text style={{ color: t.warning, fontSize: 14 }}>
          {'★'.repeat(c.rating)}<Text style={{ color: t.grid }}>{'★'.repeat(5 - c.rating)}</Text>
          <Text style={{ color: t.muted, fontSize: 12 }}>  {c.platform} · {c.posted_at.slice(0, 10)}</Text>
        </Text>
        <Text style={{ color: t.ink, fontSize: 14.5, lineHeight: 22 }}>{c.review_content}</Text>
        <Text style={{ color: t.muted, fontSize: 12 }}>— {maskPii(role, c.author_name, 'name')}</Text>
      </Section>

      <Section title="AI 分析">
        <KV k="摘要" v={c.summary} />
        <KV k="分類" v={c.categories.join('、') || '—'} />
        <KV k="關鍵字" v={c.keywords.join('、') || '—'} />
        <KV k="風險原因" v={c.risk_reasons.join('、') || '—'} />
        <KV k="模型" v={`${c.model_name} · ${c.prompt_version}`} />
      </Section>

      <Section title="處理資訊">
        <KV k="門市" v={c.store} />
        <KV k="負責人" v={c.assignee} />
        <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
          <Text style={{ color: t.muted, fontSize: 12.5 }}>SLA</Text>
          <SlaCountdown item={c} />
        </View>
        {c.reopened_count > 0 ? <KV k="回開次數" v={String(c.reopened_count)} /> : null}
      </Section>

      {transitions.length > 0 ? (
        <View style={{ gap: space.sm }}>
          {transitions.map((tr) => {
            const bg = tr.style === 'primary' ? t.s1 : tr.style === 'ok' ? t.good : t.surface;
            const fg = tr.style === 'plain' ? t.ink2 : '#fff';
            return (
              <PressableScale
                key={tr.to}
                disabled={update.isPending}
                onPress={() => {
                  // 有意義的 commit 時刻才給 haptic（apple-design §13 utility）
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  update.mutate({ id: c.id, status: tr.to });
                }}
                style={{
                  backgroundColor: bg, borderRadius: radius.md, padding: 13, alignItems: 'center',
                  borderWidth: tr.style === 'plain' ? 1 : 0, borderColor: t.border,
                  opacity: update.isPending ? 0.6 : 1,
                }}
              >
                <Text style={{ color: fg, fontWeight: '700', fontSize: 14.5 }}>{tr.label}</Text>
              </PressableScale>
            );
          })}
        </View>
      ) : null}
    </ScrollView>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  const t = useTheme();
  return (
    <View style={{ backgroundColor: t.surface, borderRadius: radius.lg, borderWidth: 1, borderColor: t.border, padding: space.lg, gap: 8 }}>
      <Text style={[type.heading, { color: t.ink }]}>{title}</Text>
      {children}
    </View>
  );
}

function KV({ k, v }: { k: string; v: string }) {
  const t = useTheme();
  return (
    <View style={{ flexDirection: 'row', gap: space.md }}>
      <Text style={{ color: t.muted, fontSize: 12.5, width: 64 }}>{k}</Text>
      <Text style={{ color: t.ink2, fontSize: 12.5, flex: 1 }}>{v}</Text>
    </View>
  );
}
