import React, { useEffect, useMemo, useState } from 'react';
import { Banner, Spin, Toast } from '@douyinfe/semi-ui';
import { Check, Copy, KeyRound, RotateCcw } from 'lucide-react';
import { API } from '../../helpers';
import { fetchTokenKey } from '../../helpers/token';
import {
  buildClaudeInstallCommand,
  CLAUDE_BASE_URL,
  CLAUDE_DEFAULT_MODEL,
  CLAUDE_MODELS,
  CLAUDE_PLATFORMS,
} from './claudeInstallCommandBuilder';

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

export default function ClaudeInstall() {
  const [loading, setLoading] = useState(true);
  const [loadingDefaultKey, setLoadingDefaultKey] = useState(false);
  const [defaultApiKey, setDefaultApiKey] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [model, setModel] = useState(CLAUDE_DEFAULT_MODEL);
  const [copied, setCopied] = useState(null);
  const [message, setMessage] = useState('');
  useEffect(() => {
    const savedModel = readSaved('newapi.claudeInstall.model');
    if (CLAUDE_MODELS.includes(savedModel)) {
      setModel(savedModel);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function loadDefaultKey() {
      setLoading(true);
      setLoadingDefaultKey(true);
      try {
        const response = await API.get('/api/token/?p=1&size=1&order=oldest', {
          disableDuplicate: true,
          skipErrorHandler: true,
        });
        const { success, data, message: apiMessage } = response.data || {};
        const firstToken = success ? data?.items?.[0] : null;

        if (!firstToken?.id) {
          if (!cancelled) {
            setMessage(apiMessage || '当前账户还没有可用令牌');
            setApiKey((current) => current || readSaved('newapi.claudeInstall.apiKey') || '');
          }
          return;
        }

        const key = normalizeApiKey(await fetchTokenKey(firstToken.id));
        if (!cancelled) {
          setDefaultApiKey(key);
          setApiKey((current) => current || readSaved('newapi.claudeInstall.apiKey') || key);
          setMessage('');
        }
      } catch (error) {
        if (!cancelled) {
          setMessage(error?.message || '默认令牌读取失败');
          setApiKey((current) => current || readSaved('newapi.claudeInstall.apiKey') || '');
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
      writeSaved('newapi.claudeInstall.apiKey', apiKey);
    }
  }, [apiKey, loading]);

  useEffect(() => {
    writeSaved('newapi.claudeInstall.model', model);
  }, [model]);

  const commandByPlatform = useMemo(
    () =>
      CLAUDE_PLATFORMS.reduce((current, platform) => {
        current[platform.id] = buildClaudeInstallCommand(
          platform.id,
          apiKey,
          CLAUDE_BASE_URL,
          model,
        );
        return current;
      }, {}),
    [apiKey, model],
  );
  const hasSavedApiKey = !loading && readSaved('newapi.claudeInstall.apiKey') !== null;
  const apiKeySourceLabel = hasSavedApiKey
    ? '使用本机保存值'
    : defaultApiKey
      ? '已从登录账户填入'
      : '可手动填写';

  const copyCommand = async (platform, command) => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(platform);
      Toast.success('已复制');
      window.setTimeout(() => setCopied(null), 1800);
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
            'radial-gradient(circle at top, rgba(56,189,248,0.16), transparent 34%), linear-gradient(180deg, #06070b, #0b0f17 45%, #081019)',
        }}
      >
        <div className='mx-auto flex max-w-5xl justify-center'>
          <div className='w-full max-w-xl rounded-lg border border-white/10 bg-[#0d1118] px-6 py-12 shadow-[0_28px_90px_rgba(0,0,0,0.35)]'>
            <div className='flex items-center justify-center gap-3 text-sm text-[#9fb0c7]'>
              <Spin size='middle' />
              正在加载 Claude 安装配置
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
          'radial-gradient(circle at top left, rgba(56,189,248,0.18), transparent 28%), radial-gradient(circle at top right, rgba(0,255,136,0.13), transparent 24%), linear-gradient(180deg, #06070b, #0c1118 42%, #09121c)',
      }}
    >
      <div className='mx-auto max-w-6xl space-y-6'>
        <section className='rounded-lg border border-white/10 bg-black/25 p-6 backdrop-blur'>
          <div className='font-mono text-xs uppercase tracking-[0.32em] text-[#38bdf8]'>
            claude install
          </div>
          <h1 className='mt-3 text-3xl font-semibold text-white'>Claude Code 安装页</h1>
          <p className='mt-3 max-w-3xl text-sm leading-6 text-[#9fb0c7]'>
            这里单独生成 Claude Code 命令，环境变量写入 Claude 专用配置，不和 Codex 安装命令混在一起。
          </p>
          <a
            href='https://docs.opencodex.uk/opencodex/apicc-claude'
            className='mt-4 inline-block text-sm text-[#38bdf8] hover:text-[#7dd3fc]'
          >
            查看 APICC Claude 安装与使用教程
          </a>
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
          <div className='border-b border-white/10 px-6 pb-4 pt-6'>
            <div className='flex items-center gap-3 text-lg font-semibold text-white'>
              <KeyRound className='h-5 w-5 text-[#38bdf8]' />
              生成 Claude 安装命令
            </div>
            <p className='mt-2 text-sm text-[#7e91aa]'>
              Linux 使用普通用户 npm 前缀安装最新版；Windows 写入用户级环境变量并立即注入当前会话。
            </p>
          </div>

          <div className='space-y-6 px-6 pb-6 pt-5'>
            <div className='grid gap-4 lg:grid-cols-[1.2fr_0.8fr]'>
              <div className='space-y-2'>
                <div className='flex flex-wrap items-center justify-between gap-2'>
                  <label className='font-mono text-xs text-[#6f8096]'>Anthropic API Key</label>
                  <span className='text-xs text-[#6f8096]'>{apiKeySourceLabel}</span>
                </div>
                <div className='flex min-h-11 items-center gap-2 rounded-lg border border-white/10 bg-[#0a0d13] px-3 text-white'>
                  <KeyRound className='h-4 w-4 flex-none text-[#6f8096]' />
                  <input
                    value={apiKey}
                    onChange={(event) => setApiKey(event.target.value)}
                    placeholder='输入你的 Anthropic API Key'
                    className='install-page-input min-w-0 flex-1 bg-transparent py-3 font-mono text-sm text-white outline-none placeholder:text-[#506078]'
                  />
                </div>
                {defaultApiKey && apiKey !== defaultApiKey ? (
                  <button
                    type='button'
                    onClick={() => setApiKey(defaultApiKey)}
                    disabled={loadingDefaultKey}
                    className='inline-flex items-center gap-1 text-xs text-[#38bdf8] transition hover:text-[#7dd3fc] disabled:cursor-not-allowed disabled:opacity-50'
                  >
                    <RotateCcw className='h-3.5 w-3.5' />
                    使用登录账户 Key
                  </button>
                ) : null}
              </div>

              <div className='space-y-2'>
                <label htmlFor='claude-install-model' className='font-mono text-xs text-[#6f8096]'>
                  模型
                </label>
                <select
                  id='claude-install-model'
                  value={model}
                  onChange={(event) => setModel(event.target.value)}
                  className='install-page-input h-11 w-full rounded-lg border border-white/10 bg-[#0a0d13] px-3 font-mono text-sm text-white outline-none focus:border-[#38bdf8]/50'
                >
                  {CLAUDE_MODELS.map((option) => (
                    <option key={option} value={option}>
                      {option}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            <div className='space-y-3'>
              <div>
                <label className='font-mono text-xs text-[#6f8096]'>Anthropic Base URL</label>
                <div className='mt-2 flex min-h-11 items-center rounded-lg border border-white/10 bg-[#0a0d13] px-3 font-mono text-sm text-white'>
                  {CLAUDE_BASE_URL}
                </div>
              </div>
            </div>
          </div>
        </section>

        <section className='grid gap-4 lg:grid-cols-2'>
          {CLAUDE_PLATFORMS.map((platform) => {
            const Icon = platform.icon;
            const command = commandByPlatform[platform.id];

            return (
              <div
                key={platform.id}
                className='rounded-lg border border-white/10 bg-[#0d1118] shadow-[0_28px_90px_rgba(0,0,0,0.35)]'
              >
                <div className='border-b border-white/10 px-6 pb-4 pt-6'>
                  <div className='flex items-center gap-3 text-lg font-semibold text-white'>
                    <Icon className='h-5 w-5 text-[#38bdf8]' />
                    Claude {platform.label}
                  </div>
                  <p className='mt-2 text-sm text-[#7e91aa]'>复制后在对应系统终端执行。</p>
                </div>
                <div className='space-y-4 px-6 pb-6 pt-5'>
                  <button
                    type='button'
                    onClick={() => copyCommand(platform.id, command)}
                    className='inline-flex h-11 items-center gap-2 rounded-lg border border-[#38bdf8]/35 bg-[#0b1f2e] px-4 text-sm font-semibold text-[#d8f3ff] shadow-[0_0_0_1px_rgba(56,189,248,0.08)] transition hover:bg-[#102f44] hover:text-white'
                  >
                    {copied === platform.id ? (
                      <Check className='h-4 w-4' />
                    ) : (
                      <Copy className='h-4 w-4' />
                    )}
                    {copied === platform.id ? '已复制' : '复制命令'}
                  </button>
                  <div className='rounded-lg border border-white/10 bg-[#090c12] p-4'>
                    <div className='mb-3 break-all font-mono text-[11px] uppercase tracking-[0.24em] text-[#6f8096]'>
                      model={model} | base={CLAUDE_BASE_URL}
                    </div>
                    <pre className='max-h-[32rem] overflow-auto whitespace-pre-wrap break-all font-mono text-xs leading-6 text-[#d7e1ee]'>
                      {command}
                    </pre>
                  </div>
                </div>
              </div>
            );
          })}
        </section>
      </div>
    </div>
  );
}
