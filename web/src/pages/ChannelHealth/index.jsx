/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Banner,
  Button,
  Card,
  Input,
  Modal,
  Progress,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { VChart } from '@visactor/react-vchart';
import {
  Activity,
  AlertTriangle,
  Clock3,
  Play,
  RefreshCw,
  RotateCcw,
  Search,
  ShieldCheck,
  Thermometer,
  Zap,
} from 'lucide-react';
import { CHART_CONFIG } from '../../constants/dashboard.constants';
import { API, isRoot, showError, showSuccess } from '../../helpers';
import { getChannelIcon } from '../../helpers';

const { Text, Title } = Typography;

const statusMeta = {
  operational: { color: 'green', label: '正常', icon: ShieldCheck },
  cooling: { color: 'yellow', label: '当前冷却', icon: Thermometer },
  disabled: { color: 'red', label: '手动禁用', icon: AlertTriangle },
};

function formatTime(timestamp) {
  if (!timestamp) return '-';
  return new Date(timestamp * 1000).toLocaleString();
}

function formatDuration(seconds) {
  if (!seconds || seconds <= 0) return '-';
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  if (minutes < 60) return rest ? `${minutes}m ${rest}s` : `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const minuteRest = minutes % 60;
  return minuteRest ? `${hours}h ${minuteRest}m` : `${hours}h`;
}

function getStatusMeta(status) {
  return statusMeta[status] || statusMeta.operational;
}

function getEventLabel(eventType) {
  const labels = {
    final_error: '最终请求错误',
    intermediate_failover_error: '中间渠道失败已切换',
    provider_cooldown: '曾上游/凭证池限流',
    newapi_channel_cooling: '曾触发 NewAPI 冷却',
    probe_waiting: '探针等待',
    probe_scanned: '探针扫描',
    probe_skipped: '探针跳过',
    probe_started: '探针中',
    probe_failed: '探针失败',
    probe_succeeded: '探针成功',
    manual_recovered: '手动恢复冷却',
  };
  return labels[eventType] || eventType || '-';
}

function getEventColor(eventType) {
  if (eventType === 'probe_succeeded' || eventType === 'manual_recovered') {
    return 'green';
  }
  if (eventType === 'probe_failed' || eventType === 'final_error') {
    return 'red';
  }
  if (eventType === 'probe_started' || eventType === 'probe_scanned') {
    return 'blue';
  }
  if (eventType === 'probe_skipped' || eventType === 'probe_waiting') {
    return 'grey';
  }
  if (eventType === 'newapi_channel_cooling') {
    return 'yellow';
  }
  return 'orange';
}

function getHealthEventErrorCount(events = {}) {
  return (
    (events.final_errors || 0) +
    (events.failover_errors || 0) +
    (events.provider_cooldowns || 0) +
    (events.newapi_cooldowns || 0) +
    (events.probe_failed || 0)
  );
}

function getRequestEventCount(events = {}) {
  return (
    (events.final_errors || 0) +
    (events.failover_errors || 0) +
    (events.provider_cooldowns || 0)
  );
}

function getProbeEventCount(events = {}) {
  return (
    (events.newapi_cooldowns || 0) +
    (events.probe_waiting || 0) +
    (events.probe_scanned || 0) +
    (events.probe_skipped || 0) +
    (events.probe_started || 0) +
    (events.probe_failed || 0) +
    (events.probe_succeeded || 0) +
    (events.manual_recovered || 0)
  );
}

function getEventScopeTitle(scope) {
  if (scope === 'request') return '请求错误历史';
  if (scope === 'probe') return '探针/冷却历史';
  return '健康事件历史';
}

function getEventScopeDescription(scope) {
  if (scope === 'request') {
    return '这里仅显示最终请求错误、中间渠道失败但已 failover 成功、上游/provider 凭证池限流等用户请求链路事件。用户额度不足属于请求错误历史，不代表探针失败，也不代表渠道当前冷却。';
  }
  if (scope === 'probe') {
    return '这里仅显示主动探针、NewAPI 渠道冷却和手动恢复历史。倍率后缀或手动开启主动探针的渠道会每分钟持续探测；探针失败或探针 60s 内没有返回有效流内容时会进入/保持 NewAPI 冷却，通过后恢复调度。';
  }
  return '这里显示最近 7 天的渠道健康事件。';
}

function StatCard({ icon: Icon, label, value, tone = 'blue' }) {
  const toneMap = {
    blue: 'rgba(24, 144, 255, 0.10)',
    green: 'rgba(46, 167, 88, 0.12)',
    yellow: 'rgba(250, 173, 20, 0.14)',
    orange: 'rgba(245, 120, 0, 0.13)',
    red: 'rgba(245, 63, 63, 0.12)',
  };
  return (
    <Card className='min-h-[92px]' bodyStyle={{ padding: 16 }}>
      <div className='flex items-center justify-between gap-3'>
        <div>
          <Text type='tertiary' size='small'>
            {label}
          </Text>
          <div className='mt-2 text-2xl font-semibold leading-8'>{value}</div>
        </div>
        <div
          className='h-10 w-10 rounded-lg flex items-center justify-center'
          style={{ background: toneMap[tone] || toneMap.blue }}
        >
          <Icon size={18} />
        </div>
      </div>
    </Card>
  );
}

const ChannelHealth = () => {
  const { t } = useTranslation();
  const [items, setItems] = useState([]);
  const [summary, setSummary] = useState({});
  const [settings, setSettings] = useState({});
  const [loading, setLoading] = useState(false);
  const [testingId, setTestingId] = useState(null);
  const [recoveringId, setRecoveringId] = useState(null);
  const [eventModalVisible, setEventModalVisible] = useState(false);
  const [eventLoading, setEventLoading] = useState(false);
  const [eventRecord, setEventRecord] = useState(null);
  const [eventItems, setEventItems] = useState([]);
  const [eventScope, setEventScope] = useState('probe');
  const [resettingAffinity, setResettingAffinity] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');

  const loadHealth = async () => {
    setLoading(true);
    try {
      const res = await API.get('/api/channel/health', {
        disableDuplicate: true,
      });
      const { success, message, data } = res.data || {};
      if (!success) {
        showError(message || t('获取渠道健康度失败'));
        return;
      }
      setItems(data?.items || []);
      setSummary(data?.summary || {});
      setSettings(data?.settings || {});
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadHealth();
  }, []);

  const filteredItems = useMemo(() => {
    const kw = keyword.trim().toLowerCase();
    return items.filter((item) => {
      if (statusFilter !== 'all' && item.health_status !== statusFilter) {
        return false;
      }
      if (!kw) return true;
      return [item.id, item.name, item.group, item.models, item.base_url]
        .join(' ')
        .toLowerCase()
        .includes(kw);
    });
  }, [items, keyword, statusFilter]);

  const recentErrorRate = useMemo(() => {
    const total = summary?.recent_requests || 0;
    if (!total) return 0;
    return (
      Math.round(
        (((summary?.recent_errors || 0) +
          (summary?.recent_problem_events || 0)) /
          total) *
          1000,
      ) / 10
    );
  }, [summary]);

  const statusChartData = useMemo(
    () => [
      { type: t('正常'), value: summary?.operational || 0 },
      { type: t('当前冷却'), value: summary?.cooling || 0 },
      { type: t('手动禁用'), value: summary?.disabled || 0 },
    ],
    [summary, t],
  );

  const eventChartData = useMemo(
    () => [
      { type: t('最终请求错误'), value: summary?.final_errors || 0 },
      { type: t('中间渠道失败'), value: summary?.failover_errors || 0 },
      { type: t('曾上游限流'), value: summary?.provider_cooldowns || 0 },
      { type: t('曾NewAPI冷却'), value: summary?.newapi_cooldowns || 0 },
      { type: t('探针等待'), value: summary?.probe_waiting || 0 },
      { type: t('探针扫描'), value: summary?.probe_scanned || 0 },
      { type: t('探针跳过'), value: summary?.probe_skipped || 0 },
      { type: t('探针中'), value: summary?.probe_started || 0 },
      { type: t('探针失败'), value: summary?.probe_failed || 0 },
      { type: t('探针成功'), value: summary?.probe_succeeded || 0 },
      { type: t('手动恢复'), value: summary?.manual_recovered || 0 },
    ],
    [summary, t],
  );

  const statusChartSpec = useMemo(
    () => ({
      type: 'pie',
      data: [{ id: 'channel-status', values: statusChartData }],
      outerRadius: 0.88,
      innerRadius: 0.58,
      padAngle: 0.5,
      valueField: 'value',
      categoryField: 'type',
      pie: {
        style: { cornerRadius: 8 },
        state: {
          hover: { outerRadius: 0.92, stroke: '#fff', lineWidth: 1 },
          selected: { outerRadius: 0.92, stroke: '#fff', lineWidth: 1 },
        },
      },
      legends: {
        visible: true,
        orient: 'bottom',
      },
      label: {
        visible: true,
        formatMethod: (_, datum) => `${datum.type}: ${datum.value}`,
      },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum.type,
              value: (datum) => datum.value,
            },
          ],
        },
      },
      color: {
        specified: {
          [t('正常')]: '#2ca02c',
          [t('当前冷却')]: '#f5a623',
          [t('手动禁用')]: '#d9534f',
        },
      },
    }),
    [statusChartData, t],
  );

  const eventChartSpec = useMemo(
    () => ({
      type: 'bar',
      data: [{ id: 'channel-events', values: eventChartData }],
      xField: 'type',
      yField: 'value',
      bar: {
        style: {
          cornerRadius: [6, 6, 0, 0],
        },
      },
      axes: [
        { orient: 'bottom', label: { angle: -20 } },
        { orient: 'left', label: { visible: true } },
      ],
      legends: { visible: false },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum) => datum.type,
              value: (datum) => datum.value,
            },
          ],
        },
      },
      color: {
        specified: {
          [t('最终请求错误')]: '#d9534f',
          [t('中间渠道失败')]: '#f39c12',
          [t('曾上游限流')]: '#f5a623',
          [t('曾NewAPI冷却')]: '#ffcc00',
          [t('探针等待')]: '#8c8c8c',
          [t('探针中')]: '#4aa3df',
          [t('探针失败')]: '#c0392b',
          [t('探针成功')]: '#2ca02c',
          [t('手动恢复')]: '#2ca02c',
        },
      },
    }),
    [eventChartData, t],
  );

  const openEventHistory = async (record, scope = 'probe') => {
    setEventRecord(record);
    setEventScope(scope);
    setEventItems([]);
    setEventModalVisible(true);
    setEventLoading(true);
    try {
      const res = await API.get(
        `/api/channel/health/${record.id}/events?hours=168&limit=200&scope=${scope}`,
        { disableDuplicate: true },
      );
      const { success, message, data } = res.data || {};
      if (!success) {
        showError(message || t('获取健康事件历史失败'));
        return;
      }
      setEventItems(data?.items || []);
    } finally {
      setEventLoading(false);
    }
  };

  const recoverCooldown = (record) => {
    Modal.confirm({
      title: t('手动恢复 NewAPI 冷却？'),
      content: t(
        '该操作只清除 NewAPI 渠道冷却和等待探针状态，不会启用被管理员禁用或自动禁用的渠道，也不会修改渠道配置。恢复后后续请求会重新参与正常调度。',
      ),
      onOk: async () => {
        setRecoveringId(record.id);
        try {
          const res = await API.post(
            `/api/channel/health/${record.id}/cooldown/recover`,
            {},
            { disableDuplicate: true },
          );
          const { success, message } = res.data || {};
          if (!success) {
            showError(message || t('恢复冷却失败'));
            return;
          }
          showSuccess(t('已清除该渠道的 NewAPI 冷却/探针状态'));
          await loadHealth();
          if (eventModalVisible && eventRecord?.id === record.id) {
            await openEventHistory(record, eventScope);
          }
        } finally {
          setRecoveringId(null);
        }
      },
    });
  };

  const resetChannelAffinity = () => {
    Modal.confirm({
      title: t('重置渠道亲和性？'),
      content: t(
        '该操作会清空当前运行实例内存中的渠道亲和性缓存，不会修改渠道配置、冷却状态或历史日志。',
      ),
      onOk: async () => {
        setResettingAffinity(true);
        try {
          const res = await API.delete('/api/option/channel_affinity_cache', {
            params: { all: true },
            disableDuplicate: true,
          });
          const { success, message, data } = res.data || {};
          if (!success) {
            showError(message || t('重置渠道亲和性失败'));
            return;
          }
          showSuccess(
            t('已重置渠道亲和性，清理 ${deleted} 条缓存').replace(
              '${deleted}',
              String(data?.deleted ?? 0),
            ),
          );
        } finally {
          setResettingAffinity(false);
        }
      },
    });
  };

  const runChannelTest = (record) => {
    Modal.confirm({
      title: t('立即测试渠道？'),
      content: t(
        '该操作会向上游发起一次真实测试请求，通常只消耗很少额度。页面刷新和健康度读取不会消耗上游额度。',
      ),
      onOk: async () => {
        setTestingId(record.id);
        try {
          const res = await API.get(`/api/channel/test/${record.id}`, {
            disableDuplicate: true,
          });
          const { success, message, time } = res.data || {};
          if (success) {
            showSuccess(
              t('通道 ${name} 测试成功，耗时 ${time.toFixed(2)} 秒。')
                .replace('${name}', record.name)
                .replace('${time.toFixed(2)}', Number(time || 0).toFixed(2)),
            );
          } else {
            showError(message || t('测试失败'));
          }
          await loadHealth();
        } finally {
          setTestingId(null);
        }
      },
    });
  };

  const columns = [
    {
      title: t('渠道'),
      dataIndex: 'name',
      fixed: 'left',
      width: 240,
      render: (_, record) => (
        <div className='min-w-0'>
          <div className='flex items-center gap-2 min-w-0'>
            {getChannelIcon(record.type)}
            <Text strong ellipsis={{ showTooltip: true }}>
              #{record.id} {record.name}
            </Text>
          </div>
          <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
            {record.base_url || '-'}
          </Text>
        </div>
      ),
    },
    {
      title: t('实时状态'),
      dataIndex: 'health_status',
      width: 150,
      render: (status, record) => {
        const meta = getStatusMeta(status);
        const Icon = meta.icon;
        return (
          <Tooltip content={record.health_reason}>
            <Tag
              color={meta.color}
              shape='circle'
              prefixIcon={<Icon size={13} />}
            >
              {t(meta.label)}
            </Tag>
          </Tooltip>
        );
      },
    },
    {
      title: t('近24小时'),
      dataIndex: 'recent',
      width: 180,
      render: (recent, record) => {
        const total = recent?.total_requests || 0;
        const requestErrors = recent?.error_requests || 0;
        const eventErrors = getHealthEventErrorCount(record?.events);
        const errors = requestErrors + eventErrors;
        const rate = total ? Math.round((errors / total) * 1000) / 10 : 0;
        return (
          <div className='space-y-1'>
            <div className='flex justify-between text-xs'>
              <span>
                {t('请求')} {total}
              </span>
              <span>
                {t('错误')} {errors}
              </span>
            </div>
            <Progress
              percent={rate}
              size='small'
              stroke={
                rate >= 50
                  ? 'var(--semi-color-danger)'
                  : 'var(--semi-color-success)'
              }
              aria-label={t('错误率')}
            />
            <Text type='tertiary' size='small'>
              {t('错误率')} {rate}%
            </Text>
          </div>
        );
      },
    },
    {
      title: t('请求错误历史'),
      dataIndex: 'events',
      width: 220,
      render: (events) => {
        if (!getRequestEventCount(events)) {
          return <Text type='tertiary'>{t('无请求错误')}</Text>;
        }
        return (
          <div className='space-y-1'>
            <Space wrap spacing={4}>
              {events?.final_errors ? (
                <Tag color='red' size='small'>
                  {t('最终错误')} {events.final_errors}
                </Tag>
              ) : null}
              {events?.failover_errors ? (
                <Tag color='orange' size='small'>
                  {t('中间渠道失败')} {events.failover_errors}
                </Tag>
              ) : null}
              {events?.provider_cooldowns ? (
                <Tag color='orange' size='small'>
                  {t('曾上游限流')} {events.provider_cooldowns}
                </Tag>
              ) : null}
            </Space>
            <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
              {t(getEventLabel(events?.last_request_event_type))}{' '}
              {formatTime(events?.last_request_event_at)}
            </Text>
            {events?.last_request_error_code === 'insufficient_user_quota' ? (
              <Tag color='grey' size='small'>
                {t('用户额度不足')}
              </Tag>
            ) : null}
            {events?.last_request_event_message ? (
              <Text size='small' type='tertiary' ellipsis={{ showTooltip: true }}>
                {events.last_request_event_message}
              </Text>
            ) : null}
          </div>
        );
      },
    },
    {
      title: t('探针/冷却历史'),
      dataIndex: 'events',
      width: 230,
      render: (events) => {
        if (!getProbeEventCount(events)) {
          return <Text type='tertiary'>{t('无探针/冷却事件')}</Text>;
        }
        return (
          <div className='space-y-1'>
            <Space wrap spacing={4}>
              {events?.newapi_cooldowns ? (
                <Tag color='yellow' size='small'>
                  {t('曾NewAPI冷却')} {events.newapi_cooldowns}
                </Tag>
              ) : null}
              {events?.probe_waiting ? (
                <Tag color='grey' size='small'>
                  {t('探针等待')} {events.probe_waiting}
                </Tag>
              ) : null}
              {events?.probe_scanned ? (
                <Tag color='blue' size='small'>
                  {t('探针扫描')} {events.probe_scanned}
                </Tag>
              ) : null}
              {events?.probe_skipped ? (
                <Tag color='grey' size='small'>
                  {t('探针跳过')} {events.probe_skipped}
                </Tag>
              ) : null}
              {events?.probe_started ? (
                <Tag color='blue' size='small'>
                  {t('探针中')} {events.probe_started}
                </Tag>
              ) : null}
              {events?.probe_failed ? (
                <Tag color='red' size='small'>
                  {t('探针失败')} {events.probe_failed}
                </Tag>
              ) : null}
              {events?.probe_succeeded ? (
                <Tag color='green' size='small'>
                  {t('探针成功')} {events.probe_succeeded}
                </Tag>
              ) : null}
              {events?.manual_recovered ? (
                <Tag color='green' size='small'>
                  {t('手动恢复')} {events.manual_recovered}
                </Tag>
              ) : null}
            </Space>
            <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
              {t(getEventLabel(events?.last_probe_event_type))}{' '}
              {formatTime(events?.last_probe_event_at)}
            </Text>
            {events?.last_probe_event_message ? (
              <Text size='small' type='tertiary' ellipsis={{ showTooltip: true }}>
                {events.last_probe_event_message}
              </Text>
            ) : null}
          </div>
        );
      },
    },
    {
      title: t('当前冷却/探针'),
      dataIndex: 'cooldown',
      width: 220,
      render: (cooldown) => {
        if (!cooldown?.cooling_down && !cooldown?.probe_required) {
          if (cooldown?.active_probe_enabled) {
            return (
              <div className='space-y-1'>
                <Tag color='green' shape='circle'>
                  {t('主动探针开启')}
                </Tag>
                <div>
                  <Text size='small' type='tertiary'>
                    {t('范围')} {cooldown.active_probe_mode || '-'}
                  </Text>
                </div>
                {cooldown.last_probe_at ? (
                  <div>
                    <Text size='small' type='tertiary'>
                      {t('最后探针')} {formatTime(cooldown.last_probe_at)}
                    </Text>
                  </div>
                ) : null}
                {cooldown.next_probe_at ? (
                  <div>
                    <Text size='small' type='tertiary'>
                      {t('下次探针')} {formatTime(cooldown.next_probe_at)}
                    </Text>
                  </div>
                ) : null}
              </div>
            );
          }
          return <Text type='tertiary'>{t('当前未冷却，未等待探针')}</Text>;
        }
        let probeLabel = t('探针等待');
        if (cooldown.probing) {
          probeLabel = t('探针中');
        } else if (!cooldown.probe_required) {
          probeLabel = t('当前冷却中');
        }
        return (
          <div className='space-y-1'>
            <Tag color='yellow' shape='circle'>
              {probeLabel}
            </Tag>
            <div>
              <Text size='small' type='tertiary'>
                TTL {formatDuration(cooldown.cooldown_ttl_seconds)}
              </Text>
            </div>
            <div>
              <Text size='small' type='tertiary'>
                {t('下次探针')} {formatTime(cooldown.next_probe_at)}
              </Text>
            </div>
            {cooldown.probe_model ? (
              <div>
                <Text size='small' type='tertiary'>
                  {t('探针模型')} {cooldown.probe_model}
                </Text>
              </div>
            ) : null}
            {cooldown.last_failure_at ? (
              <div>
                <Text size='small' type='tertiary'>
                  {t('最后失败')} {formatTime(cooldown.last_failure_at)}
                </Text>
              </div>
            ) : null}
            {cooldown.last_error ? (
              <Text size='small' type='danger' ellipsis={{ showTooltip: true }}>
                {cooldown.last_error}
              </Text>
            ) : null}
          </div>
        );
      },
    },
    {
      title: t('测试记录'),
      dataIndex: 'test_time',
      width: 180,
      render: (_, record) => (
        <div className='space-y-1'>
          <div>
            <Text size='small'>{record.response_time || '-'} ms</Text>
          </div>
          <Text type='tertiary' size='small'>
            {formatTime(record.test_time)}
          </Text>
        </div>
      ),
    },
    {
      title: t('最近请求'),
      dataIndex: 'recent',
      width: 170,
      render: (recent) => (
        <div className='space-y-1'>
          <Text size='small'>
            {recent?.avg_use_time
              ? `${Number(recent.avg_use_time).toFixed(1)}s avg`
              : '-'}
          </Text>
          <Text type='tertiary' size='small'>
            {formatTime(recent?.last_seen_at)}
          </Text>
        </div>
      ),
    },
    {
      title: t('分组/模型'),
      dataIndex: 'group',
      width: 220,
      render: (_, record) => (
        <div className='space-y-1'>
          <Text size='small' ellipsis={{ showTooltip: true }}>
            {record.group || '-'}
          </Text>
          <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
            {record.test_model || (record.models || '').split(',')[0] || '-'}
          </Text>
        </div>
      ),
    },
    {
      title: t('操作'),
      dataIndex: 'operate',
      fixed: 'right',
      width: 280,
      render: (_, record) => {
        const cooldown = record?.cooldown || {};
        const canRecover =
          cooldown.cooling_down ||
          cooldown.probe_required ||
          cooldown.probing ||
          (cooldown.failure_count || 0) > 0;
        return (
          <Space wrap spacing={4}>
            <Button
              size='small'
              type='tertiary'
              icon={<Clock3 size={14} />}
              onClick={() => openEventHistory(record, 'probe')}
            >
              {t('探针历史')}
            </Button>
            <Button
              size='small'
              type='tertiary'
              icon={<AlertTriangle size={14} />}
              onClick={() => openEventHistory(record, 'request')}
            >
              {t('请求错误')}
            </Button>
            {canRecover ? (
              <Button
                size='small'
                type='warning'
                theme='light'
                icon={<RefreshCw size={14} />}
                loading={recoveringId === record.id}
                onClick={() => recoverCooldown(record)}
              >
                {t('恢复冷却')}
              </Button>
            ) : null}
            <Button
              size='small'
              type='tertiary'
              icon={<Play size={14} />}
              loading={testingId === record.id}
              onClick={() => runChannelTest(record)}
            >
              {t('立即测试')}
            </Button>
          </Space>
        );
      },
    },
  ];

  const eventColumns = [
    {
      title: t('时间'),
      dataIndex: 'created_at',
      width: 170,
      render: (value) => formatTime(value),
    },
    {
      title: t('类型'),
      dataIndex: 'event_type',
      width: 150,
      render: (value) => (
        <Tag color={getEventColor(value)} size='small'>
          {t(getEventLabel(value))}
        </Tag>
      ),
    },
    {
      title: t('模型'),
      dataIndex: 'model_name',
      width: 130,
      render: (value) => value || '-',
    },
    {
      title: t('状态码/错误'),
      dataIndex: 'status_code',
      width: 150,
      render: (_, record) => (
        <div className='space-y-1'>
          <Text size='small'>{record.status_code || '-'}</Text>
          <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
            {[record.error_type, record.error_code].filter(Boolean).join(' / ') || '-'}
          </Text>
        </div>
      ),
    },
    {
      title: t('内容'),
      dataIndex: 'content',
      width: 280,
      render: (value) => (
        <Text size='small' ellipsis={{ showTooltip: true }}>
          {value || '-'}
        </Text>
      ),
    },
    {
      title: t('细节'),
      dataIndex: 'other',
      width: 360,
      render: (other) => {
        const detail = other || {};
        const preferredKeys = [
          'skip_reason',
          'cooldown_ttl_seconds',
          'next_probe_at',
          'last_failure_at',
          'probe_model',
          'manual_recovery_scope',
          'admin_disable_untouched',
        ];
        const visible = preferredKeys
          .filter((key) => detail[key] !== undefined && detail[key] !== '')
          .map((key) => `${key}=${detail[key]}`);
        return (
          <Text size='small' ellipsis={{ showTooltip: true }}>
            {visible.length ? visible.join(', ') : JSON.stringify(detail)}
          </Text>
        );
      },
    },
  ];

  return (
    <div className='mt-[60px] px-2'>
      <div className='mb-3 flex flex-col gap-3 md:flex-row md:items-center md:justify-between'>
        <div>
          <Title heading={4} style={{ margin: 0 }}>
            {t('渠道健康度')}
          </Title>
          <Text type='tertiary'>
            {t('实时状态里的“当前冷却”才表示正在冷却；健康事件是近24小时历史记录')}
          </Text>
        </div>
        <Space wrap>
          {isRoot() ? (
            <Button
              type='warning'
              theme='light'
              icon={<RotateCcw size={15} />}
              loading={resettingAffinity}
              onClick={resetChannelAffinity}
            >
              {t('重置渠道亲和性')}
            </Button>
          ) : null}
          <Button
            type='primary'
            theme='light'
            icon={<RefreshCw size={15} />}
            loading={loading}
            onClick={loadHealth}
          >
            {t('刷新')}
          </Button>
        </Space>
      </div>

      <Banner
        type='info'
        closeIcon={null}
        className='mb-3'
        description={t(
          '只有“当前冷却”状态和“当前冷却/探针”列表示渠道现在被 NewAPI 冷却或等待恢复探针；名称后缀带倍率或手动开启 active_probe_enabled=true 的启用渠道会每分钟持续主动探测。主动探针失败或 60s 内没有返回有效流内容会进入/保持 NewAPI 冷却，后续探针通过后恢复调度。普通用户请求的 SSE/Responses 断流只记录健康事件和 failover，不进入 NewAPI 冷却。页面读取不消耗上游额度，手动“立即测试”和主动探针会发起少量真实请求；“恢复冷却”只清 NewAPI 冷却，不会启用管理员禁用的渠道。',
        )}
      />

      <div className='grid grid-cols-1 xl:grid-cols-2 gap-3 mb-3'>
        <Card
          title={t('渠道状态分布')}
          bodyStyle={{ padding: 0 }}
          headerStyle={{ paddingBottom: 8 }}
        >
          <div className='h-72 p-2'>
            {statusChartData.some((item) => item.value > 0) ? (
              <VChart spec={statusChartSpec} option={CHART_CONFIG} />
            ) : (
              <div className='h-full flex items-center justify-center text-sm text-[var(--semi-color-text-1)]'>
                {t('暂无渠道状态数据')}
              </div>
            )}
          </div>
        </Card>
        <Card
          title={t('健康事件分布')}
          bodyStyle={{ padding: 0 }}
          headerStyle={{ paddingBottom: 8 }}
        >
          <div className='h-72 p-2'>
            {eventChartData.some((item) => item.value > 0) ? (
              <VChart spec={eventChartSpec} option={CHART_CONFIG} />
            ) : (
              <div className='h-full flex items-center justify-center text-sm text-[var(--semi-color-text-1)]'>
                {t('暂无健康事件')}
              </div>
            )}
          </div>
        </Card>
      </div>

      <div className='grid grid-cols-2 lg:grid-cols-6 gap-3 mb-3'>
        <StatCard
          icon={Activity}
          label={t('全部渠道')}
          value={summary.total || 0}
        />
        <StatCard
          icon={ShieldCheck}
          label={t('正常')}
          value={summary.operational || 0}
          tone='green'
        />
        <StatCard
          icon={Thermometer}
          label={t('当前冷却')}
          value={summary.cooling || 0}
          tone='yellow'
        />
        <StatCard
          icon={Zap}
          label={t('曾上游限流')}
          value={summary.provider_cooldowns || 0}
          tone='orange'
        />
        <StatCard
          icon={AlertTriangle}
          label={t('最终错误')}
          value={summary.final_errors || 0}
          tone='orange'
        />
        <StatCard
          icon={AlertTriangle}
          label={t('中间渠道失败')}
          value={summary.failover_errors || 0}
          tone='orange'
        />
      </div>

      <div className='grid grid-cols-2 lg:grid-cols-4 gap-3 mb-3'>
        <StatCard
          icon={Activity}
          label={t('近24小时请求')}
          value={summary.recent_requests || 0}
        />
        <StatCard
          icon={AlertTriangle}
          label={t('近24小时错误率')}
          value={`${recentErrorRate}%`}
          tone='red'
        />
        <StatCard
          icon={Thermometer}
          label={t('探针失败/成功')}
          value={`${summary.probe_failed || 0}/${summary.probe_succeeded || 0}`}
          tone={summary.probe_failed ? 'red' : 'green'}
        />
        <StatCard
          icon={Clock3}
          label={t('探针等待')}
          value={summary.probe_waiting || 0}
          tone='yellow'
        />
      </div>

      <Card bodyStyle={{ padding: 16 }} className='mb-3'>
        <div className='flex flex-col md:flex-row gap-2 md:items-center md:justify-between'>
          <Space wrap>
            <Input
              prefix={<Search size={14} />}
              showClear
              value={keyword}
              placeholder={t('搜索渠道名称、ID、分组、模型或地址')}
              onChange={setKeyword}
              style={{ width: 300, maxWidth: '100%' }}
            />
            <Select
              value={statusFilter}
              onChange={setStatusFilter}
              style={{ width: 160 }}
              optionList={[
                { label: t('全部状态'), value: 'all' },
                { label: t('正常'), value: 'operational' },
                { label: t('当前冷却'), value: 'cooling' },
                { label: t('手动禁用'), value: 'disabled' },
              ]}
            />
          </Space>
          <Text type='tertiary' size='small'>
            {t('冷却阈值')} {settings.channel_cooldown_failure_threshold || '-'}{' '}
            / {settings.channel_cooldown_failure_window || '-'}s,{' '}
            {t('冷却时长')} {settings.channel_cooldown_seconds || '-'}s,{' '}
            {t('探针')}{' '}
            {settings.channel_cooldown_probe_enabled ? t('开启') : t('关闭')},{' '}
            {t('端点')}{' '}
            {settings.channel_cooldown_probe_protocol === 'openai-response'
              ? `OpenAI Response (${settings.channel_cooldown_probe_endpoint || '/v1/responses'})`
              : settings.channel_cooldown_probe_endpoint || '-'}
            ,{' '}
            {t('范围')} {settings.channel_cooldown_probe_scope || '-'}
          </Text>
        </div>
      </Card>

      <Spin spinning={loading && items.length === 0}>
        <Table
          rowKey='id'
          columns={columns}
          dataSource={filteredItems}
          pagination={{
            pageSize: 12,
            showSizeChanger: true,
            formatPageText: (page) =>
              `${t('第')} ${page.currentStart} - ${page.currentEnd} ${t('条，共')} ${page.total} ${t('条')}`,
          }}
          scroll={{ x: 2090 }}
          size='middle'
        />
      </Spin>

      <Modal
        title={
          eventRecord
            ? `${t(getEventScopeTitle(eventScope))} #${eventRecord.id} ${eventRecord.name}`
            : t(getEventScopeTitle(eventScope))
        }
        visible={eventModalVisible}
        footer={null}
        width={1120}
        onCancel={() => setEventModalVisible(false)}
      >
        <Banner
          type='info'
          closeIcon={null}
          className='mb-3'
          description={t(getEventScopeDescription(eventScope))}
        />
        <Table
          rowKey='id'
          columns={eventColumns}
          dataSource={eventItems}
          loading={eventLoading}
          pagination={{ pageSize: 10 }}
          scroll={{ x: 1240 }}
          size='small'
        />
      </Modal>
    </div>
  );
};

export default ChannelHealth;
