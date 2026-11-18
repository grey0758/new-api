/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useState } from 'react';
import { Button, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { ArrowUp, ArrowDown } from 'lucide-react';
import { API, renderQuota, showError, showSuccess } from '../../helpers';

const { Text } = Typography;

export default function SubscriptionPriorityEditor({
  t,
  subscriptions,
  priority = [],
  planTitleMap,
  reload,
}) {
  const [ordered, setOrdered] = useState(subscriptions);
  const [saving, setSaving] = useState(false);
  useEffect(() => setOrdered(subscriptions), [subscriptions]);

  if (subscriptions.length === 0) return null;
  const changed = ordered.some(
    (item, index) =>
      item.subscription.id !== subscriptions[index]?.subscription.id,
  );
  const move = (index, delta) => {
    setOrdered((items) => {
      const next = [...items];
      [next[index], next[index + delta]] = [next[index + delta], next[index]];
      return next;
    });
  };
  const save = async (reset = false) => {
    setSaving(true);
    try {
      const { data } = await API.put('/api/subscription/self/priority', {
        subscription_ids: reset
          ? []
          : ordered.map((item) => item.subscription.id),
      });
      if (!data?.success) {
        showError(data?.message || t('保存套餐顺序失败'));
        await reload?.();
        return;
      }
      showSuccess(t('套餐使用顺序已保存'));
      await reload?.();
    } catch {
      showError(t('保存套餐顺序失败'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className='mt-3 space-y-3' data-testid='subscription-priority-editor'>
      <div>
        <Text strong>{t('套餐使用优先级')}</Text>
        <p className='text-xs text-gray-500 mt-1'>
          {t('按从上到下的顺序使用，额度不足时自动尝试下一个套餐。')}
          {t('未设置时优先使用最早到期的套餐。')}
        </p>
      </div>
      <ol className='space-y-2 list-none p-0 m-0'>
        {ordered.map(({ subscription: sub }, index) => (
          <li
            key={sub.id}
            className='flex flex-wrap items-center gap-2 rounded-lg border border-gray-200 dark:border-gray-700 p-2'
            data-subscription-id={sub.id}
          >
            <Tag color='blue'>{index + 1}</Tag>
            <div className='flex-1 min-w-0 text-xs'>
              <div className='font-medium break-words'>
                {planTitleMap.get(sub.plan_id) || t('订阅')} · #{sub.id}
              </div>
              <div className='text-gray-500 mt-1'>
                {t('剩余')}{' '}
                {sub.amount_total > 0
                  ? renderQuota(Math.max(0, sub.amount_total - sub.amount_used))
                  : t('不限量')}
                {' · '}
                {t('至')} {new Date(sub.end_time * 1000).toLocaleDateString()}
              </div>
            </div>
            <Space spacing={4}>
              <Button
                size='small'
                aria-label={t('上移套餐') + ' #' + sub.id}
                icon={<ArrowUp size={14} />}
                disabled={saving || index === 0}
                onClick={() => move(index, -1)}
              />
              <Button
                size='small'
                aria-label={t('下移套餐') + ' #' + sub.id}
                icon={<ArrowDown size={14} />}
                disabled={saving || index === ordered.length - 1}
                onClick={() => move(index, 1)}
              />
            </Space>
          </li>
        ))}
      </ol>
      <Space wrap>
        <Button
          size='small'
          theme='solid'
          loading={saving}
          disabled={!changed || saving}
          onClick={() => save()}
        >
          {t('保存使用顺序')}
        </Button>
        <Button
          size='small'
          disabled={saving || priority.length === 0}
          onClick={() => save(true)}
        >
          {t('恢复最早到期优先')}
        </Button>
      </Space>
    </div>
  );
}
