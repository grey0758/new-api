import React, { useEffect, useMemo, useState } from 'react';
import { Banner, Spin, Toast } from '@douyinfe/semi-ui';
import { Check, Copy, KeyRound, RotateCcw } from 'lucide-react';
import { API } from '../../helpers';
import { fetchTokenKey } from '../../helpers/token';
import {
  buildInstallCommand,
  defaultBaseUrlFromStatus,
  INSTALL_MODELS,
  normalizeCodexBaseUrl,
  PLATFORMS,
} from './installCommandBuilder';

function normalizeApiKey(value) {
  const trimmed = String(value || '').trim();
  if (!trimmed) {
    return '';
  }
  return trimmed.startsWith('sk-') ? trimmed : `sk-${trimmed}`;
}

function readSaved(key) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function writeSaved(key, value) {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Ignore storage failures in restricted browser contexts.
  }
}

export default function Install() {
  const [loading, setLoading] = useState(true);
  const [loadingDefaultKey, setLoadingDefaultKey] = useState(false);
  const [defaultApiKey, setDefaultApiKey] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [model, setModel] = useState(INSTALL_MODELS[0]);
  const [platform, setPlatform] = useState('linux');
  const [copied, setCopied] = useState(false);
  const [message, setMessage] = useState('');
  const defaultBaseUrl = useMemo(() => defaultBaseUrlFromStatus(), []);
  const baseUrlOptions = useMemo(
    () => Array.from(new Set([defaultBaseUrl].filter(Boolean).map(normalizeCodexBaseUrl))),
    [defaultBaseUrl],
  );

  useEffect(() => {
    const savedBaseUrl = readSaved('newapi.install.baseUrl');
    const savedModel = readSaved('newapi.install.model');
    const savedPlatform = readSaved('newapi.install.platform');

    setBaseUrl(savedBaseUrl || defaultBaseUrlFromStatus());
    if (INSTALL_MODELS.includes(savedModel)) {
      setModel(savedModel);
    }
    if (PLATFORMS.some((item) => item.id === savedPlatform)) {
      setPlatform(savedPlatform);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function loadDefaultKey() {
      setLoading(true);
      setLoadingDefaultKey(true);
      try {
        const response = await API.get('/api/token/?p=1&size=1', {
          disableDuplicate: true,
          skipErrorHandler: true,
        });
        const { success, data, message: apiMessage } = response.data || {};
        const firstToken = success ? data?.items?.[0] : null;

        if (!firstToken?.id) {
          if (!cancelled) {
            setMessage(apiMessage || '当前账户还没有可用令牌');
          }
          return;
        }

        const key = normalizeApiKey(await fetchTokenKey(firstToken.id));
        if (!cancelled) {
          setDefaultApiKey(key);
          setApiKey((current) => current || readSaved('newapi.install.apiKey') || key);
          setMessage('');
        }
      } catch (error) {
        if (!cancelled) {
          setMessage(error?.message || '默认令牌读取失败');
          setApiKey((current) => current || readSaved('newapi.install.apiKey') || '');
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
          setLoadingDefaultKey(false);
        }
      }
    }

    void loadDefaultKey();

    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (!loading) {
      writeSaved('newapi.install.apiKey', apiKey);
    }
  }, [apiKey, loading]);

  useEffect(() => {
    if (baseUrl) {
      writeSaved('newapi.install.baseUrl', baseUrl);
    }
  }, [baseUrl]);

  useEffect(() => {
    writeSaved('newapi.install.model', model);
  }, [model]);

  useEffect(() => {
    writeSaved('newapi.install.platform', platform);
  }, [platform]);

  const normalizedBaseUrl = useMemo(() => normalizeCodexBaseUrl(baseUrl), [baseUrl]);
  const command = useMemo(
    () => buildInstallCommand(platform, apiKey, normalizedBaseUrl, model),
    [apiKey, model, normalizedBaseUrl, platform],
  );
  const selectedPlatform = PLATFORMS.find((item) => item.id === platform) || PLATFORMS[0];
  const isPresetBaseUrl = Boolean(normalizedBaseUrl) && baseUrlOptions.includes(normalizedBaseUrl);
  const hasSavedApiKey = !loading && readSaved('newapi.install.apiKey') !== null;
  const apiKeySourceLabel = hasSavedApiKey
    ? '使用本机保存值'
    : defaultApiKey
      ? '已从登录账户填入'
      : '可手动填写';

  const copyCommand = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      Toast.success('已复制');
      window.setTimeout(() => setCopied(false), 1800);
    } catch {
      Toast.error('复制失败');
    }
  };

  if (loading) {
    return (
      <div
        className='min-h-[calc(100vh-64px)] px-4 pb-10 pt-24'
        style={{
          background:
            'radial-gradient(circle at top, rgba(0,255,136,0.14), transparent 35%), linear-gradient(180deg, #06070b, #0b0f17 45%, #081019)',
        }}
      >
        <div className='mx-auto flex max-w-5xl justify-center'>
          <div className='w-full max-w-xl rounded-lg border border-white/10 bg-[#0d1118] px-6 py-12 shadow-[0_28px_90px_rgba(0,0,0,0.35)]'>
            <div className='flex items-center justify-center gap-3 text-sm text-[#9fb0c7]'>
              <Spin size='middle' />
              正在加载安装配置
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      className='min-h-[calc(100vh-64px)] overflow-y-auto px-4 pb-8 pt-24'
      style={{
        background:
          'radial-gradient(circle at top left, rgba(0,255,136,0.16), transparent 28%), radial-gradient(circle at top right, rgba(56,189,248,0.18), transparent 24%), linear-gradient(180deg, #06070b, #0c1118 42%, #09121c)',
      }}
    >
      <div className='mx-auto max-w-6xl space-y-6'>
        <section className='rounded-lg border border-white/10 bg-black/25 p-6 backdrop-blur'>
          <div className='font-mono text-xs uppercase tracking-[0.32em] text-[#00ff88]'>
            codex install
          </div>
          <h1 className='mt-3 text-3xl font-semibold text-white'>用户安装页</h1>
          <p className='mt-3 max-w-3xl text-sm leading-6 text-[#9fb0c7]'>
            默认线路来自当前站点部署环境，卡密和 Base URL 都可以直接编辑。登录用户会自动读取账户第一个 Key；本机已编辑过的值会保留。
          </p>
        </section>

        {message ? (
          <Banner
            type='warning'
            bordered
            fullMode={false}
            description={message}
            closeIcon={null}
          />
        ) : null}

        <section className='rounded-lg border border-white/10 bg-[#0d1118] shadow-[0_28px_90px_rgba(0,0,0,0.35)]'>
          <div className='border-b border-white/10 px-6 pt-6 pb-4'>
            <div className='flex items-center gap-3 text-lg font-semibold text-white'>
              <KeyRound className='h-5 w-5 text-[#00ff88]' />
              生成安装命令
            </div>
            <p className='mt-2 text-sm text-[#7e91aa]'>
              选择系统、模型和线路后复制命令。默认模型为 {INSTALL_MODELS[0]}。
            </p>
          </div>

          <div className='space-y-6 px-6 pb-6 pt-5'>
            <div className='grid gap-4 lg:grid-cols-[1.2fr_0.8fr]'>
              <div className='space-y-2'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <label className='font-mono text-xs text-[#6f8096]'>卡密</label>
                  <span className='text-xs text-[#6f8096]'>{apiKeySourceLabel}</span>
                </div>
                <div className='flex min-h-11 items-center gap-2 rounded-lg border border-white/10 bg-[#0a0d13] px-3 text-white'>
                  <KeyRound className='h-4 w-4 flex-none text-[#6f8096]' />
                  <input
                    value={apiKey}
                    onChange={(event) => setApiKey(event.target.value)}
                    placeholder='输入你的 API Key'
                    className='install-page-input min-w-0 flex-1 bg-transparent py-3 font-mono text-sm text-white outline-none placeholder:text-[#506078]'
                  />
                </div>
                {defaultApiKey && apiKey !== defaultApiKey ? (
                  <button
                    type='button'
                    onClick={() => setApiKey(defaultApiKey)}
                    disabled={loadingDefaultKey}
                    className='inline-flex items-center gap-1 text-xs text-[#00ff88] transition hover:text-[#45ffab] disabled:cursor-not-allowed disabled:opacity-50'
                  >
                    <RotateCcw className='h-3.5 w-3.5' />
                    使用登录账户 Key
                  </button>
                ) : null}
              </div>

              <div className='space-y-2'>
                <label className='font-mono text-xs text-[#6f8096]'>模型</label>
                <div className='grid gap-2 sm:grid-cols-2'>
                  {INSTALL_MODELS.map((option) => {
                    const selected = option === model;
                    return (
                      <button
                        key={option}
                        type='button'
                        onClick={() => setModel(option)}
                        className={`h-11 rounded-lg border px-4 font-mono text-sm transition-colors ${
                          selected
                            ? 'border-[#00ff88]/35 bg-[#00ff88]/10 text-[#00ff88]'
                            : 'border-white/10 bg-[#0a0d13] text-[#9fb0c7] hover:bg-white/5'
                        }`}
                      >
                        {option}
                      </button>
                    );
                  })}
                </div>
              </div>
            </div>

            <div className='space-y-3'>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <label className='font-mono text-xs text-[#6f8096]'>Base URL</label>
                <span className='break-all text-right font-mono text-xs text-[#6f8096]'>
                  默认：{defaultBaseUrl}
                </span>
              </div>
              <div className='grid gap-2'>
                {baseUrlOptions.map((option) => {
                  const selected = option === normalizedBaseUrl;
                  return (
                    <button
                      key={option}
                      type='button'
                      onClick={() => setBaseUrl(option)}
                      className={`flex min-h-11 items-center rounded-lg border px-4 py-3 text-left font-mono text-sm transition-colors ${
                        selected
                          ? 'border-[#38bdf8]/35 bg-[#38bdf8]/10 text-[#c8ecfb]'
                          : 'border-white/10 bg-[#0a0d13] text-[#9fb0c7] hover:bg-white/5'
                      }`}
                    >
                      {option}
                    </button>
                  );
                })}
              </div>
              <div className='space-y-2'>
                <input
                  value={baseUrl}
                  onChange={(event) => setBaseUrl(event.target.value)}
                  placeholder='输入自定义 Base URL，例如 https://api.example.com/v1'
                  className='install-page-input min-h-11 w-full rounded-lg border border-white/10 bg-[#0a0d13] px-4 py-3 font-mono text-sm text-white outline-none placeholder:text-[#506078]'
                />
                <p className='text-xs text-[#6f8096]'>
                  {isPresetBaseUrl ? '当前使用预设地址，也可以直接编辑。' : `当前生成地址：${normalizedBaseUrl}`}
                </p>
              </div>
            </div>

            <div className='flex flex-wrap gap-3'>
              {PLATFORMS.map((item) => {
                const Icon = item.icon;
                const selected = item.id === platform;
                return (
                  <button
                    key={item.id}
                    type='button'
                    onClick={() => setPlatform(item.id)}
                    className={`inline-flex h-11 items-center gap-2 rounded-lg border px-4 font-mono text-sm transition-colors ${
                      selected
                        ? 'border-[#00ff88]/35 bg-[#00ff88]/10 text-[#00ff88]'
                        : 'border-white/10 bg-[#0a0d13] text-[#9fb0c7] hover:bg-white/5'
                    }`}
                  >
                    <Icon className='h-4 w-4' />
                    {item.label}
                  </button>
                );
              })}

              <button
                type='button'
                onClick={copyCommand}
                className='ml-auto inline-flex h-11 items-center gap-2 rounded-lg border border-[#00ff88]/35 bg-[#0b2419] px-4 text-sm font-semibold text-[#d9ffe9] shadow-[0_0_0_1px_rgba(0,255,136,0.06)] transition hover:bg-[#103521] hover:text-white'
              >
                {copied ? <Check className='h-4 w-4' /> : <Copy className='h-4 w-4' />}
                {copied ? '已复制' : `复制 ${selectedPlatform.label} 命令`}
              </button>
            </div>

            <div className='rounded-lg border border-white/10 bg-[#090c12] p-4'>
              <div className='mb-3 flex items-center justify-between gap-3'>
                <div className='font-mono text-xs uppercase tracking-[0.28em] text-[#6f8096]'>
                  {selectedPlatform.label} command
                </div>
                <div className='break-all text-right font-mono text-[11px] text-[#00ff88]'>
                  model={model} | base={normalizedBaseUrl}
                </div>
              </div>
              <pre className='max-h-[32rem] overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-6 text-[#d7e1ee]'>
                {command}
              </pre>
            </div>
          </div>
        </section>
      </div>
    </div>
  );
}
