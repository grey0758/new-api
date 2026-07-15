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

import { useMemo } from 'react';

function isModuleEnabled(modules, key) {
  const value = modules[key];
  if (key === 'pricing') {
    return typeof value === 'object' ? value.enabled : value;
  }
  if (typeof value === 'object' && value !== null) {
    return value.enabled !== false;
  }
  return value === true;
}

function getModuleLink(modules, key, fallback) {
  const value = modules[key];
  if (typeof value === 'object' && value !== null) {
    const link = typeof value.link === 'string' ? value.link.trim() : '';
    if (link) {
      return link;
    }
  }
  return fallback;
}

export const useNavigation = (t, docsLink, headerNavModules, userState) => {
  const mainNavLinks = useMemo(() => {
    const canUseOps = Number(userState?.user?.role || 0) >= 10;
    // 默认配置，如果没有传入配置则显示所有模块
    const defaultModules = {
      home: true,
      console: true,
      pricing: true,
      docs: true,
      install: true,
      ops: true,
      customerMaintenance: false,
      about: false,
    };

    // 使用传入的配置或默认配置
    const modules = {
      ...defaultModules,
      ...(headerNavModules || {}),
    };
    const installLink = getModuleLink(modules, 'install', '/install/');

    const allLinks = [
      {
        text: t('首页'),
        itemKey: 'home',
        to: '/',
      },
      {
        text: t('控制台'),
        itemKey: 'console',
        to: '/console',
      },
      {
        text: t('模型广场'),
        itemKey: 'pricing',
        to: '/pricing',
      },
      ...(docsLink
        ? [
            {
              text: t('文档'),
              itemKey: 'docs',
              isExternal: true,
              externalLink: docsLink,
            },
          ]
        : []),
      {
        text: t('点我安装'),
        itemKey: 'install',
        isExternal: true,
        externalLink: installLink,
      },
      {
        text: t('兑换运维'),
        itemKey: 'ops',
        to: '/newapi-ops',
      },
      {
        text: t('客户维护'),
        itemKey: 'customerMaintenance',
        to: '/opencodex-customer-maintenance',
      },
      {
        text: t('关于'),
        itemKey: 'about',
        to: '/about',
      },
    ];

    // 根据配置过滤导航链接
    return allLinks.filter((link) => {
      if (link.itemKey === 'docs') {
        return docsLink && isModuleEnabled(modules, 'docs');
      }
      if (link.itemKey === 'ops' || link.itemKey === 'customerMaintenance') {
        return canUseOps && isModuleEnabled(modules, link.itemKey);
      }
      return isModuleEnabled(modules, link.itemKey);
    });
  }, [t, docsLink, headerNavModules, userState?.user?.role]);

  return {
    mainNavLinks,
  };
};
