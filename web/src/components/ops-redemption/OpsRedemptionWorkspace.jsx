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

import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  API,
  copy,
  showError,
  showInfo,
  showSuccess,
} from '../../helpers';
import {
  Banner,
  Button,
  Card,
  Input,
  Select,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import {
  Copy,
  ExternalLink,
  RefreshCw,
  Settings2,
} from 'lucide-react';
import { useSearchParams } from 'react-router-dom';

const { Text, Title } = Typography;

const durationLabelMap = {
  year: '年',
  month: '月',
  day: '天',
  hour: '小时',
  custom: '自定义',
};

function normalizeTextUrl(value) {
  return String(value || '').trim().replace(/\/+$/, '');
}

function toInt(value, fallback = 0) {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function formatPlanLabel(plan) {
  if (!plan) return '';
  const duration = `${plan.duration_value || 0}${durationLabelMap[plan.duration_unit] || plan.duration_unit || ''}`;
  const price = `${plan.price_amount ?? 0}${plan.currency ? ` ${plan.currency}` : ''}`;
  return `${plan.title || `#${plan.id}`} · ${duration} · ${price}`;
}

function buildGenerateEndpointParams({
  token,
  subscriptionPlanId,
  count,
  name,
  baseUrl,
  docsUrl,
  expiredTime,
}) {
  const params = new URLSearchParams();
  if (token) params.set('token', token);
  if (subscriptionPlanId > 0) params.set('subscription_plan_id', String(subscriptionPlanId));
  if (count > 0) params.set('count', String(count));
  if (name) params.set('name', name);
  if (baseUrl) params.set('base_url', baseUrl);
  if (docsUrl) params.set('docs_url', docsUrl);
  if (expiredTime > 0) params.set('expired_time', String(expiredTime));
  return params;
}

export default function OpsRedemptionWorkspace({ title, subtitle, autoGenerate = false }) {
  const [searchParams] = useSearchParams();
  const tokenFromQuery = useMemo(() => searchParams.get('token')?.trim() || '', [searchParams]);
  const subscriptionPlanFromQuery = useMemo(
    () => toInt(searchParams.get('subscription_plan_id'), 0),
    [searchParams],
  );
  const countFromQuery = useMemo(() => toInt(searchParams.get('count'), 1), [searchParams]);
  const nameFromQuery = useMemo(() => searchParams.get('name')?.trim() || '', [searchParams]);
  const baseUrlFromQuery = useMemo(
    () => normalizeTextUrl(searchParams.get('base_url') || ''),
    [searchParams],
  );
  const docsUrlFromQuery = useMemo(
    () => normalizeTextUrl(searchParams.get('docs_url') || ''),
    [searchParams],
  );
  const expiredTimeFromQuery = useMemo(
    () => toInt(searchParams.get('expired_time'), 0),
    [searchParams],
  );

  const [token, setToken] = useState('');
  const [tokenHint, setTokenHint] = useState('');
  const [loadingPlans, setLoadingPlans] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [plans, setPlans] = useState([]);
  const [selectedPlanId, setSelectedPlanId] = useState(0);
  const [defaultBaseUrl, setDefaultBaseUrl] = useState('');
  const [defaultDocsUrl, setDefaultDocsUrl] = useState('');
  const [defaultName, setDefaultName] = useState('');
  const [count, setCount] = useState(countFromQuery);
  const [name, setName] = useState(nameFromQuery);
  const [baseUrl, setBaseUrl] = useState(baseUrlFromQuery);
  const [docsUrl, setDocsUrl] = useState(docsUrlFromQuery);
  const [expiredTime, setExpiredTime] = useState(expiredTimeFromQuery);
  const [generated, setGenerated] = useState(null);
  const [lastGeneratedText, setLastGeneratedText] = useState('');
  const autoGenerateRef = useRef(false);
  const loadedTokenRef = useRef('');
  const pendingLinkTokenLoadRef = useRef(false);

  const selectedPlan = useMemo(
    () => plans.find((item) => item.id === selectedPlanId) || null,
    [plans, selectedPlanId],
  );

  const canGenerate = Boolean(token && selectedPlanId > 0 && !generating);

  const loadPlans = async (inputToken) => {
    const trimmedToken = String(inputToken || token).trim();
    if (!trimmedToken) {
      showInfo('缺少授权信息，请从 1Password 打开运维链接');
      return false;
    }
    setLoadingPlans(true);
    try {
      const res = await API.get('/api/redemption/subscription-plans-with-token', {
        params: { token: trimmedToken },
        skipErrorHandler: true,
      });
      const payload = res.data?.data || {};
      const nextPlans = Array.isArray(payload.plans) ? payload.plans : [];
      setPlans(nextPlans);
      setDefaultBaseUrl(normalizeTextUrl(payload.default_base_url || ''));
      setDefaultDocsUrl(normalizeTextUrl(payload.default_docs_url || ''));
      setDefaultName(payload.default_name || '');
      setToken(trimmedToken);
      setTokenHint('授权已验证，订阅列表已加载');
      loadedTokenRef.current = trimmedToken;
      if (!baseUrl) setBaseUrl(normalizeTextUrl(payload.default_base_url || ''));
      if (!docsUrl) setDocsUrl(normalizeTextUrl(payload.default_docs_url || ''));
      if (!name) setName(payload.default_name || '');
      if (selectedPlanId <= 0) {
        const preferred = nextPlans.find((item) => item.id === payload.default_plan_id) || nextPlans[0] || null;
        setSelectedPlanId(preferred?.id || 0);
      }
      return nextPlans.length > 0;
    } catch (error) {
      setPlans([]);
      setSelectedPlanId(0);
      setTokenHint('授权无效或无法加载订阅列表');
      loadedTokenRef.current = trimmedToken;
      showError('授权无效，请检查 1Password 中的运维链接');
      return false;
    } finally {
      setLoadingPlans(false);
    }
  };

  const generateText = async () => {
    const trimmedToken = token.trim();
    if (!trimmedToken) {
      showInfo('缺少授权信息，请从 1Password 打开运维链接');
      return;
    }
    if (selectedPlanId <= 0) {
      showInfo('请选择订阅套餐');
      return;
    }
    setGenerating(true);
    try {
      const res = await API.post(
        '/api/redemption/generate-with-token',
        {
          count: Math.max(1, Math.min(100, toInt(count, 1))),
          name: name || selectedPlan?.title || defaultName,
          subscription_plan_id: selectedPlanId,
          base_url: normalizeTextUrl(baseUrl || defaultBaseUrl),
          docs_url: normalizeTextUrl(docsUrl || defaultDocsUrl),
          expired_time: Math.max(0, toInt(expiredTime, 0)),
        },
        {
          params: { token: trimmedToken },
          skipErrorHandler: true,
        },
      );
      const data = res.data?.data || {};
      setGenerated(data);
      setLastGeneratedText(data.text || '');
      showSuccess('兑换文案已生成');
    } catch (error) {
      showError('生成兑换文案失败');
    } finally {
      setGenerating(false);
    }
  };

  const copyGeneratedText = async () => {
    if (!lastGeneratedText) {
      showInfo('请先生成文案');
      return;
    }
    const ok = await copy(lastGeneratedText);
    if (ok) {
      showSuccess('已复制兑换文案');
    } else {
      showError('复制失败，请手动复制');
    }
  };

  const openApiPage = () => {
    const endpointParams = buildGenerateEndpointParams({
      token: token.trim(),
      subscriptionPlanId: selectedPlanId,
      count: Math.max(1, Math.min(100, toInt(count, 1))),
      name: name || selectedPlan?.title || defaultName,
      baseUrl: normalizeTextUrl(baseUrl || defaultBaseUrl),
      docsUrl: normalizeTextUrl(docsUrl || defaultDocsUrl),
      expiredTime: Math.max(0, toInt(expiredTime, 0)),
    });
    window.open(`/api/redemption/generate-with-token?${endpointParams.toString()}`, '_blank', 'noopener,noreferrer');
  };

  useEffect(() => {
    if (!tokenFromQuery) return;
    setToken(tokenFromQuery);
    setTokenHint('授权链接已载入');
    pendingLinkTokenLoadRef.current = true;
    const next = new URLSearchParams(window.location.search);
    next.delete('token');
    const query = next.toString();
    window.history.replaceState(
      {},
      document.title,
      `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash || ''}`,
    );
  }, [tokenFromQuery]);

  useEffect(() => {
    if (subscriptionPlanFromQuery > 0) {
      setSelectedPlanId(subscriptionPlanFromQuery);
    }
  }, [subscriptionPlanFromQuery]);

  useEffect(() => {
    if (countFromQuery > 0) {
      setCount(countFromQuery);
    }
  }, [countFromQuery]);

  useEffect(() => {
    if (nameFromQuery) {
      setName(nameFromQuery);
    }
  }, [nameFromQuery]);

  useEffect(() => {
    if (baseUrlFromQuery) {
      setBaseUrl(baseUrlFromQuery);
    }
  }, [baseUrlFromQuery]);

  useEffect(() => {
    if (docsUrlFromQuery) {
      setDocsUrl(docsUrlFromQuery);
    }
  }, [docsUrlFromQuery]);

  useEffect(() => {
    if (expiredTimeFromQuery >= 0) {
      setExpiredTime(expiredTimeFromQuery);
    }
  }, [expiredTimeFromQuery]);

  useEffect(() => {
    if (!token) return;
    if (loadingPlans || generating) return;
    if (!autoGenerate && !pendingLinkTokenLoadRef.current) return;
    if (loadedTokenRef.current !== token) {
      autoGenerateRef.current = false;
      pendingLinkTokenLoadRef.current = false;
      loadPlans(token);
      return;
    }
    if (autoGenerate && !autoGenerateRef.current && plans.length > 0 && selectedPlanId > 0) {
      autoGenerateRef.current = true;
      generateText();
    }
  }, [autoGenerate, token, loadingPlans, generating, plans.length, selectedPlanId]);

  const planOptions = useMemo(
    () =>
      plans.map((plan) => ({
        label: formatPlanLabel(plan),
        value: plan.id,
      })),
    [plans],
  );

  return (
    <div className='mx-auto w-full max-w-6xl px-3 py-4 md:py-6'>
      <Card className='!rounded-xl border border-[var(--semi-color-border)] shadow-sm'>
        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-2'>
            <div className='flex items-center gap-2'>
              <Settings2 size={18} />
              <Title heading={3} className='!m-0'>
                {title}
              </Title>
            </div>
            <Text type='tertiary'>{subtitle}</Text>
          </div>

          <Banner
            type='info'
            description='已根据授权链接自动加载订阅列表，可直接生成可复制的兑换文案。'
            closeIcon={null}
          />

          <div className='grid grid-cols-1 xl:grid-cols-2 gap-4'>
            <div className='space-y-4'>
              {!token ? (
                <Banner
                  type='warning'
                  description='未检测到授权信息，请从 1Password 中打开该站点的运维链接。'
                  closeIcon={null}
                />
              ) : (
                <div className='flex flex-wrap gap-2 items-center'>
                  <Button
                    type='tertiary'
                    icon={<RefreshCw size={16} />}
                    loading={loadingPlans}
                    onClick={() => loadPlans(token)}
                  >
                    刷新订阅
                  </Button>
                  {tokenHint ? <Tag color='blue'>{tokenHint}</Tag> : null}
                </div>
              )}

              <div className='space-y-3'>
                <Text strong>生成参数</Text>
                <div className='grid grid-cols-1 md:grid-cols-2 gap-3'>
                  <div className='space-y-1'>
                    <Text type='tertiary'>订阅套餐</Text>
                    <Select
                      value={selectedPlanId || undefined}
                      placeholder='先加载订阅列表'
                      onChange={(value) => setSelectedPlanId(Number(value) || 0)}
                      optionList={planOptions}
                      loading={loadingPlans}
                      style={{ width: '100%' }}
                      size='large'
                      filter
                    />
                  </div>
                  <div className='space-y-1'>
                    <Text type='tertiary'>数量</Text>
                    <Input
                      value={String(count)}
                      onChange={(value) => setCount(toInt(value, 1))}
                      type='number'
                      min={1}
                      max={100}
                      size='large'
                    />
                  </div>
                  <div className='space-y-1'>
                    <Text type='tertiary'>名称</Text>
                    <Input
                      value={name}
                      onChange={(value) => setName(value)}
                      placeholder={selectedPlan?.title || defaultName || '所选订阅名称'}
                      size='large'
                    />
                  </div>
                  <div className='space-y-1'>
                    <Text type='tertiary'>过期时间</Text>
                    <Input
                      value={String(expiredTime)}
                      onChange={(value) => setExpiredTime(toInt(value, 0))}
                      placeholder='0'
                      type='number'
                      min={0}
                      size='large'
                    />
                  </div>
                  <div className='space-y-1 md:col-span-2'>
                    <Text type='tertiary'>注册地址</Text>
                    <Input
                      value={baseUrl}
                      onChange={(value) => setBaseUrl(value)}
                      placeholder={defaultBaseUrl || 'https://api.opencodex.uk'}
                      size='large'
                    />
                  </div>
                  <div className='space-y-1 md:col-span-2'>
                    <Text type='tertiary'>文档地址</Text>
                    <Input
                      value={docsUrl}
                      onChange={(value) => setDocsUrl(value)}
                      placeholder={defaultDocsUrl || 'https://docs.opencodex.uk/opencodex/opencodex-uk'}
                      size='large'
                    />
                  </div>
                </div>

                <div className='flex flex-wrap gap-2'>
                  <Button
                    type='primary'
                    onClick={generateText}
                    loading={generating}
                    disabled={!canGenerate}
                  >
                    生成文案
                  </Button>
                  <Button
                    type='tertiary'
                    icon={<ExternalLink size={16} />}
                    onClick={openApiPage}
                    disabled={!token || selectedPlanId <= 0}
                  >
                    打开接口复制页
                  </Button>
                </div>
              </div>
            </div>

            <div className='space-y-4'>
              <div className='space-y-2'>
                <Text strong>生成结果</Text>
                <TextArea
                  value={generated?.text || lastGeneratedText}
                  placeholder='生成后显示可复制文案'
                  autosize={{ minRows: 12, maxRows: 20 }}
                  readOnly
                />
              </div>

              <div className='flex flex-wrap gap-2 items-center'>
                <Button
                  type='secondary'
                  icon={<Copy size={16} />}
                  onClick={copyGeneratedText}
                  disabled={!lastGeneratedText}
                >
                  复制文案
                </Button>
                <Text type='tertiary'>
                  已生成 {generated?.keys?.length || 0} 个兑换码
                </Text>
              </div>

              {generated?.keys?.length ? (
                <div className='flex flex-wrap gap-2'>
                  {generated.keys.map((key) => (
                    <Tag key={key} color='green' size='large'>
                      {key}
                    </Tag>
                  ))}
                </div>
              ) : (
                <Banner
                  type='light'
                  description='生成后这里会显示兑换码列表。'
                  closeIcon={null}
                />
              )}

              {selectedPlan ? (
                <Card className='!rounded-lg border border-[var(--semi-color-border)]'>
                  <div className='flex flex-col gap-1'>
                    <Text strong>{selectedPlan.title}</Text>
                    <Text type='tertiary'>{formatPlanLabel(selectedPlan)}</Text>
                  </div>
                </Card>
              ) : null}
            </div>
          </div>
        </div>
      </Card>
    </div>
  );
}
