# IpPlanMap · 可复用 IP 规划「地址地图」组件

参考「华中大区地址管理平台」地址规划页，用 **React + Ant Design 5** 自研的图形化 IP 规划核心组件。
纯前端实现，不依赖任何第三方 IP 计算 / 绘图库（IP 数学为内置工具函数）。

## 文件说明

| 文件 | 用途 |
| --- | --- |
| `IpPlanMap.jsx` | **可复用组件源码**，拷进你的 React + antd5 项目即可使用 |
| `index.html`   | **最小演示，完全离线**：双击打开即可，无需联网（依赖都在本地 `lib/`） |
| `lib/`         | 本地化的运行时依赖（React / ReactDOM / dayjs / @ant-design/icons / antd / reset.css） |
| `demo.app.js`  | 演示应用预编译产物（JSX 已转成普通 JS），由 `build.js` 生成 |
| `demo.jsx` `demo-app-part.jsx` | 演示源码（共享主体 + 演示 App），`build.js` 据此重新生成 `demo.app.js` |
| `build.js`     | 一键构建脚本：下载依赖到 `lib/` + 预编译 `demo.app.js`（需联网执行一次） |
| `README.md`    | 本文档 |

> 说明：`IpPlanMap.jsx` 是打包项目用的组件源（ESM import）；演示页通过 `demo.app.js` 复用了同一份组件主体代码。

## 快速开始（打包项目）

```bash
npm i react react-dom antd @ant-design/icons
```

```jsx
import { useState } from 'react';
import IpPlanMap from './IpPlanMap';

function Page() {
  const [ips, setIps] = useState([
    // IP 对象，见下方「数据模型」
  ]);

  return (
    <IpPlanMap
      cidr="10.59.193.0/24"
      ips={ips}                       // 可选：不传则按网段自动生成
      subnets={[
        { cidr: '10.59.193.0/24', name: '综合办公' },
        { cidr: '10.59.194.0/24', name: '数据中心' },
      ]}                              // 可选：显示网段下拉切换
      onChange={(sel, { lastIp }) => setSelected(sel)}
      onAction={(action, ips) => console.log(action, ips)}
      onSave={(target, values) => console.log(target, values)}
      onSubnetChange={(cidr) => setCidr(cidr)}
      readOnly={false}
    />
  );
}
```

## Props

| Prop | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `cidr` | string | `10.59.193.0/24` | 网段，如 `10.59.193.0/24`。传入则受控（配合 `onSubnetChange`）；不传内部维护 |
| `ips` | IP[] | 自动生成 | IP 数据。传入则受控（增删改由父组件负责）；不传则按网段自动生成（全为未规划） |
| `subnets` | {cidr, name}[] | - | 网段下拉选项；配合上一段/下一段切换 |
| `value` | string[] | - | 受控选中 IP 数组 |
| `readOnly` | boolean | false | 只读：不可选择 / 不可操作 |
| `onChange` | (selected, {lastIp}) | - | 选中变化 |
| `onAction` | (action, ipObj[]) | - | 批量操作：`toStatic` / `toDynamic` / `toReserve` |
| `onSave` | (target, formValues) | - | 编辑弹窗保存 |
| `onSubnetChange` | (cidr) | - | 网段切换 |

组件内部自带：图例、统计（已规划 / 在线 / 已选中）、批量操作按钮、编辑弹窗、详情抽屉。其余未知 props 透传给 antd `Card`（如 `style`、`className`）。

## 数据模型（IP 对象）

```js
{
  ip: '10.59.193.60',           // 完整 IP
  host: 60,                     // 网段内主机序号（/24 即 0-255，用于格子序号与区间选择）
  status: 'static',             // 基础状态，见下方 IP_STATUS
  overlays: ['online'],         // 叠加状态数组：zombie / conflict / online（格子上右下角圆点角标）
  // —— 分配信息 ——
  hostname: 'PRINT-01',
  leaseStatus: '已分配', leaseStart: '2026-08-01 09:00', leaseEnd: '2026-09-01 09:00',
  // —— 扫描信息 ——
  deviceName: '', switchPort: '',
  // —— 管理信息 ——
  purpose: '共享打印机', user: '综合部', phone: '138-0000-0001',
  customAttrs: { 资产编号: 'ZC-100' },   // 自定义属性 {键: 值}
  remark: '',
}
```

## 状态与配色

**基础状态（格子底色）**：`network 网络` `broadcast 广播` `available 未规划` `planned 已规划` `static 静态` `dynamic 动态` `reserved 保留`

**叠加状态（圆点角标）**：`zombie 僵尸` `conflict 冲突` `online 在线`

配色常量 `IP_STATUS` / `OVERLAY_STATUS` 均已导出，可按需覆盖。

## 演示

直接双击打开 `index.html` 即可，**无需联网**（首次遇到白屏时按 F12 查看 Console；若提示"本地依赖 lib/ 未加载成功"，说明 `lib/` 目录缺失或被移动）。

页面预置了 `/24` 网段与各状态示例（在线、冲突、僵尸、DHCP 租约、资产信息等），可试玩：点击 / Shift+点击 选择 IP、转静态/动态/保留、编辑、查看详情、切换网段。

如需重新构建（例如修改了组件代码后刷新 `demo.app.js`），执行 `node build.js`（需联网一次下载依赖）。

## 备注

- Ant Design v5 的静态 `message` 在正式项目中建议配合 `<App>` 或 `ConfigProvider` 使用。
- 超出 `MAX_CELLS`（2048）的网段会提示改用更小网段，避免一次性渲染过万格子。
- 组件使用内联样式，**不依赖 Tailwind 等 CSS 框架**，可直接嵌入任何 React 站点。
