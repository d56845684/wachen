import React from 'react';
import { Text, View } from 'react-native';
import { radius } from '@/theme/tokens';
import { useTheme } from '@/theme/useTheme';
import type { CaseStatus, RiskLevel } from '@/types/domain';
import { CASE_STATUS_LABEL, RISK_LABEL } from '@/types/domain';

export function RiskBadge({ level }: { level: RiskLevel }) {
  const t = useTheme();
  const color = { high: t.critical, medium: t.warning, low: t.good }[level];
  return <Chip label={RISK_LABEL[level]} color={color} />;
}

export function StatusPill({ status }: { status: CaseStatus }) {
  const t = useTheme();
  const color =
    status === 'done' || status === 'closed' ? t.good
    : status === 'in_progress' || status === 'pending_review' ? t.s1
    : status === 'canceled' ? t.muted
    : t.serious;
  return <Chip label={CASE_STATUS_LABEL[status]} color={color} />;
}

function Chip({ label, color }: { label: string; color: string }) {
  return (
    <View style={{ backgroundColor: color + '22', borderRadius: radius.pill, paddingHorizontal: 8, paddingVertical: 2, alignSelf: 'flex-start' }}>
      <Text style={{ color, fontSize: 11, fontWeight: '700' }}>{label}</Text>
    </View>
  );
}
