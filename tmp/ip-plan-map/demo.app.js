const antdIcons = window.icons;
/* =========================================================
 * 以下为「共享主体」：index.html 演示页内联了等价副本，请保持同步
 * ========================================================= */
const {
  useState,
  useMemo,
  useCallback,
  useEffect
} = React;
const {
  Card,
  Button,
  Space,
  Select,
  Tooltip,
  Tag,
  Divider,
  Drawer,
  Modal,
  Form,
  Input,
  Alert,
  message,
  Descriptions,
  Row,
  Col
} = antd;
const {
  LeftOutlined,
  RightOutlined,
  EditOutlined,
  InfoCircleOutlined,
  RetweetOutlined,
  LockOutlined,
  ClearOutlined
} = antdIcons;

/* ---------------- IP 工具（纯函数，无第三方依赖） ---------------- */

/** '10.1.2.3' -> 167772163 */
function ipToInt(ip) {
  return ip.split('.').reduce((acc, o) => (acc << 8 >>> 0) + parseInt(o, 10), 0) >>> 0;
}

/** 167772163 -> '10.1.2.3' */
function intToIp(n) {
  return [n >>> 24, n >>> 16 & 255, n >>> 8 & 255, n & 255].join('.');
}

/** 解析 '10.59.193.0/24'，返回 网络号/广播号/主机数 等 */
function parseCidr(cidr) {
  const parts = String(cidr).split('/');
  const bits = parseInt(parts[1] || '32', 10);
  const ipInt = ipToInt(parts[0]);
  const mask = bits === 0 ? 0 : 0xffffffff << 32 - bits >>> 0;
  const network = (ipInt & mask) >>> 0;
  const broadcast = (network | ~mask >>> 0) >>> 0;
  return {
    ip: parts[0],
    bits,
    ipInt,
    mask,
    network,
    broadcast,
    hostCount: broadcast - network + 1
  };
}

/** 按网段生成默认 IP 对象数组（网络/广播地址自动标记，其余为未规划） */
function makeDefaultIps(cidr) {
  const {
    network,
    broadcast
  } = parseCidr(cidr);
  const list = [];
  for (let n = network; n <= broadcast; n++) {
    const ip = intToIp(n);
    list.push({
      ip,
      host: n - network,
      // 网段内主机序号（/24 即 0-255）
      status: n === network ? 'network' : n === broadcast ? 'broadcast' : 'available',
      overlays: [],
      // 叠加状态：zombie / conflict / online
      // 分配信息
      hostname: '',
      leaseStatus: '',
      leaseStart: '',
      leaseEnd: '',
      // 扫描信息
      deviceName: '',
      switchPort: '',
      // 管理信息
      purpose: '',
      user: '',
      phone: '',
      customAttrs: {},
      // 自定义属性 {键: 值}
      remark: ''
    });
  }
  return list;
}

/* ---------------- 状态与配色 ---------------- */

/** 基础状态：决定格子底色（颜色沿用原平台视觉） */
const IP_STATUS = {
  network: {
    label: '网络',
    color: '#637196'
  },
  broadcast: {
    label: '广播',
    color: '#637196'
  },
  available: {
    label: '未规划',
    color: '#9DBEFF'
  },
  planned: {
    label: '已规划',
    color: '#BE86E4'
  },
  static: {
    label: '静态',
    color: '#69C0FF'
  },
  dynamic: {
    label: '动态',
    color: '#FFD666'
  },
  reserved: {
    label: '保留',
    color: '#FF9C6E'
  }
};

/** 叠加状态：格子上以圆点角标展示 */
const OVERLAY_STATUS = {
  zombie: {
    label: '僵尸',
    color: '#C41D7F'
  },
  conflict: {
    label: '冲突',
    color: '#FF6262'
  },
  online: {
    label: '在线',
    color: '#21BF86'
  }
};
const STATUS_DISABLED = ['network', 'broadcast']; // 不可操作的状态
const CELL_SIZE = 32; // 每格像素
const MAX_CELLS = 2048; // 图形化展示上限（超过提示改用更小网段）

/* ---------------- 地址地图组件 ---------------- */

function IpPlanMap(props) {
  const {
    cidr,
    ips,
    subnets,
    value,
    readOnly,
    onChange,
    onAction,
    onSave,
    onSubnetChange
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
  const currentSelected = 'value' in props ? value || [] : selState;
  const selectedSet = useMemo(() => new Set(currentSelected), [currentSelected]);
  const hostToIp = useMemo(() => {
    const m = new Map();
    ipList.forEach(p => m.set(p.host, p));
    return m;
  }, [ipList]);
  const [lastClickHost, setLastClickHost] = useState(null);
  const updateSelected = useCallback((next, lastIp) => {
    if (!('value' in props)) setSelState(next);
    if (onChange) onChange(next, {
      lastIp
    });
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
  const selectedIps = useMemo(() => currentSelected.map(ip => hostToIp.get(ip)).filter(Boolean), [currentSelected, hostToIp]);

  /* 批量改状态：转静态 / 转动态 / 转保留 */
  const applyStatus = useCallback(status => {
    if (readOnly || !selectedIps.length) return;
    const ids = new Set(selectedIps.map(p => p.ip));
    if (!ips) setInternalIps(prev => prev.map(p => ids.has(p.ip) ? {
      ...p,
      status
    } : p));
    const actionName = {
      static: 'toStatic',
      dynamic: 'toDynamic',
      reserved: 'toReserve'
    }[status];
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
      remark: first.remark || ''
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
      setInternalIps(prev => prev.map(p => ids.has(p.ip) ? {
        ...p,
        hostname: vals.hostname,
        status: vals.status,
        purpose: vals.purpose,
        user: vals.user,
        phone: vals.phone,
        leaseStart: vals.leaseStart,
        leaseEnd: vals.leaseEnd,
        customAttrs,
        remark: vals.remark
      } : p));
    }
    if (onSave) onSave(editing, {
      ...vals,
      customAttrs
    });
    message.success(`已保存 ${editing.length} 个 IP 的信息`);
    setEditOpen(false);
  }, [form, editing, ips, onSave]);

  /* ---- 详情抽屉 ---- */
  const [detail, setDetail] = useState(null);
  const openDetail = useCallback(() => {
    if (readOnly) return;
    if (selectedIps.length === 1) setDetail(selectedIps[0]);else message.info('请先只选中 1 个 IP 后再查看详情');
  }, [selectedIps, readOnly]);

  /* ---- 网段切换 ---- */
  const changeSubnet = useCallback(next => {
    if (onSubnetChange) {
      onSubnetChange(next);
      return;
    }
    setCidrState(next);
    if (!('value' in props)) setSelState([]);
  }, [onSubnetChange, props]);
  const prevSubnet = useCallback(() => {
    const idx = (subnets || []).findIndex(s => s.cidr === currentCidr);
    if (subnets && subnets.length && idx > 0) changeSubnet(subnets[idx - 1].cidr);else if (!subnets) {
      const p = parseCidr(currentCidr);
      const next = p.network - p.hostCount;
      if (next >= 0) changeSubnet(`${intToIp(next)}/${p.bits}`);
    }
  }, [subnets, currentCidr, changeSubnet]);
  const nextSubnet = useCallback(() => {
    const idx = (subnets || []).findIndex(s => s.cidr === currentCidr);
    if (subnets && subnets.length && idx >= 0 && idx < subnets.length - 1) changeSubnet(subnets[idx + 1].cidr);else if (!subnets) {
      const p = parseCidr(currentCidr);
      changeSubnet(`${intToIp(p.network + p.hostCount)}/${p.bits}`);
    }
  }, [subnets, currentCidr, changeSubnet]);
  const subnetOptions = useMemo(() => (subnets || []).map(s => ({
    label: s.name ? `${s.name}（${s.cidr}）` : s.cidr,
    value: s.cidr
  })), [subnets]);

  /* ---- 统计 ---- */
  const stats = useMemo(() => {
    const planned = ipList.filter(p => ['static', 'dynamic', 'reserved', 'planned'].includes(p.status)).length;
    const online = ipList.filter(p => (p.overlays || []).includes('online') || p.status === 'online').length;
    return {
      total: ipList.length,
      planned,
      online,
      selected: currentSelected.length
    };
  }, [ipList, currentSelected]);

  /* ---- 边界保护 ---- */
  const parsed = parseCidr(currentCidr);
  if (isNaN(parsed.hostCount) || parsed.hostCount < 1) {
    return /*#__PURE__*/React.createElement(Alert, {
      type: "error",
      showIcon: true,
      message: `网段格式错误：${currentCidr}`
    });
  }
  if (parsed.hostCount > MAX_CELLS) {
    return /*#__PURE__*/React.createElement(Alert, {
      type: "warning",
      showIcon: true,
      message: `网段 ${currentCidr} 共 ${parsed.hostCount} 个地址，超出图形化展示上限（${MAX_CELLS}），建议规划更小的网段。`
    });
  }
  const legendBase = Object.keys(IP_STATUS);
  const legendOverlay = Object.keys(OVERLAY_STATUS);
  const tooltipTitle = p => {
    const s = (IP_STATUS[p.status] || {}).label || p.status;
    const ovs = (p.overlays || []).map(o => (OVERLAY_STATUS[o] || {}).label || o).join('、');
    return /*#__PURE__*/React.createElement("div", null, /*#__PURE__*/React.createElement("div", {
      style: {
        fontWeight: 600
      }
    }, p.ip), /*#__PURE__*/React.createElement("div", null, "状态：", s, ovs ? `（叠加：${ovs}）` : ''), p.hostname ? /*#__PURE__*/React.createElement("div", null, "主机名：", p.hostname) : null, p.user ? /*#__PURE__*/React.createElement("div", null, "使用人：", p.user) : null);
  };
  const showNav = subnets && subnets.length || onSubnetChange;
  return /*#__PURE__*/React.createElement(Card, {
    ...rest,
    title: /*#__PURE__*/React.createElement(Space, {
      size: 8
    }, /*#__PURE__*/React.createElement("span", null, "地址规划"), /*#__PURE__*/React.createElement(Tag, {
      color: "blue"
    }, currentCidr), readOnly ? /*#__PURE__*/React.createElement(Tag, null, "只读") : null),
    extra: showNav ? /*#__PURE__*/React.createElement(Space, {
      size: 4
    }, /*#__PURE__*/React.createElement(Button, {
      size: "small",
      icon: /*#__PURE__*/React.createElement(LeftOutlined, null),
      onClick: prevSubnet,
      "aria-label": "上一个网段"
    }), /*#__PURE__*/React.createElement(Select, {
      size: "small",
      value: currentCidr,
      options: subnetOptions,
      onChange: changeSubnet,
      style: {
        width: 210
      },
      placeholder: "选择网段"
    }), /*#__PURE__*/React.createElement(Button, {
      size: "small",
      icon: /*#__PURE__*/React.createElement(RightOutlined, null),
      onClick: nextSubnet,
      "aria-label": "下一个网段"
    })) : null,
    size: "small"
  }, /*#__PURE__*/React.createElement(Space, {
    size: 28,
    wrap: true,
    style: {
      marginBottom: 12
    }
  }, /*#__PURE__*/React.createElement("span", null, /*#__PURE__*/React.createElement("b", {
    style: {
      color: '#BE86E4'
    }
  }, stats.planned), " 已规划IP数"), /*#__PURE__*/React.createElement("span", null, /*#__PURE__*/React.createElement("b", {
    style: {
      color: '#21BF86'
    }
  }, stats.online), " 在线IP数"), /*#__PURE__*/React.createElement("span", null, /*#__PURE__*/React.createElement("b", {
    style: {
      color: '#0065FF'
    }
  }, stats.selected), " 已选中"), /*#__PURE__*/React.createElement("span", {
    style: {
      color: '#A1A7C4'
    }
  }, "共 ", stats.total, " 个地址")), /*#__PURE__*/React.createElement(Space, {
    wrap: true,
    style: {
      marginBottom: 12
    }
  }, /*#__PURE__*/React.createElement(Button, {
    size: "small",
    type: "primary",
    icon: /*#__PURE__*/React.createElement(RetweetOutlined, null),
    disabled: readOnly || !stats.selected,
    onClick: () => applyStatus('static')
  }, "转静态"), /*#__PURE__*/React.createElement(Button, {
    size: "small",
    icon: /*#__PURE__*/React.createElement(RetweetOutlined, null),
    disabled: readOnly || !stats.selected,
    onClick: () => applyStatus('dynamic')
  }, "转动态"), /*#__PURE__*/React.createElement(Button, {
    size: "small",
    icon: /*#__PURE__*/React.createElement(LockOutlined, null),
    disabled: readOnly || !stats.selected,
    onClick: () => applyStatus('reserved')
  }, "转保留"), /*#__PURE__*/React.createElement(Button, {
    size: "small",
    icon: /*#__PURE__*/React.createElement(EditOutlined, null),
    disabled: readOnly || !stats.selected,
    onClick: openEdit
  }, "编辑"), /*#__PURE__*/React.createElement(Button, {
    size: "small",
    icon: /*#__PURE__*/React.createElement(InfoCircleOutlined, null),
    disabled: readOnly || stats.selected !== 1,
    onClick: openDetail
  }, "详情"), stats.selected > 0 && /*#__PURE__*/React.createElement(Button, {
    size: "small",
    danger: true,
    icon: /*#__PURE__*/React.createElement(ClearOutlined, null),
    onClick: () => updateSelected([], null)
  }, "清空选择")), /*#__PURE__*/React.createElement(Space, {
    wrap: true,
    size: [14, 6],
    style: {
      marginBottom: 12,
      fontSize: 12,
      color: '#A1A7C4'
    }
  }, legendBase.map(k => /*#__PURE__*/React.createElement("span", {
    key: k,
    style: {
      display: 'inline-flex',
      alignItems: 'center',
      gap: 5
    }
  }, /*#__PURE__*/React.createElement("span", {
    style: {
      width: 12,
      height: 12,
      borderRadius: 3,
      backgroundColor: IP_STATUS[k].color,
      display: 'inline-block',
      opacity: STATUS_DISABLED.includes(k) ? 0.75 : 1
    }
  }), IP_STATUS[k].label)), /*#__PURE__*/React.createElement(Divider, {
    type: "vertical",
    style: {
      margin: '0 2px'
    }
  }), /*#__PURE__*/React.createElement("span", null, "叠加状态："), legendOverlay.map(k => /*#__PURE__*/React.createElement("span", {
    key: k,
    style: {
      display: 'inline-flex',
      alignItems: 'center',
      gap: 5
    }
  }, /*#__PURE__*/React.createElement("span", {
    style: {
      width: 12,
      height: 12,
      borderRadius: '50%',
      backgroundColor: OVERLAY_STATUS[k].color,
      display: 'inline-block'
    }
  }), OVERLAY_STATUS[k].label))), /*#__PURE__*/React.createElement(Divider, {
    style: {
      margin: '0 0 12px'
    }
  }), /*#__PURE__*/React.createElement("div", {
    style: {
      display: 'flex',
      flexWrap: 'wrap',
      gap: 4,
      maxHeight: 420,
      overflow: 'auto',
      paddingBottom: 4
    }
  }, ipList.map(ipObj => {
    const meta = IP_STATUS[ipObj.status] || IP_STATUS.available;
    const disabled = readOnly || STATUS_DISABLED.includes(ipObj.status);
    const isSel = selectedSet.has(ipObj.ip);
    const overlays = ipObj.overlays || [];
    return /*#__PURE__*/React.createElement(Tooltip, {
      key: ipObj.ip,
      title: tooltipTitle(ipObj),
      mouseEnterDelay: 0.2
    }, /*#__PURE__*/React.createElement("div", {
      onClick: e => handleCellClick(ipObj, e),
      style: {
        width: CELL_SIZE,
        height: CELL_SIZE,
        fontSize: 11,
        borderRadius: 4,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: meta.color,
        color: '#fff',
        userSelect: 'none',
        position: 'relative',
        fontVariantNumeric: 'tabular-nums',
        lineHeight: 1,
        boxSizing: 'border-box',
        border: isSel ? '2px dashed #000' : '1px solid #e6e9f4',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.8 : 1
      }
    }, ipObj.host, overlays.slice(0, 2).map((ov, i) => {
      const om = OVERLAY_STATUS[ov];
      if (!om) return null;
      return /*#__PURE__*/React.createElement("span", {
        key: ov,
        style: {
          position: 'absolute',
          right: 2 + i * 9,
          bottom: 2,
          width: 7,
          height: 7,
          borderRadius: '50%',
          backgroundColor: om.color,
          border: '1px solid #fff'
        }
      });
    })));
  })), /*#__PURE__*/React.createElement("div", {
    style: {
      marginTop: 10,
      fontSize: 12,
      color: '#A1A7C4'
    }
  }, "点击选择 IP · Shift+点击 区间选择 · 网络/广播地址不可操作", readOnly ? ' · 当前为只读模式' : ''), /*#__PURE__*/React.createElement(Modal, {
    title: editing.length > 1 ? `编辑 ${editing.length} 个 IP` : `编辑 IP · ${(editing[0] || {}).ip || ''}`,
    open: editOpen,
    onOk: handleEditOk,
    onCancel: () => setEditOpen(false),
    okText: "保存",
    width: 540
  }, /*#__PURE__*/React.createElement(Form, {
    form: form,
    layout: "vertical"
  }, /*#__PURE__*/React.createElement(Form.Item, {
    label: "状态",
    name: "status"
  }, /*#__PURE__*/React.createElement(Select, {
    options: Object.entries(IP_STATUS).filter(([k]) => !STATUS_DISABLED.includes(k)).map(([k, v]) => ({
      label: v.label,
      value: k
    }))
  })), /*#__PURE__*/React.createElement(Row, {
    gutter: 12
  }, /*#__PURE__*/React.createElement(Col, {
    span: 12
  }, /*#__PURE__*/React.createElement(Form.Item, {
    label: "主机名",
    name: "hostname"
  }, /*#__PURE__*/React.createElement(Input, {
    placeholder: "如 PC-001"
  }))), /*#__PURE__*/React.createElement(Col, {
    span: 12
  }, /*#__PURE__*/React.createElement(Form.Item, {
    label: "用途",
    name: "purpose"
  }, /*#__PURE__*/React.createElement(Input, {
    placeholder: "办公 / 服务器 / 打印机…"
  }))), /*#__PURE__*/React.createElement(Col, {
    span: 12
  }, /*#__PURE__*/React.createElement(Form.Item, {
    label: "使用人",
    name: "user"
  }, /*#__PURE__*/React.createElement(Input, null))), /*#__PURE__*/React.createElement(Col, {
    span: 12
  }, /*#__PURE__*/React.createElement(Form.Item, {
    label: "电话",
    name: "phone"
  }, /*#__PURE__*/React.createElement(Input, null))), /*#__PURE__*/React.createElement(Col, {
    span: 12
  }, /*#__PURE__*/React.createElement(Form.Item, {
    label: "租约开始",
    name: "leaseStart"
  }, /*#__PURE__*/React.createElement(Input, {
    placeholder: "2026-08-01 09:00"
  }))), /*#__PURE__*/React.createElement(Col, {
    span: 12
  }, /*#__PURE__*/React.createElement(Form.Item, {
    label: "租约到期",
    name: "leaseEnd"
  }, /*#__PURE__*/React.createElement(Input, {
    placeholder: "2026-09-01 09:00"
  })))), /*#__PURE__*/React.createElement(Form.Item, {
    label: "自定义属性",
    name: "customAttrText",
    extra: "每行一条，格式：键=值"
  }, /*#__PURE__*/React.createElement(Input.TextArea, {
    rows: 2,
    placeholder: '资产编号=ZC-001\n位置=三楼机房'
  })), /*#__PURE__*/React.createElement(Form.Item, {
    label: "备注",
    name: "remark"
  }, /*#__PURE__*/React.createElement(Input.TextArea, {
    rows: 2
  })))), /*#__PURE__*/React.createElement(Drawer, {
    title: `IP 详情 · ${(detail || {}).ip || ''}`,
    width: 440,
    open: !!detail,
    onClose: () => setDetail(null)
  }, detail && /*#__PURE__*/React.createElement("div", null, /*#__PURE__*/React.createElement(Space, {
    wrap: true,
    style: {
      marginBottom: 12
    }
  }, /*#__PURE__*/React.createElement(Tag, {
    color: IP_STATUS[detail.status] ? IP_STATUS[detail.status].color : undefined
  }, (IP_STATUS[detail.status] || {}).label || detail.status), (detail.overlays || []).map(o => /*#__PURE__*/React.createElement(Tag, {
    key: o,
    color: OVERLAY_STATUS[o] ? OVERLAY_STATUS[o].color : undefined
  }, (OVERLAY_STATUS[o] || {}).label || o))), /*#__PURE__*/React.createElement(Divider, {
    orientation: "left",
    plain: true,
    style: {
      margin: '8px 0'
    }
  }, "分配信息"), /*#__PURE__*/React.createElement(Descriptions, {
    column: 1,
    size: "small",
    bordered: true
  }, /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "地址"
  }, detail.ip), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "主机名"
  }, detail.hostname || '-'), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "租约状态"
  }, detail.leaseStatus || '-'), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "租约时间"
  }, detail.leaseStart || '-'), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "到期时间"
  }, detail.leaseEnd || '-')), /*#__PURE__*/React.createElement(Divider, {
    orientation: "left",
    plain: true,
    style: {
      margin: '12px 0'
    }
  }, "扫描信息"), /*#__PURE__*/React.createElement(Descriptions, {
    column: 1,
    size: "small",
    bordered: true
  }, /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "设备名称"
  }, detail.deviceName || '-'), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "交换机端口"
  }, detail.switchPort || '-')), /*#__PURE__*/React.createElement(Divider, {
    orientation: "left",
    plain: true,
    style: {
      margin: '12px 0'
    }
  }, "资产信息"), /*#__PURE__*/React.createElement(Descriptions, {
    column: 1,
    size: "small",
    bordered: true
  }, Object.keys(detail.customAttrs || {}).length ? Object.entries(detail.customAttrs).map(([k, v]) => /*#__PURE__*/React.createElement(Descriptions.Item, {
    key: k,
    label: k
  }, String(v))) : /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "资产信息"
  }, "-")), /*#__PURE__*/React.createElement(Divider, {
    orientation: "left",
    plain: true,
    style: {
      margin: '12px 0'
    }
  }, "管理信息"), /*#__PURE__*/React.createElement(Descriptions, {
    column: 1,
    size: "small",
    bordered: true
  }, /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "用途"
  }, detail.purpose || '-'), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "使用人"
  }, detail.user || '-'), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "电话"
  }, detail.phone || '-'), /*#__PURE__*/React.createElement(Descriptions.Item, {
    label: "备注"
  }, detail.remark || '-')))));
}

/* =========================================================
 * 以下为演示应用（可替换为你的业务代码）
 * ========================================================= */
const SUBNETS = [{
  cidr: '10.59.193.0/24',
  name: '综合办公'
}, {
  cidr: '10.59.194.0/24',
  name: '数据中心'
}, {
  cidr: '10.59.195.0/24',
  name: '生产网段'
}];
function buildDemoIps(cidr) {
  const list = makeDefaultIps(cidr);
  const put = (host, patch) => {
    const p = list.find(x => x.host === host);
    if (p) Object.assign(p, patch);
  };
  for (let h = 11; h <= 20; h++) put(h, {
    status: 'planned'
  });
  for (let h = 21; h <= 30; h++) put(h, {
    status: 'static'
  });
  for (let h = 31; h <= 40; h++) put(h, {
    status: 'dynamic'
  });
  for (let h = 41; h <= 50; h++) put(h, {
    status: 'reserved'
  });
  put(60, {
    status: 'static',
    overlays: ['online'],
    hostname: 'PRINT-01',
    purpose: '共享打印机',
    user: '综合部',
    phone: '138-0000-0001',
    remark: 'HP MFP 打印扫描一体机'
  });
  put(61, {
    status: 'static',
    overlays: ['online']
  });
  put(70, {
    status: 'static',
    overlays: ['conflict'],
    hostname: 'SRV-FILE-01',
    purpose: '文件服务器',
    remark: '与 .71 存在 IP 冲突'
  });
  put(80, {
    status: 'static',
    overlays: ['zombie'],
    hostname: 'OLD-PC-88',
    remark: '僵尸地址，建议清理'
  });
  put(90, {
    status: 'dynamic',
    leaseStatus: '已分配',
    leaseStart: '2026-08-01 09:00',
    leaseEnd: '2026-09-01 09:00',
    user: '张三',
    purpose: '临时办公终端'
  });
  put(100, {
    status: 'static',
    customAttrs: {
      '资产编号': 'ZC-100',
      '位置': '三楼机房'
    },
    remark: '核心交换机上联地址'
  });
  put(200, {
    status: 'planned',
    purpose: '预留扩容'
  });
  return list;
}
function DemoApp() {
  const [cidr, setCidr] = useState('10.59.193.0/24');
  const [ips, setIps] = useState(() => buildDemoIps('10.59.193.0/24'));
  const [value, setValue] = useState([]);
  const handleSubnetChange = next => {
    setCidr(next);
    setIps(buildDemoIps(next));
    setValue([]);
  };
  const handleAction = (action, ipObjs) => {
    const names = {
      toStatic: '转静态',
      toDynamic: '转动态',
      toReserve: '转保留'
    };
    message.info(`执行操作：${names[action] || action}，目标 ${ipObjs.length} 个 IP（${ipObjs.slice(0, 3).map(o => o.ip).join(', ')}${ipObjs.length > 3 ? '…' : ''}）`);
    if (action === 'toStatic' || action === 'toDynamic' || action === 'toReserve') {
      const map = {
        toStatic: 'static',
        toDynamic: 'dynamic',
        toReserve: 'reserved'
      };
      const ids = new Set(ipObjs.map(o => o.ip));
      setIps(prev => prev.map(p => ids.has(p.ip) ? {
        ...p,
        status: map[action]
      } : p));
    }
  };
  const handleSave = (target, values) => {
    const ids = new Set(target.map(t => t.ip));
    setIps(prev => prev.map(p => ids.has(p.ip) ? {
      ...p,
      hostname: values.hostname,
      status: values.status,
      purpose: values.purpose,
      user: values.user,
      phone: values.phone,
      leaseStart: values.leaseStart,
      leaseEnd: values.leaseEnd,
      customAttrs: values.customAttrs,
      remark: values.remark
    } : p));
  };
  return /*#__PURE__*/React.createElement("div", {
    style: {
      maxWidth: 980,
      margin: '0 auto',
      padding: '24px 16px 48px'
    }
  }, /*#__PURE__*/React.createElement("h2", {
    style: {
      marginBottom: 4
    }
  }, "IP 规划 · 自研地址地图组件演示"), /*#__PURE__*/React.createElement("p", {
    style: {
      color: '#A1A7C4',
      margin: '0 0 16px',
      fontSize: 13
    }
  }, "React + Ant Design 5 实现的可复用 IP 规划核心组件：图形化地址地图、状态着色、叠加状态、 多选 / Shift 区间选择、批量操作（转静态 / 转动态 / 转保留 / 编辑 / 详情）、网段切换、图例。"), /*#__PURE__*/React.createElement(IpPlanMap, {
    cidr: cidr,
    ips: ips,
    subnets: SUBNETS,
    value: value,
    onChange: next => setValue(next),
    onAction: handleAction,
    onSave: handleSave,
    onSubnetChange: handleSubnetChange
  }), /*#__PURE__*/React.createElement(Card, {
    title: "如何复用（把 IpPlanMap.jsx 拷进你的项目）",
    size: "small",
    style: {
      marginTop: 16
    }
  }, /*#__PURE__*/React.createElement("pre", {
    className: "code"
  }, `// 1. 安装依赖
npm i react react-dom antd @ant-design/icons

// 2. 引入组件
import IpPlanMap from './IpPlanMap';

// 3. 使用
const [ips, setIps] = useState([...]);   // IP 对象数组，见 README 数据模型
<IpPlanMap
  cidr="10.59.193.0/24"     // 网段（也可通过 subnets 下拉切换）
  ips={ips}                 // IP 数据（不传则按网段自动生成）
  subnets={[{ cidr, name }]}
  value={selectedIps}       // 受控选中
  onChange={setSelectedIps}
  onAction={(action, ips) => console.log(action, ips)}
  onSave={(target, values) => console.log(target, values)}
  onSubnetChange={(cidr) => setCidr(cidr)}
  readOnly={false}
/>`)));
}
try {
  ReactDOM.createRoot(document.getElementById('root')).render(/*#__PURE__*/React.createElement(DemoApp, null));
} catch (e) {
  if (window.__showErr) window.__showErr(e && e.message || String(e));else throw e;
  console.error(e);
}