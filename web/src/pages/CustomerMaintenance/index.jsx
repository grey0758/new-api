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

import React, { useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Input,
  Modal,
  Pagination,
  Select,
  Space,
  Switch,
  Table,
  TabPane,
  Tabs,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconCopy,
  IconEdit,
  IconRefresh,
  IconSearch,
} from '@douyinfe/semi-icons';
import { useTranslation } from 'react-i18next';
import {
  API,
  copy,
  showError,
  showSuccess,
  timestamp2string,
} from '../../helpers';

const { Text, Title } = Typography;

const emptyContact = {
  user_id: 0,
  username: '',
  display_name: '',
  wechat_account: '',
  wechat_username: '',
  notes: '',
  push_enabled: true,
};

function customerName(record) {
  return (
    record?.display_name || record?.username || `#${record?.user_id || '-'}`
  );
}

function statusTag(status, t) {
  if (status === 'acknowledged') {
    return <Tag color='green'>{t('已处理')}</Tag>;
  }
  if (status === 'superseded') {
    return <Tag color='grey'>{t('已续订排除')}</Tag>;
  }
  return <Tag color='orange'>{t('待处理')}</Tag>;
}

function pushStatusTag(status, t) {
  if (status === 'sent') {
    return <Tag color='green'>{t('已推送')}</Tag>;
  }
  if (status === 'failed') {
    return <Tag color='red'>{t('推送失败')}</Tag>;
  }
  if (status === 'queued') {
    return <Tag color='blue'>{t('等待推送')}</Tag>;
  }
  return <Tag color='grey'>{t('机器人接口待接入')}</Tag>;
}

const CustomerMaintenance = () => {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState('customers');
  const [customers, setCustomers] = useState([]);
  const [customersLoading, setCustomersLoading] = useState(false);
  const [customerKeyword, setCustomerKeyword] = useState('');
  const [customerPage, setCustomerPage] = useState(1);
  const [customerPageSize, setCustomerPageSize] = useState(20);
  const [customerTotal, setCustomerTotal] = useState(0);

  const [notifications, setNotifications] = useState([]);
  const [notificationsLoading, setNotificationsLoading] = useState(false);
  const [notificationKeyword, setNotificationKeyword] = useState('');
  const [notificationStatus, setNotificationStatus] = useState('');
  const [notificationPage, setNotificationPage] = useState(1);
  const [notificationPageSize, setNotificationPageSize] = useState(20);
  const [notificationTotal, setNotificationTotal] = useState(0);
  const [backfilling, setBackfilling] = useState(false);

  const [contactVisible, setContactVisible] = useState(false);
  const [contactSaving, setContactSaving] = useState(false);
  const [contactDraft, setContactDraft] = useState(emptyContact);

  const loadCustomers = async (
    page = customerPage,
    pageSize = customerPageSize,
    keyword = customerKeyword,
  ) => {
    setCustomersLoading(true);
    try {
      const response = await API.get(
        '/api/opencodex/customer-maintenance/customers',
        {
          params: {
            p: page,
            page_size: pageSize,
            keyword: keyword.trim(),
          },
        },
      );
      const { success, message, data } = response.data;
      if (!success) {
        showError(message);
        return;
      }
      setCustomers(
        (data.items || []).map((item) => ({ ...item, key: item.user_id })),
      );
      setCustomerPage(data.page || page);
      setCustomerTotal(data.total || 0);
    } catch (error) {
      showError(error);
    } finally {
      setCustomersLoading(false);
    }
  };

  const loadNotifications = async (
    page = notificationPage,
    pageSize = notificationPageSize,
    keyword = notificationKeyword,
    status = notificationStatus,
  ) => {
    setNotificationsLoading(true);
    try {
      const response = await API.get(
        '/api/opencodex/customer-maintenance/notifications',
        {
          params: {
            p: page,
            page_size: pageSize,
            keyword: keyword.trim(),
            status,
          },
        },
      );
      const { success, message, data } = response.data;
      if (!success) {
        showError(message);
        return;
      }
      setNotifications(
        (data.items || []).map((item) => ({ ...item, key: item.id })),
      );
      setNotificationPage(data.page || page);
      setNotificationTotal(data.total || 0);
    } catch (error) {
      showError(error);
    } finally {
      setNotificationsLoading(false);
    }
  };

  useEffect(() => {
    loadCustomers(1, 20, '');
    loadNotifications(1, 20, '', '');
  }, []);

  const openContactEditor = (record) => {
    setContactDraft({
      user_id: record.user_id,
      username: record.username || '',
      display_name: record.display_name || '',
      wechat_account: record.wechat_account || '',
      wechat_username: record.wechat_username || '',
      notes: record.notes || '',
      push_enabled:
        record.push_enabled === undefined
          ? record.contact_push_enabled !== false
          : record.push_enabled !== false,
    });
    setContactVisible(true);
  };

  const saveContact = async () => {
    if (!contactDraft.user_id) {
      return;
    }
    setContactSaving(true);
    try {
      const response = await API.put(
        `/api/opencodex/customer-maintenance/customers/${contactDraft.user_id}/contact`,
        {
          wechat_account: contactDraft.wechat_account,
          wechat_username: contactDraft.wechat_username,
          notes: contactDraft.notes,
          push_enabled: contactDraft.push_enabled,
        },
      );
      const { success, message } = response.data;
      if (!success) {
        showError(message);
        return;
      }
      showSuccess(t('微信绑定已保存'));
      setContactVisible(false);
      await Promise.all([
        loadCustomers(customerPage, customerPageSize, customerKeyword),
        loadNotifications(
          notificationPage,
          notificationPageSize,
          notificationKeyword,
          notificationStatus,
        ),
      ]);
    } catch (error) {
      showError(error);
    } finally {
      setContactSaving(false);
    }
  };

  const backfillExpiredMonthlyCards = () => {
    Modal.confirm({
      title: t('同步最近两个月到期月卡'),
      content: t(
        '只会为当前仍处于到期状态的普通用户生成幂等通知，已续费用户不会加入。',
      ),
      onOk: async () => {
        setBackfilling(true);
        try {
          const response = await API.post(
            '/api/opencodex/customer-maintenance/notifications/backfill?months=2',
          );
          const { success, message, data } = response.data;
          if (!success) {
            showError(message);
            return;
          }
          showSuccess(
            t(
              '已生成 {{created}} 条通知，已存在 {{existing}} 条，已排除续订 {{superseded}} 条',
              {
                created: data.created || 0,
                existing: data.existing || 0,
                superseded: data.superseded_renewals || 0,
              },
            ),
          );
          setActiveTab('notifications');
          await loadNotifications(
            1,
            notificationPageSize,
            notificationKeyword,
            notificationStatus,
          );
        } catch (error) {
          showError(error);
        } finally {
          setBackfilling(false);
        }
      },
    });
  };

  const acknowledgeNotification = async (record) => {
    try {
      const response = await API.post(
        `/api/opencodex/customer-maintenance/notifications/${record.id}/acknowledge`,
      );
      const { success, message } = response.data;
      if (!success) {
        showError(message);
        return;
      }
      showSuccess(t('通知已标记为已处理'));
      await loadNotifications(
        notificationPage,
        notificationPageSize,
        notificationKeyword,
        notificationStatus,
      );
    } catch (error) {
      showError(error);
    }
  };

  const copyNotification = async (record) => {
    if (await copy(record.content || '')) {
      showSuccess(t('复制成功'));
      return;
    }
    showError(t('复制失败，请手动复制'));
  };

  const customerColumns = useMemo(
    () => [
      {
        title: t('用户'),
        dataIndex: 'username',
        width: 210,
        render: (_, record) => (
          <div>
            <Text strong>{customerName(record)}</Text>
            <div>
              <Text type='tertiary'>
                #{record.user_id} · {record.username || '-'}
              </Text>
            </div>
          </div>
        ),
      },
      {
        title: t('邮箱'),
        dataIndex: 'email',
        width: 220,
        render: (value) => value || '-',
      },
      {
        title: t('用户组'),
        dataIndex: 'group',
        width: 110,
        render: (value) => <Tag color='blue'>{value || '-'}</Tag>,
      },
      {
        title: t('微信号'),
        dataIndex: 'wechat_account',
        width: 170,
        render: (value) => value || <Text type='tertiary'>{t('未绑定')}</Text>,
      },
      {
        title: t('微信用户名'),
        dataIndex: 'wechat_username',
        width: 170,
        render: (value) => value || '-',
      },
      {
        title: t('最近订阅'),
        dataIndex: 'latest_plan_title',
        width: 180,
        render: (value, record) => (
          <div>
            <div>{value || t('暂无订阅')}</div>
            {record.latest_subscription_status ? (
              <Text type='tertiary'>{record.latest_subscription_status}</Text>
            ) : null}
          </div>
        ),
      },
      {
        title: t('订阅日期'),
        dataIndex: 'latest_subscription_start_time',
        width: 190,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('到期时间'),
        dataIndex: 'latest_subscription_end_time',
        width: 190,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('通知开关'),
        dataIndex: 'push_enabled',
        width: 110,
        render: (value, record) => (
          <Tag color={record.wechat_account && value ? 'green' : 'grey'}>
            {record.wechat_account && value ? t('已启用') : t('未启用')}
          </Tag>
        ),
      },
      {
        title: t('操作'),
        dataIndex: 'operate',
        width: 120,
        fixed: 'right',
        render: (_, record) => (
          <Button
            size='small'
            theme='borderless'
            icon={<IconEdit />}
            onClick={() => openContactEditor(record)}
          >
            {t('编辑绑定')}
          </Button>
        ),
      },
    ],
    [t],
  );

  const notificationColumns = useMemo(
    () => [
      {
        title: t('用户'),
        dataIndex: 'username',
        width: 210,
        render: (_, record) => (
          <div>
            <Text strong>{customerName(record)}</Text>
            <div>
              <Text type='tertiary'>
                #{record.user_id} · {record.username || '-'}
              </Text>
            </div>
          </div>
        ),
      },
      {
        title: t('月卡'),
        dataIndex: 'plan_title',
        width: 180,
        render: (value) => value || '-',
      },
      {
        title: t('订阅日期'),
        dataIndex: 'subscription_start_time',
        width: 190,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('到期时间'),
        dataIndex: 'occurred_at',
        width: 190,
        render: (value) => (value ? timestamp2string(value) : '-'),
      },
      {
        title: t('微信号'),
        dataIndex: 'wechat_account',
        width: 160,
        render: (value) => value || <Tag color='orange'>{t('待绑定')}</Tag>,
      },
      {
        title: t('微信用户名'),
        dataIndex: 'wechat_username',
        width: 160,
        render: (value) => value || '-',
      },
      {
        title: t('消息内容'),
        dataIndex: 'content',
        width: 360,
        render: (value) => (
          <Text ellipsis={{ showTooltip: true }} style={{ maxWidth: 340 }}>
            {value}
          </Text>
        ),
      },
      {
        title: t('通知状态'),
        dataIndex: 'status',
        width: 110,
        render: (value) => statusTag(value, t),
      },
      {
        title: t('推送状态'),
        dataIndex: 'push_status',
        width: 150,
        render: (value) => pushStatusTag(value, t),
      },
      {
        title: t('操作'),
        dataIndex: 'operate',
        width: 250,
        fixed: 'right',
        render: (_, record) => (
          <Space spacing={4}>
            <Button
              size='small'
              theme='borderless'
              icon={<IconEdit />}
              onClick={() => openContactEditor(record)}
            >
              {t('绑定微信')}
            </Button>
            <Button
              size='small'
              theme='borderless'
              icon={<IconCopy />}
              onClick={() => copyNotification(record)}
            >
              {t('复制消息')}
            </Button>
            {record.status === 'pending' ? (
              <Button
                size='small'
                theme='borderless'
                type='primary'
                onClick={() => acknowledgeNotification(record)}
              >
                {t('标记已处理')}
              </Button>
            ) : null}
          </Space>
        ),
      },
    ],
    [t],
  );

  return (
    <div className='mt-[60px] px-2 pb-8'>
      <Card>
        <div className='flex flex-col gap-2 mb-4'>
          <Title heading={3}>{t('客户维护')}</Title>
          <Text type='secondary'>
            {t('微信资料与订阅通知存放在独立扩展表中，不修改官方用户表。')}
          </Text>
        </div>
        <Banner
          type='info'
          closeIcon={null}
          description={t(
            '消息推送位已预留为微信机器人通道；当前只记录待推送状态，不会主动调用外部接口。',
          )}
          className='!rounded-lg mb-4'
        />
        <Tabs activeKey={activeTab} onChange={setActiveTab} type='line'>
          <TabPane tab={t('客户列表')} itemKey='customers'>
            <div className='flex flex-col md:flex-row gap-2 justify-between mb-4'>
              <Space>
                <Input
                  value={customerKeyword}
                  onChange={setCustomerKeyword}
                  onEnterPress={() =>
                    loadCustomers(1, customerPageSize, customerKeyword)
                  }
                  prefix={<IconSearch />}
                  placeholder={t('搜索用户、邮箱、微信号或微信用户名')}
                  style={{ width: 320 }}
                  showClear
                />
                <Button
                  icon={<IconSearch />}
                  loading={customersLoading}
                  onClick={() =>
                    loadCustomers(1, customerPageSize, customerKeyword)
                  }
                >
                  {t('搜索')}
                </Button>
                <Button
                  icon={<IconRefresh />}
                  theme='light'
                  onClick={() =>
                    loadCustomers(
                      customerPage,
                      customerPageSize,
                      customerKeyword,
                    )
                  }
                >
                  {t('刷新')}
                </Button>
              </Space>
            </div>
            <Table
              columns={customerColumns}
              dataSource={customers}
              rowKey='user_id'
              loading={customersLoading}
              pagination={false}
              scroll={{ x: 'max-content' }}
              empty={t('暂无客户记录')}
            />
            <div className='flex justify-end mt-4'>
              <Pagination
                currentPage={customerPage}
                pageSize={customerPageSize}
                total={customerTotal}
                showSizeChanger
                pageSizeOpts={[10, 20, 50, 100]}
                onPageChange={(page) =>
                  loadCustomers(page, customerPageSize, customerKeyword)
                }
                onPageSizeChange={(size) => {
                  setCustomerPageSize(size);
                  loadCustomers(1, size, customerKeyword);
                }}
              />
            </div>
          </TabPane>
          <TabPane tab={t('订阅通知')} itemKey='notifications'>
            <div className='flex flex-col xl:flex-row gap-2 justify-between mb-4'>
              <Space wrap>
                <Button
                  type='primary'
                  loading={backfilling}
                  onClick={backfillExpiredMonthlyCards}
                >
                  {t('同步最近两个月到期月卡')}
                </Button>
                <Button
                  icon={<IconRefresh />}
                  theme='light'
                  onClick={() =>
                    loadNotifications(
                      notificationPage,
                      notificationPageSize,
                      notificationKeyword,
                      notificationStatus,
                    )
                  }
                >
                  {t('刷新')}
                </Button>
              </Space>
              <Space wrap>
                <Input
                  value={notificationKeyword}
                  onChange={setNotificationKeyword}
                  onEnterPress={() =>
                    loadNotifications(
                      1,
                      notificationPageSize,
                      notificationKeyword,
                      notificationStatus,
                    )
                  }
                  prefix={<IconSearch />}
                  placeholder={t('搜索通知ID、用户或微信资料')}
                  style={{ width: 280 }}
                  showClear
                />
                <Select
                  value={notificationStatus}
                  onChange={(value) => {
                    setNotificationStatus(value);
                    loadNotifications(
                      1,
                      notificationPageSize,
                      notificationKeyword,
                      value,
                    );
                  }}
                  style={{ width: 140 }}
                  optionList={[
                    { label: t('全部状态'), value: '' },
                    { label: t('待处理'), value: 'pending' },
                    { label: t('已处理'), value: 'acknowledged' },
                    { label: t('已续订排除'), value: 'superseded' },
                  ]}
                />
                <Button
                  icon={<IconSearch />}
                  loading={notificationsLoading}
                  onClick={() =>
                    loadNotifications(
                      1,
                      notificationPageSize,
                      notificationKeyword,
                      notificationStatus,
                    )
                  }
                >
                  {t('搜索')}
                </Button>
              </Space>
            </div>
            <Table
              columns={notificationColumns}
              dataSource={notifications}
              rowKey='id'
              loading={notificationsLoading}
              pagination={false}
              scroll={{ x: 'max-content' }}
              empty={t('暂无订阅通知')}
            />
            <div className='flex justify-end mt-4'>
              <Pagination
                currentPage={notificationPage}
                pageSize={notificationPageSize}
                total={notificationTotal}
                showSizeChanger
                pageSizeOpts={[10, 20, 50, 100]}
                onPageChange={(page) =>
                  loadNotifications(
                    page,
                    notificationPageSize,
                    notificationKeyword,
                    notificationStatus,
                  )
                }
                onPageSizeChange={(size) => {
                  setNotificationPageSize(size);
                  loadNotifications(
                    1,
                    size,
                    notificationKeyword,
                    notificationStatus,
                  );
                }}
              />
            </div>
          </TabPane>
        </Tabs>
      </Card>

      <Modal
        title={t('绑定微信资料')}
        visible={contactVisible}
        onCancel={() => setContactVisible(false)}
        onOk={saveContact}
        confirmLoading={contactSaving}
        okText={t('保存')}
        cancelText={t('取消')}
      >
        <div className='flex flex-col gap-4'>
          <div>
            <Text type='secondary'>{t('客户')}</Text>
            <div>
              <Text strong>{customerName(contactDraft)}</Text>
              <Text type='tertiary'> #{contactDraft.user_id}</Text>
            </div>
          </div>
          <div>
            <Text>{t('微信号')}</Text>
            <Input
              value={contactDraft.wechat_account}
              onChange={(value) =>
                setContactDraft((current) => ({
                  ...current,
                  wechat_account: value,
                }))
              }
              placeholder={t('填写后续联系或机器人识别使用的微信号')}
              maxLength={128}
              showClear
            />
          </div>
          <div>
            <Text>{t('微信用户名')}</Text>
            <Input
              value={contactDraft.wechat_username}
              onChange={(value) =>
                setContactDraft((current) => ({
                  ...current,
                  wechat_username: value,
                }))
              }
              placeholder={t('填写微信中显示的用户名或备注名')}
              maxLength={128}
              showClear
            />
          </div>
          <div>
            <Text>{t('备注')}</Text>
            <TextArea
              value={contactDraft.notes}
              onChange={(value) =>
                setContactDraft((current) => ({ ...current, notes: value }))
              }
              placeholder={t('记录客户维护备注')}
              maxCount={512}
              autosize={{ minRows: 3, maxRows: 6 }}
            />
          </div>
          <div className='flex items-center justify-between'>
            <div>
              <Text>{t('允许微信通知')}</Text>
              <div>
                <Text type='tertiary'>{t('机器人接入后才会实际推送')}</Text>
              </div>
            </div>
            <Switch
              checked={contactDraft.push_enabled}
              onChange={(checked) =>
                setContactDraft((current) => ({
                  ...current,
                  push_enabled: checked,
                }))
              }
            />
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default CustomerMaintenance;
