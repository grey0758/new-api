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
import { Banner, Button, Col, Form, Row, Spin } from '@douyinfe/semi-ui';
import { Info } from 'lucide-react';
import {
  API,
  removeTrailingSlash,
  showError,
  showSuccess,
} from '../../../helpers';
import { useTranslation } from 'react-i18next';

export default function SettingsPaymentGatewayWxDaEpay(props) {
  const { t } = useTranslation();
  const sectionTitle = props.hideSectionTitle ? undefined : t('wxDa 支付设置');
  const [loading, setLoading] = useState(false);
  const [inputs, setInputs] = useState({
    WxDaEpayEnabled: false,
    WxDaEpayAddress: '',
    WxDaEpayPid: '',
    WxDaEpaySignType: 'MD5',
    WxDaEpayMD5Key: '',
    WxDaEpayPlatformPublicKey: '',
    WxDaEpayMerchantPrivateKey: '',
    WxDaEpaySubmitPath: '',
    WxDaEpayAlipayEnabled: true,
    WxDaEpayWxpayEnabled: true,
  });
  const formApiRef = useRef(null);

  useEffect(() => {
    if (props.options && formApiRef.current) {
      const currentInputs = {
        WxDaEpayEnabled: !!props.options.WxDaEpayEnabled,
        WxDaEpayAddress: props.options.WxDaEpayAddress || '',
        WxDaEpayPid: props.options.WxDaEpayPid || '',
        WxDaEpaySignType: props.options.WxDaEpaySignType || 'MD5',
        WxDaEpayMD5Key: props.options.WxDaEpayMD5Key || '',
        WxDaEpayPlatformPublicKey:
          props.options.WxDaEpayPlatformPublicKey || '',
        WxDaEpayMerchantPrivateKey:
          props.options.WxDaEpayMerchantPrivateKey || '',
        WxDaEpaySubmitPath: props.options.WxDaEpaySubmitPath || '',
        WxDaEpayAlipayEnabled:
          props.options.WxDaEpayAlipayEnabled !== undefined
            ? !!props.options.WxDaEpayAlipayEnabled
            : true,
        WxDaEpayWxpayEnabled:
          props.options.WxDaEpayWxpayEnabled !== undefined
            ? !!props.options.WxDaEpayWxpayEnabled
            : true,
      };
      setInputs(currentInputs);
      formApiRef.current.setValues(currentInputs);
    }
  }, [props.options]);

  const submit = async () => {
    setLoading(true);
    try {
      const options = [
        { key: 'WxDaEpayEnabled', value: inputs.WxDaEpayEnabled },
        {
          key: 'WxDaEpayAddress',
          value: removeTrailingSlash(inputs.WxDaEpayAddress || ''),
        },
        { key: 'WxDaEpayPid', value: inputs.WxDaEpayPid || '' },
        { key: 'WxDaEpaySignType', value: inputs.WxDaEpaySignType || 'MD5' },
        { key: 'WxDaEpaySubmitPath', value: inputs.WxDaEpaySubmitPath || '' },
        {
          key: 'WxDaEpayAlipayEnabled',
          value: !!inputs.WxDaEpayAlipayEnabled,
        },
        {
          key: 'WxDaEpayWxpayEnabled',
          value: !!inputs.WxDaEpayWxpayEnabled,
        },
      ];

      if (inputs.WxDaEpayMD5Key) {
        options.push({ key: 'WxDaEpayMD5Key', value: inputs.WxDaEpayMD5Key });
      }
      if (inputs.WxDaEpayPlatformPublicKey) {
        options.push({
          key: 'WxDaEpayPlatformPublicKey',
          value: inputs.WxDaEpayPlatformPublicKey,
        });
      }
      if (inputs.WxDaEpayMerchantPrivateKey) {
        options.push({
          key: 'WxDaEpayMerchantPrivateKey',
          value: inputs.WxDaEpayMerchantPrivateKey,
        });
      }

      const results = await Promise.all(
        options.map((opt) => API.put('/api/option/', opt)),
      );
      const failed = results.find((res) => !res.data?.success);
      if (failed) {
        showError(failed.data?.message || t('更新失败'));
      } else {
        showSuccess(t('更新成功'));
        props.refresh && props.refresh();
      }
    } catch (error) {
      showError(t('更新失败'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Spin spinning={loading}>
      <Form
        initValues={inputs}
        onValueChange={setInputs}
        getFormApi={(api) => (formApiRef.current = api)}
      >
        <Form.Section text={sectionTitle}>
          <Banner
            type='info'
            icon={<Info size={16} />}
            description={t(
              'wxDa 支付为独立通道，不会影响现有易支付、Stripe、Creem、Waffo 等支付方式。',
            )}
            style={{ marginBottom: 16 }}
          />
          <Row gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch field='WxDaEpayEnabled' label={t('启用')} />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='WxDaEpayAlipayEnabled'
                label={t('启用支付宝')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Switch
                field='WxDaEpayWxpayEnabled'
                label={t('启用微信支付')}
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WxDaEpayAddress'
                label={t('接口地址')}
                placeholder='https://epayapi.wxda.net'
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Input
                field='WxDaEpayPid'
                label={t('商户 ID')}
                placeholder={t('例如：1001')}
              />
            </Col>
            <Col xs={24} sm={24} md={8} lg={8} xl={8}>
              <Form.Select
                field='WxDaEpaySignType'
                label={t('签名方式')}
                optionList={[
                  { label: 'MD5', value: 'MD5' },
                  { label: 'RSA', value: 'RSA' },
                ]}
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='WxDaEpayMD5Key'
                label={t('MD5 密钥')}
                placeholder={t('敏感信息不会发送到前端显示')}
                type='password'
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.Input
                field='WxDaEpaySubmitPath'
                label={t('提交路径')}
                placeholder='/submit.php'
                extraText={t(
                  '留空时 MD5 使用 /submit.php，RSA 使用 /api/pay/submit',
                )}
              />
            </Col>
          </Row>
          <Row
            gutter={{ xs: 8, sm: 16, md: 24, lg: 24, xl: 24, xxl: 24 }}
            style={{ marginTop: 16 }}
          >
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='WxDaEpayPlatformPublicKey'
                label={t('平台公钥')}
                placeholder={t('RSA 模式需要，敏感信息不会发送到前端显示')}
                rows={4}
              />
            </Col>
            <Col xs={24} sm={24} md={12} lg={12} xl={12}>
              <Form.TextArea
                field='WxDaEpayMerchantPrivateKey'
                label={t('商户私钥')}
                placeholder={t('RSA 模式需要，敏感信息不会发送到前端显示')}
                rows={4}
              />
            </Col>
          </Row>
          <Button onClick={submit} style={{ marginTop: 16 }}>
            {t('更新 wxDa 支付设置')}
          </Button>
        </Form.Section>
      </Form>
    </Spin>
  );
}
