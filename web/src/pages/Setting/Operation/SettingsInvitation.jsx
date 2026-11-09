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

import React, { useEffect, useRef, useState } from 'react';
import { Button, Form, Typography } from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, showError, showSuccess } from '../../../helpers';

export default function SettingsInvitation({ options, refresh }) {
  const { t } = useTranslation();
  const form = useRef();
  const [loading, setLoading] = useState(false);
  const read = (key) => options[`invitation_setting.${key}`];
  useEffect(() => {
    form.current?.setValues({
      enabled: read('enabled') === 'true',
      site_origin: read('site_origin') || '',
      balance_percent: Number(read('balance_rate_bps') || 0) / 100,
      subscription_percent: Number(read('subscription_rate_bps') || 0) / 100,
    });
  }, [options]);

  const submit = async () => {
    const values = form.current.getValues();
    setLoading(true);
    try {
      const { data } = await API.put('/api/option/invitation', {
        enabled: values.enabled === true,
        site_origin: values.site_origin || '',
        balance_rate_bps: Math.round(Number(values.balance_percent || 0) * 100),
        subscription_rate_bps: Math.round(
          Number(values.subscription_percent || 0) * 100,
        ),
        enabled_at:
          Number(read('enabled_at') || 0) || Math.floor(Date.now() / 1000),
        min_redemption_issued_at: Number(read('min_redemption_issued_at') || 0),
      });
      if (!data.success) {
        showError(data.message);
        return;
      }
      showSuccess(t('保存成功'));
      await refresh();
    } catch {
      showError(t('保存失败，请重试'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Form
      getFormApi={(api) => {
        form.current = api;
      }}
    >
      <Form.Section text={t('邀请返佣')}>
        <Typography.Paragraph type='tertiary'>
          {t(
            '只在配置的站点启用。余额按实际到账额度、周卡和月卡按整张卡总额度计算；注册不发固定奖励，历史已完成交易不会自动补算。',
          )}
        </Typography.Paragraph>
        <Form.Switch field='enabled' label={t('开启邀请返佣')} />
        <Form.Input
          field='site_origin'
          label={t('邀请站点地址')}
          placeholder='https://api.open-codex.com'
        />
        <Form.InputNumber
          field='balance_percent'
          label={t('余额充值返佣比例')}
          min={0}
          max={100}
          step={0.01}
          suffix='%'
        />
        <Form.InputNumber
          field='subscription_percent'
          label={t('周卡、月卡总额度返佣比例')}
          min={0}
          max={100}
          step={0.01}
          suffix='%'
        />
        <Button theme='solid' onClick={submit} loading={loading}>
          {t('保存')}
        </Button>
      </Form.Section>
    </Form>
  );
}
