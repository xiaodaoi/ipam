/* =========================================================
 * 以下为演示应用（可替换为你的业务代码）
 * ========================================================= */
const SUBNETS = [
  { cidr: '10.59.193.0/24', name: '综合办公' },
  { cidr: '10.59.194.0/24', name: '数据中心' },
  { cidr: '10.59.195.0/24', name: '生产网段' },
];

function buildDemoIps(cidr) {
  const list = makeDefaultIps(cidr);
  const put = (host, patch) => { const p = list.find(x => x.host === host); if (p) Object.assign(p, patch); };
  for (let h = 11; h <= 20; h++) put(h, { status: 'planned' });
  for (let h = 21; h <= 30; h++) put(h, { status: 'static' });
  for (let h = 31; h <= 40; h++) put(h, { status: 'dynamic' });
  for (let h = 41; h <= 50; h++) put(h, { status: 'reserved' });
  put(60, { status: 'static', overlays: ['online'], hostname: 'PRINT-01', purpose: '共享打印机', user: '综合部', phone: '138-0000-0001', remark: 'HP MFP 打印扫描一体机' });
  put(61, { status: 'static', overlays: ['online'] });
  put(70, { status: 'static', overlays: ['conflict'], hostname: 'SRV-FILE-01', purpose: '文件服务器', remark: '与 .71 存在 IP 冲突' });
  put(80, { status: 'static', overlays: ['zombie'], hostname: 'OLD-PC-88', remark: '僵尸地址，建议清理' });
  put(90, { status: 'dynamic', leaseStatus: '已分配', leaseStart: '2026-08-01 09:00', leaseEnd: '2026-09-01 09:00', user: '张三', purpose: '临时办公终端' });
  put(100, { status: 'static', customAttrs: { '资产编号': 'ZC-100', '位置': '三楼机房' }, remark: '核心交换机上联地址' });
  put(200, { status: 'planned', purpose: '预留扩容' });
  return list;
}

function DemoApp() {
  const [cidr, setCidr] = useState('10.59.193.0/24');
  const [ips, setIps] = useState(() => buildDemoIps('10.59.193.0/24'));
  const [value, setValue] = useState([]);

  const handleSubnetChange = (next) => {
    setCidr(next);
    setIps(buildDemoIps(next));
    setValue([]);
  };

  const handleAction = (action, ipObjs) => {
    const names = { toStatic: '转静态', toDynamic: '转动态', toReserve: '转保留' };
    message.info(`执行操作：${names[action] || action}，目标 ${ipObjs.length} 个 IP（${ipObjs.slice(0, 3).map(o => o.ip).join(', ')}${ipObjs.length > 3 ? '…' : ''}）`);
    if (action === 'toStatic' || action === 'toDynamic' || action === 'toReserve') {
      const map = { toStatic: 'static', toDynamic: 'dynamic', toReserve: 'reserved' };
      const ids = new Set(ipObjs.map(o => o.ip));
      setIps(prev => prev.map(p => (ids.has(p.ip) ? { ...p, status: map[action] } : p)));
    }
  };

  const handleSave = (target, values) => {
    const ids = new Set(target.map(t => t.ip));
    setIps(prev => prev.map(p =>
      ids.has(p.ip) ? {
        ...p,
        hostname: values.hostname, status: values.status, purpose: values.purpose,
        user: values.user, phone: values.phone,
        leaseStart: values.leaseStart, leaseEnd: values.leaseEnd,
        customAttrs: values.customAttrs, remark: values.remark,
      } : p
    ));
  };

  return (
    <div style={{ maxWidth: 980, margin: '0 auto', padding: '24px 16px 48px' }}>
      <h2 style={{ marginBottom: 4 }}>IP 规划 · 自研地址地图组件演示</h2>
      <p style={{ color: '#A1A7C4', margin: '0 0 16px', fontSize: 13 }}>
        React + Ant Design 5 实现的可复用 IP 规划核心组件：图形化地址地图、状态着色、叠加状态、
        多选 / Shift 区间选择、批量操作（转静态 / 转动态 / 转保留 / 编辑 / 详情）、网段切换、图例。
      </p>

      <IpPlanMap
        cidr={cidr}
        ips={ips}
        subnets={SUBNETS}
        value={value}
        onChange={(next) => setValue(next)}
        onAction={handleAction}
        onSave={handleSave}
        onSubnetChange={handleSubnetChange}
      />

      <Card title="如何复用（把 IpPlanMap.jsx 拷进你的项目）" size="small" style={{ marginTop: 16 }}>
        <pre className="code">{`// 1. 安装依赖
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
/>`}</pre>
      </Card>
    </div>
  );
}

try {
  ReactDOM.createRoot(document.getElementById('root')).render(<DemoApp />);
} catch (e) {
  if (window.__showErr) window.__showErr((e && e.message) || String(e)); else throw e;
  console.error(e);
}
