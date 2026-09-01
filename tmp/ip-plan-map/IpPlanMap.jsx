/**
 * IpPlanMap —— 可复用 IP 规划「地址地图」组件（React + Ant Design 5）
 *
 * 功能特性（对齐原始「华中大区地址管理平台」地址规划页）：
 *  1. 图形化地址地图：一个网段每个 IP 一个彩色格子，直观看到整个网段
 *  2. 状态着色：网络 / 广播 / 未规划 / 已规划 / 静态 / 动态 / 保留
 *  3. 叠加状态：僵尸 / 冲突 / 在线（格子右下角圆点角标）
 *  4. 选择：点击单选、Shift+点击 区间选择、已选统计、一键清空
 *  5. 批量操作：转静态 / 转动态 / 转保留 / 编辑 / 详情
 *  6. 编辑弹窗：主机名 / 状态 / 用途 / 使用人 / 电话 / 租约时间 / 自定义属性 / 备注
 *  7. 详情抽屉：分配信息 / 扫描信息 / 资产信息 / 管理信息
 *  8. 网段切换：下拉选择 + 上一段 / 下一段
 *  9. 图例：基础状态 + 叠加状态
 *
 * 依赖：react、react-dom、antd（v5）、@ant-design/icons（无任何第三方 IP 计算库，IP 数学为内置工具）
 *
 * 受控 / 非受控：
 *  - cidr  传入则受控，否则内部维护
 *  - ips   传入则受控（父组件负责增删改），否则内部按网段自动生成
 *  - value 传入则受控（选中 IP 数组），否则内部维护
 *
 * 事件：
 *  - onChange(selectedIp[], { lastIp })    选中变化
 *  - onAction(action, ipObj[])             批量操作 action: toStatic | toDynamic | toReserve
 *  - onSave(targetIpObj[], formValues)     编辑弹窗保存
 *  - onSubnetChange(cidr)                  网段切换
 *
 * 说明：Ant Design v5 的静态 message 建议配合 <App> 或 ConfigProvider 使用。
 */

import React from 'react';
import * as antd from 'antd';
import * as antdIcons from '@ant-design/icons';

/* =========================================================
 * 以下为「共享主体」：index.html 演示页内联了等价副本，请保持同步
 * ========================================================= */
const { useState, useMemo, useCallback, useEffect } = React;
const {
  Card, Button, Space, Select, Tooltip, Tag, Divider, Drawer, Modal,
  Form, Input, Alert, message, Descriptions, Row, Col,
} = antd;
const {
  LeftOutlined, RightOutlined, EditOutlined, InfoCircleOutlined,
  RetweetOutlined, LockOutlined, ClearOutlined,
} = antdIcons;

/* ---------------- IP 工具（纯函数，无第三方依赖） ---------------- */

/** '10.1.2.3' -> 167772163 */
function ipToInt(ip) {
  return ip.split('.').reduce((acc, o) => ((acc << 8) >>> 0) + parseInt(o, 10), 0) >>> 0;
}

/** 167772163 -> '10.1.2.3' */
function intToIp(n) {
  return [n >>> 24, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.');
}

/** 解析 '10.59.193.0/24'，返回 网络号/广播号/主机数 等 */
function parseCidr(cidr) {
  const parts = String(cidr).split('/');
  const bits = parseInt(parts[1] || '32', 10);
  const ipInt = ipToInt(parts[0]);
  const mask = bits === 0 ? 0 : (0xffffffff << (32 - bits)) >>> 0;
  const network = (ipInt & mask) >>> 0;
  const broadcast = (network | (~mask >>> 0)) >>> 0;
  return { ip: parts[0], bits, ipInt, mask, network, broadcast, hostCount: broadcast - network + 1 };
}

/** 按网段生成默认 IP 对象数组（网络/广播地址自动标记，其余为未规划） */
function makeDefaultIps(cidr) {
  const { network, broadcast } = parseCidr(cidr);
  const list = [];
  for (let n = network; n <= broadcast; n++) {
    const ip = intToIp(n);
    list.push({
      ip,
      host: n - network,                                  // 网段内主机序号（/24 即 0-255）
      status: n === network ? 'network' : n === broadcast ? 'broadcast' : 'available',
      overlays: [],                                       // 叠加状态：zombie / conflict / online
      // 分配信息
      hostname: '', leaseStatus: '', leaseStart: '', leaseEnd: '',
      // 扫描信息
      deviceName: '', switchPort: '',
      // 管理信息
      purpose: '', user: '', phone: '',
      customAttrs: {},                                    // 自定义属性 {键: 值}
      remark: '',
    });
  }
  return list;
}

/* ---------------- 状态与配色 ---------------- */

/** 基础状态：决定格子底色（颜色沿用原平台视觉） */
const IP_STATUS = {
  network:   { label: '网络',   color: '#637196' },
  broadcast: { label: '广播',   color: '#637196' },
  available: { label: '未规划', color: '#9DBEFF' },
  planned:   { label: '已规划', color: '#BE86E4' },
  static:    { label: '静态',   color: '#69C0FF' },
  dynamic:   { label: '动态',   color: '#FFD666' },
  reserved:  { label: '保留',   color: '#FF9C6E' },
};

/** 叠加状态：格子上以圆点角标展示 */
const OVERLAY_STATUS = {
  zombie:   { label: '僵尸', color: '#C41D7F' },
  conflict: { label: '冲突', color: '#FF6262' },
  online:   { label: '在线', color: '#21BF86' },
};

const STATUS_DISABLED = ['network', 'broadcast']; // 不可操作的状态
const CELL_SIZE = 32;                               // 每格像素
const MAX_CELLS = 2048;                             // 图形化展示上限（超过提示改用更小网段）

/* ---------------- 地址地图组件 ---------------- */

function IpPlanMap(props) {
  const {
    cidr, ips, subnets, value, readOnly,
    onChange, onAction, onSave, onSubnetChange,
  } = props;
  const rest = Object.keys(props).reduce((acc, k) => {
    if (!['cidr', 'ips', 'subnets', 'value', 'readOnly', 'onChange', 'onAction', 'onSave', 'onSubnetChange'].includes(k)) acc[k] = props[k];
    return acc;
  }, {});

  /* 网段：受控 or 内部 */
  const [cidrState, setCidrState] = useState(cidr || '10.59.193.0/24');
  const currentCidr = cidr || cidrState;

  /* IP 列表：受控 or 内部 */
  const [internalIps, setInternalIps] = useState(() => makeDefaultIps(currentCidr));
  const ipList = ips || internalIps;

  useEffect(() => {
    if (!ips) {
      setInternalIps(makeDefaultIps(currentCidr));
      if (!('value' in props)) setSelState([]);
    }
  }, [currentCidr]); // eslint-disable-line react-hooks/exhaustive-deps

  /* 选中：受控 or 内部 */
  const [selState, setSelState] = useState(value || []);
  const currentSelected = 'value' in props ? (value || []) : selState;
  const selectedSet = useMemo(() => new Set(currentSelected), [currentSelected]);

  const hostToIp = useMemo(() => {
    const m = new Map();
    ipList.forEach(p => m.set(p.host, p));
    return m;
  }, [ipList]);

  const [lastClickHost, setLastClickHost] = useState(null);

  const updateSelected = useCallback((next, lastIp) => {
    if (!('value' in props)) setSelState(next);
    if (onChange) onChange(next, { lastIp });
  }, [onChange, props]);

  const handleCellClick = useCallback((ipObj, evt) => {
    if (readOnly || STATUS_DISABLED.includes(ipObj.status)) return;
    let next;
    if (evt.shiftKey && lastClickHost != null) {
      // Shift+点击：区间选择
      const lo = Math.min(ipObj.host, lastClickHost);
      const hi = Math.max(ipObj.host, lastClickHost);
      const range = [];
      for (let h = lo; h <= hi; h++) {
        const p = hostToIp.get(h);
        if (p && !STATUS_DISABLED.includes(p.status)) range.push(p.ip);
      }
      next = Array.from(new Set([...currentSelected, ...range]));
    } else if (selectedSet.has(ipObj.ip)) {
      next = currentSelected.filter(x => x !== ipObj.ip);
    } else {
      next = [...currentSelected, ipObj.ip];
    }
    setLastClickHost(ipObj.host);
    updateSelected(next, ipObj.ip);
  }, [readOnly, lastClickHost, hostToIp, currentSelected, selectedSet, updateSelected]);

  const selectedIps = useMemo(
    () => currentSelected.map(ip => hostToIp.get(ip)).filter(Boolean),
    [currentSelected, hostToIp]
  );

  /* 批量改状态：转静态 / 转动态 / 转保留 */
  const applyStatus = useCallback((status) => {
    if (readOnly || !selectedIps.length) return;
    const ids = new Set(selectedIps.map(p => p.ip));
    if (!ips) setInternalIps(prev => prev.map(p => (ids.has(p.ip) ? { ...p, status } : p)));
    const actionName = { static: 'toStatic', dynamic: 'toDynamic', reserved: 'toReserve' }[status];
    if (onAction) onAction(actionName, selectedIps);
    message.success(`已将 ${selectedIps.length} 个 IP 转为「${IP_STATUS[status].label}」`);
  }, [selectedIps, ips, onAction, readOnly]);

  /* ---- 编辑弹窗 ---- */
  const [form] = Form.useForm();
  const [editOpen, setEditOpen] = useState(false);
  const [editing, setEditing] = useState([]);

  const openEdit = useCallback(() => {
    if (readOnly || !selectedIps.length) return;
    const first = selectedIps[0];
    setEditing(selectedIps);
    form.setFieldsValue({
      hostname: first.hostname || '',
      status: first.status,
      purpose: first.purpose || '',
      user: first.user || '',
      phone: first.phone || '',
      leaseStart: first.leaseStart || '',
      leaseEnd: first.leaseEnd || '',
      customAttrText: Object.entries(first.customAttrs || {}).map(([k, v]) => `${k}=${v}`).join('\n'),
      remark: first.remark || '',
    });
    setEditOpen(true);
  }, [selectedIps, form, readOnly]);

  const handleEditOk = useCallback(() => {
    const vals = form.getFieldsValue();
    const customAttrs = {};
    String(vals.customAttrText || '').split('\n').forEach(line => {
      const i = line.indexOf('=');
      if (i > 0) customAttrs[line.slice(0, i).trim()] = line.slice(i + 1).trim();
    });
    const ids = new Set(editing.map(e => e.ip));
    if (!ips) {
      setInternalIps(prev => prev.map(p =>
        ids.has(p.ip) ? {
          ...p,
          hostname: vals.hostname, status: vals.status, purpose: vals.purpose,
          user: vals.user, phone: vals.phone,
          leaseStart: vals.leaseStart, leaseEnd: vals.leaseEnd,
          customAttrs, remark: vals.remark,
        } : p
      ));
    }
    if (onSave) onSave(editing, { ...vals, customAttrs });
    message.success(`已保存 ${editing.length} 个 IP 的信息`);
    setEditOpen(false);
  }, [form, editing, ips, onSave]);

  /* ---- 详情抽屉 ---- */
  const [detail, setDetail] = useState(null);
  const openDetail = useCallback(() => {
    if (readOnly) return;
    if (selectedIps.length === 1) setDetail(selectedIps[0]);
    else message.info('请先只选中 1 个 IP 后再查看详情');
  }, [selectedIps, readOnly]);

  /* ---- 网段切换 ---- */
  const changeSubnet = useCallback((next) => {
    if (onSubnetChange) { onSubnetChange(next); return; }
    setCidrState(next);
    if (!('value' in props)) setSelState([]);
  }, [onSubnetChange, props]);

  const prevSubnet = useCallback(() => {
    const idx = (subnets || []).findIndex(s => s.cidr === currentCidr);
    if (subnets && subnets.length && idx > 0) changeSubnet(subnets[idx - 1].cidr);
    else if (!subnets) {
      const p = parseCidr(currentCidr);
      const next = p.network - p.hostCount;
      if (next >= 0) changeSubnet(`${intToIp(next)}/${p.bits}`);
    }
  }, [subnets, currentCidr, changeSubnet]);

  const nextSubnet = useCallback(() => {
    const idx = (subnets || []).findIndex(s => s.cidr === currentCidr);
    if (subnets && subnets.length && idx >= 0 && idx < subnets.length - 1) changeSubnet(subnets[idx + 1].cidr);
    else if (!subnets) {
      const p = parseCidr(currentCidr);
      changeSubnet(`${intToIp(p.network + p.hostCount)}/${p.bits}`);
    }
  }, [subnets, currentCidr, changeSubnet]);

  const subnetOptions = useMemo(
    () => (subnets || []).map(s => ({ label: s.name ? `${s.name}（${s.cidr}）` : s.cidr, value: s.cidr })),
    [subnets]
  );

  /* ---- 统计 ---- */
  const stats = useMemo(() => {
    const planned = ipList.filter(p => ['static', 'dynamic', 'reserved', 'planned'].includes(p.status)).length;
    const online = ipList.filter(p => (p.overlays || []).includes('online') || p.status === 'online').length;
    return { total: ipList.length, planned, online, selected: currentSelected.length };
  }, [ipList, currentSelected]);

  /* ---- 边界保护 ---- */
  const parsed = parseCidr(currentCidr);
  if (isNaN(parsed.hostCount) || parsed.hostCount < 1) {
    return <Alert type="error" showIcon message={`网段格式错误：${currentCidr}`} />;
  }
  if (parsed.hostCount > MAX_CELLS) {
    return (
      <Alert type="warning" showIcon
        message={`网段 ${currentCidr} 共 ${parsed.hostCount} 个地址，超出图形化展示上限（${MAX_CELLS}），建议规划更小的网段。`} />
    );
  }

  const legendBase = Object.keys(IP_STATUS);
  const legendOverlay = Object.keys(OVERLAY_STATUS);

  const tooltipTitle = (p) => {
    const s = (IP_STATUS[p.status] || {}).label || p.status;
    const ovs = (p.overlays || []).map(o => (OVERLAY_STATUS[o] || {}).label || o).join('、');
    return (
      <div>
        <div style={{ fontWeight: 600 }}>{p.ip}</div>
        <div>状态：{s}{ovs ? `（叠加：${ovs}）` : ''}</div>
        {p.hostname ? <div>主机名：{p.hostname}</div> : null}
        {p.user ? <div>使用人：{p.user}</div> : null}
      </div>
    );
  };

  const showNav = (subnets && subnets.length) || onSubnetChange;

  return (
    <Card
      {...rest}
      title={
        <Space size={8}>
          <span>地址规划</span>
          <Tag color="blue">{currentCidr}</Tag>
          {readOnly ? <Tag>只读</Tag> : null}
        </Space>
      }
      extra={showNav ? (
        <Space size={4}>
          <Button size="small" icon={<LeftOutlined />} onClick={prevSubnet} aria-label="上一个网段" />
          <Select
            size="small"
            value={currentCidr}
            options={subnetOptions}
            onChange={changeSubnet}
            style={{ width: 210 }}
            placeholder="选择网段"
          />
          <Button size="small" icon={<RightOutlined />} onClick={nextSubnet} aria-label="下一个网段" />
        </Space>
      ) : null}
      size="small"
    >
      {/* 统计 */}
      <Space size={28} wrap style={{ marginBottom: 12 }}>
        <span><b style={{ color: '#BE86E4' }}>{stats.planned}</b> 已规划IP数</span>
        <span><b style={{ color: '#21BF86' }}>{stats.online}</b> 在线IP数</span>
        <span><b style={{ color: '#0065FF' }}>{stats.selected}</b> 已选中</span>
        <span style={{ color: '#A1A7C4' }}>共 {stats.total} 个地址</span>
      </Space>

      {/* 操作栏 */}
      <Space wrap style={{ marginBottom: 12 }}>
        <Button size="small" type="primary" icon={<RetweetOutlined />}
          disabled={readOnly || !stats.selected} onClick={() => applyStatus('static')}>转静态</Button>
        <Button size="small" icon={<RetweetOutlined />}
          disabled={readOnly || !stats.selected} onClick={() => applyStatus('dynamic')}>转动态</Button>
        <Button size="small" icon={<LockOutlined />}
          disabled={readOnly || !stats.selected} onClick={() => applyStatus('reserved')}>转保留</Button>
        <Button size="small" icon={<EditOutlined />}
          disabled={readOnly || !stats.selected} onClick={openEdit}>编辑</Button>
        <Button size="small" icon={<InfoCircleOutlined />}
          disabled={readOnly || stats.selected !== 1} onClick={openDetail}>详情</Button>
        {stats.selected > 0 && (
          <Button size="small" danger icon={<ClearOutlined />}
            onClick={() => updateSelected([], null)}>清空选择</Button>
        )}
      </Space>

      {/* 图例 */}
      <Space wrap size={[14, 6]} style={{ marginBottom: 12, fontSize: 12, color: '#A1A7C4' }}>
        {legendBase.map(k => (
          <span key={k} style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
            <span style={{
              width: 12, height: 12, borderRadius: 3, backgroundColor: IP_STATUS[k].color,
              display: 'inline-block', opacity: STATUS_DISABLED.includes(k) ? 0.75 : 1,
            }} />
            {IP_STATUS[k].label}
          </span>
        ))}
        <Divider type="vertical" style={{ margin: '0 2px' }} />
        <span>叠加状态：</span>
        {legendOverlay.map(k => (
          <span key={k} style={{ display: 'inline-flex', alignItems: 'center', gap: 5 }}>
            <span style={{
              width: 12, height: 12, borderRadius: '50%', backgroundColor: OVERLAY_STATUS[k].color,
              display: 'inline-block',
            }} />
            {OVERLAY_STATUS[k].label}
          </span>
        ))}
      </Space>

      <Divider style={{ margin: '0 0 12px' }} />

      {/* 地址地图 */}
      <div style={{
        display: 'flex', flexWrap: 'wrap', gap: 4,
        maxHeight: 420, overflow: 'auto', paddingBottom: 4,
      }}>
        {ipList.map(ipObj => {
          const meta = IP_STATUS[ipObj.status] || IP_STATUS.available;
          const disabled = readOnly || STATUS_DISABLED.includes(ipObj.status);
          const isSel = selectedSet.has(ipObj.ip);
          const overlays = ipObj.overlays || [];
          return (
            <Tooltip key={ipObj.ip} title={tooltipTitle(ipObj)} mouseEnterDelay={0.2}>
              <div
                onClick={e => handleCellClick(ipObj, e)}
                style={{
                  width: CELL_SIZE, height: CELL_SIZE, fontSize: 11, borderRadius: 4,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  backgroundColor: meta.color, color: '#fff',
                  userSelect: 'none', position: 'relative',
                  fontVariantNumeric: 'tabular-nums', lineHeight: 1, boxSizing: 'border-box',
                  border: isSel ? '2px dashed #000' : '1px solid #e6e9f4',
                  cursor: disabled ? 'not-allowed' : 'pointer',
                  opacity: disabled ? 0.8 : 1,
                }}
              >
                {ipObj.host}
                {overlays.slice(0, 2).map((ov, i) => {
                  const om = OVERLAY_STATUS[ov];
                  if (!om) return null;
                  return (
                    <span key={ov} style={{
                      position: 'absolute', right: 2 + i * 9, bottom: 2,
                      width: 7, height: 7, borderRadius: '50%',
                      backgroundColor: om.color, border: '1px solid #fff',
                    }} />
                  );
                })}
              </div>
            </Tooltip>
          );
        })}
      </div>

      {/* 底部提示 */}
      <div style={{ marginTop: 10, fontSize: 12, color: '#A1A7C4' }}>
        点击选择 IP · Shift+点击 区间选择 · 网络/广播地址不可操作{readOnly ? ' · 当前为只读模式' : ''}
      </div>

      {/* 编辑弹窗 */}
      <Modal
        title={editing.length > 1 ? `编辑 ${editing.length} 个 IP` : `编辑 IP · ${(editing[0] || {}).ip || ''}`}
        open={editOpen}
        onOk={handleEditOk}
        onCancel={() => setEditOpen(false)}
        okText="保存"
        width={540}
      >
        <Form form={form} layout="vertical">
          <Form.Item label="状态" name="status">
            <Select
              options={Object.entries(IP_STATUS)
                .filter(([k]) => !STATUS_DISABLED.includes(k))
                .map(([k, v]) => ({ label: v.label, value: k }))}
            />
          </Form.Item>
          <Row gutter={12}>
            <Col span={12}>
              <Form.Item label="主机名" name="hostname"><Input placeholder="如 PC-001" /></Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="用途" name="purpose"><Input placeholder="办公 / 服务器 / 打印机…" /></Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="使用人" name="user"><Input /></Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="电话" name="phone"><Input /></Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="租约开始" name="leaseStart"><Input placeholder="2026-08-01 09:00" /></Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item label="租约到期" name="leaseEnd"><Input placeholder="2026-09-01 09:00" /></Form.Item>
            </Col>
          </Row>
          <Form.Item label="自定义属性" name="customAttrText" extra="每行一条，格式：键=值">
            <Input.TextArea rows={2} placeholder={'资产编号=ZC-001\n位置=三楼机房'} />
          </Form.Item>
          <Form.Item label="备注" name="remark">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>

      {/* 详情抽屉 */}
      <Drawer title={`IP 详情 · ${(detail || {}).ip || ''}`} width={440} open={!!detail} onClose={() => setDetail(null)}>
        {detail && (
          <div>
            <Space wrap style={{ marginBottom: 12 }}>
              <Tag color={IP_STATUS[detail.status] ? IP_STATUS[detail.status].color : undefined}>
                {(IP_STATUS[detail.status] || {}).label || detail.status}
              </Tag>
              {(detail.overlays || []).map(o => (
                <Tag key={o} color={OVERLAY_STATUS[o] ? OVERLAY_STATUS[o].color : undefined}>
                  {(OVERLAY_STATUS[o] || {}).label || o}
                </Tag>
              ))}
            </Space>

            <Divider orientation="left" plain style={{ margin: '8px 0' }}>分配信息</Divider>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="地址">{detail.ip}</Descriptions.Item>
              <Descriptions.Item label="主机名">{detail.hostname || '-'}</Descriptions.Item>
              <Descriptions.Item label="租约状态">{detail.leaseStatus || '-'}</Descriptions.Item>
              <Descriptions.Item label="租约时间">{detail.leaseStart || '-'}</Descriptions.Item>
              <Descriptions.Item label="到期时间">{detail.leaseEnd || '-'}</Descriptions.Item>
            </Descriptions>

            <Divider orientation="left" plain style={{ margin: '12px 0' }}>扫描信息</Divider>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="设备名称">{detail.deviceName || '-'}</Descriptions.Item>
              <Descriptions.Item label="交换机端口">{detail.switchPort || '-'}</Descriptions.Item>
            </Descriptions>

            <Divider orientation="left" plain style={{ margin: '12px 0' }}>资产信息</Divider>
            <Descriptions column={1} size="small" bordered>
              {Object.keys(detail.customAttrs || {}).length ? (
                Object.entries(detail.customAttrs).map(([k, v]) => (
                  <Descriptions.Item key={k} label={k}>{String(v)}</Descriptions.Item>
                ))
              ) : (
                <Descriptions.Item label="资产信息">-</Descriptions.Item>
              )}
            </Descriptions>

            <Divider orientation="left" plain style={{ margin: '12px 0' }}>管理信息</Divider>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="用途">{detail.purpose || '-'}</Descriptions.Item>
              <Descriptions.Item label="使用人">{detail.user || '-'}</Descriptions.Item>
              <Descriptions.Item label="电话">{detail.phone || '-'}</Descriptions.Item>
              <Descriptions.Item label="备注">{detail.remark || '-'}</Descriptions.Item>
            </Descriptions>
          </div>
        )}
      </Drawer>
    </Card>
  );
}

export default IpPlanMap;
export { ipToInt, intToIp, parseCidr, makeDefaultIps, IP_STATUS, OVERLAY_STATUS };
