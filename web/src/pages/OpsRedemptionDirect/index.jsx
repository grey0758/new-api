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

import React from 'react';
import OpsRedemptionWorkspace from '../../components/ops-redemption/OpsRedemptionWorkspace';

export default function OpsRedemptionDirect() {
  return (
    <div className='mt-[64px]'>
      <OpsRedemptionWorkspace
        title='兑换码直达页'
        subtitle='打开授权链接后会自动加载订阅并生成对应文案，适合直接复制发送。'
        autoGenerate
      />
    </div>
  );
}
