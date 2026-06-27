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
  Search,
  ShieldCheck,
  Thermometer,
  Zap,
} from 'lucide-react';
import { CHART_CONFIG } from '../../constants/dashboard.constants';
import { API, showError, showInfo, showSuccess } from '../../helpers';
import { getChannelIcon } from '../../helpers';

const { Text, Title } = Typography;

const statusMeta = {
  operational: { color: 'green', label: '正常', icon: ShieldCheck },
  degraded: { color: 'orange', label: '历史异常', icon: AlertTriangle },
  provider_cooling: { color: 'orange', label: '历史上游限流', icon: Zap },
  cooling: { color: 'yellow', label: '当前冷却', icon: Thermometer },
  disabled: { color: 'red', label: '手动禁用', icon: AlertTriangle },
  auto_disabled: { color: 'red', label: '自动禁用', icon: AlertTriangle },
  unobserved: { color: 'grey', label: '未观测', icon: Clock3 },
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
  return statusMeta[status] || statusMeta.unobserved;
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
  };
  return labels[eventType] || eventType || '-';
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
      { type: t('历史上游限流'), value: summary?.provider_cooling || 0 },
      { type: t('当前冷却'), value: summary?.cooling || 0 },
      { type: t('历史异常'), value: summary?.degraded || 0 },
      { type: t('手动禁用'), value: summary?.disabled || 0 },
      { type: t('自动禁用'), value: summary?.auto_disabled || 0 },
      { type: t('未观测'), value: summary?.unobserved || 0 },
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
    ],
    [summary, t],
  );

  const directCooldownItems = useMemo(() => {
    return [...items]
      .filter((item) => {
        const events = item?.events || {};
        const cooldown = item?.cooldown || {};
        return (
          (events.newapi_cooldowns || 0) > 0 ||
          (events.provider_cooldowns || 0) > 0 ||
          cooldown.cooling_down ||
          cooldown.probe_required ||
          cooldown.probing
        );
      })
      .sort((a, b) => {
        const aTime =
          a?.events?.last_event_at ||
          a?.cooldown?.last_probe_at ||
          a?.cooldown?.next_probe_at ||
          0;
        const bTime =
          b?.events?.last_event_at ||
          b?.cooldown?.last_probe_at ||
          b?.cooldown?.next_probe_at ||
          0;
        return bTime - aTime;
      })
      .slice(0, 8);
  }, [items]);

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
          [t('历史上游限流')]: '#f39c12',
          [t('当前冷却')]: '#f5a623',
          [t('历史异常')]: '#ff7f0e',
          [t('手动禁用')]: '#d9534f',
          [t('自动禁用')]: '#c0392b',
          [t('未观测')]: '#8c8c8c',
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
        },
      },
    }),
    [eventChartData, t],
  );

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
      title: t('近24h健康事件'),
      dataIndex: 'events',
      width: 240,
      render: (events) => {
        const problemEvents = events?.recent_problem_events || 0;
        if (
          !problemEvents &&
          !events?.probe_waiting &&
          !events?.probe_scanned &&
          !events?.probe_skipped &&
          !events?.probe_started &&
          !events?.probe_succeeded
        ) {
          return <Text type='tertiary'>{t('无事件')}</Text>;
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
            </Space>
            <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
              {t(getEventLabel(events?.last_event_type))}{' '}
              {formatTime(events?.last_event_at)}
            </Text>
            {events?.last_event_message ? (
              <Text size='small' type='tertiary' ellipsis={{ showTooltip: true }}>
                {events.last_event_message}
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
      width: 130,
      render: (_, record) => (
        <Button
          size='small'
          type='tertiary'
          icon={<Play size={14} />}
          loading={testingId === record.id}
          onClick={() => runChannelTest(record)}
        >
          {t('立即测试')}
        </Button>
      ),
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
        <Button
          type='primary'
          theme='light'
          icon={<RefreshCw size={15} />}
          loading={loading}
          onClick={loadHealth}
        >
          {t('刷新')}
        </Button>
      </div>

      <Banner
        type='info'
        closeIcon={null}
        className='mb-3'
        description={t(
          '只有“当前冷却”状态和“当前冷却/探针”列表示渠道现在被 NewAPI 冷却或等待恢复探针；“历史异常”“历史上游限流”“曾NewAPI冷却”都是近24h历史记录，不代表当前仍在冷却。页面读取不消耗上游额度，只有手动“立即测试”和冷却主动探针会发起上游请求。',
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

      <Card
        title={t('当前冷却与近24h冷却事件')}
        bodyStyle={{ padding: 16 }}
        className='mb-3'
      >
        {directCooldownItems.length ? (
          <div className='space-y-3'>
            {directCooldownItems.map((record) => {
              const events = record?.events || {};
              const cooldown = record?.cooldown || {};
              const tags = [];
              if ((events.newapi_cooldowns || 0) > 0) {
                tags.push(
                  <Tag key='newapi' color='yellow' size='small'>
                    {t('曾NewAPI冷却')} {events.newapi_cooldowns}
                  </Tag>,
                );
              }
              if ((events.provider_cooldowns || 0) > 0) {
                tags.push(
                  <Tag key='provider' color='orange' size='small'>
                    {t('曾上游限流')} {events.provider_cooldowns}
                  </Tag>,
                );
              }
              if (cooldown.cooling_down) {
                tags.push(
                  <Tag key='cooling' color='red' size='small'>
                    {t('当前冷却中')}
                  </Tag>,
                );
              }
              if (cooldown.probe_required) {
                tags.push(
                  <Tag key='probe' color='blue' size='small'>
                    {cooldown.probing ? t('探针中') : t('探针等待')}
                  </Tag>,
                );
              }
              return (
                <div
                  key={record.id}
                  className='flex flex-col gap-2 rounded-lg border border-[var(--semi-color-border)] px-3 py-2 md:flex-row md:items-center md:justify-between'
                >
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
                  <div className='flex flex-wrap items-center gap-2'>
                    {tags}
                  </div>
                  <div className='text-right'>
                    <Text size='small' ellipsis={{ showTooltip: true }}>
                      {t(getEventLabel(events.last_event_type))}
                    </Text>
                    <Text type='tertiary' size='small' ellipsis={{ showTooltip: true }}>
                      {formatTime(events.last_event_at)}
                    </Text>
                    {cooldown.cooldown_ttl_seconds ? (
                      <Text type='tertiary' size='small'>
                        TTL {formatDuration(cooldown.cooldown_ttl_seconds)}
                      </Text>
                    ) : null}
                    {cooldown.last_failure_at ? (
                      <Text type='tertiary' size='small'>
                        {t('最后失败')} {formatTime(cooldown.last_failure_at)}
                      </Text>
                    ) : null}
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <Text type='tertiary'>{t('当前没有冷却，也没有近24小时冷却事件')}</Text>
        )}
      </Card>

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
                { label: t('历史异常'), value: 'degraded' },
                { label: t('历史上游限流'), value: 'provider_cooling' },
                { label: t('当前冷却'), value: 'cooling' },
                { label: t('手动禁用'), value: 'disabled' },
                { label: t('自动禁用'), value: 'auto_disabled' },
                { label: t('未观测'), value: 'unobserved' },
              ]}
            />
          </Space>
          <Text type='tertiary' size='small'>
            {t('冷却阈值')} {settings.channel_cooldown_failure_threshold || '-'}{' '}
            / {settings.channel_cooldown_failure_window || '-'}s,{' '}
            {t('冷却时长')} {settings.channel_cooldown_seconds || '-'}s,{' '}
            {t('探针')}{' '}
            {settings.channel_cooldown_probe_enabled ? t('开启') : t('关闭')}
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
          scroll={{ x: 1460 }}
          size='middle'
        />
      </Spin>
    </div>
  );
};

export default ChannelHealth;
