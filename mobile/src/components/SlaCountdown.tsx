/**
 * SLA 倒數 — 每秒 tick（單一 interval，文字更新非動畫，不觸發 layout animation）。
 * 逾期 = critical 紅字；一小時內到期 = warning。
 */
import React, { useEffect, useState } from 'react';
import { Text } from 'react-native';
import { useTheme } from '@/theme/useTheme';
import type { Case } from '@/types/domain';
import { isActive } from '@/types/domain';

function fmt(due: number, now: number) {
  const diff = due - now;
  const over = diff < 0;
  const a = Math.abs(diff);
  const h = Math.floor(a / 3.6e6);
  const m = Math.floor((a % 3.6e6) / 6e4);
  const s = Math.floor((a % 6e4) / 1e3);
  const p = (n: number) => String(n).padStart(2, '0');
  return { text: (over ? '逾期 ' : '') + `${h}:${p(m)}:${p(s)}`, over, soon: !over && diff < 3.6e6 };
}

export function SlaCountdown({ item }: { item: Case }) {
  const t = useTheme();
  const [now, setNow] = useState(Date.now);
  const due = Date.parse(item.sla_due_at);

  useEffect(() => {
    if (!isActive(item) || Number.isNaN(due)) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [item, due]);

  if (!isActive(item)) return <Text style={{ color: t.muted, fontSize: 12 }}>已結束</Text>;
  if (Number.isNaN(due)) return <Text style={{ color: t.muted, fontSize: 12 }}>—</Text>;

  const r = fmt(due, now);
  const color = r.over ? t.critical : r.soon ? t.warning : t.ink2;
  return (
    <Text style={{ color, fontSize: 12, fontWeight: r.over ? '700' : '500', fontVariant: ['tabular-nums'] }}>
      {r.text}
    </Text>
  );
}
