/**
 * build.js —— 把演示所需的运行时依赖下载到本地 lib/，
 * 并把 demo 源码（IpPlanMap.jsx 的共享主体 + demo-app-part.jsx）预编译为普通 JS（demo.app.js）。
 *
 * 这样 index.html 双击即可离线打开，无需联网访问 unpkg。
 * 本目录下的 lib/ 和 demo.app.js 已构建好，一般无需重复执行。
 *
 * 用法：node build.js    （本机需能联网一次；之后产物均为本地文件）
 */
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const libDir = path.join(__dirname, 'lib');
fs.mkdirSync(libDir, { recursive: true });

// 运行时依赖（保持 script 加载顺序：React → ReactDOM → dayjs → icons → antd）
const deps = {
  'react.production.min.js':    'https://unpkg.com/react@18/umd/react.production.min.js',
  'react-dom.production.min.js': 'https://unpkg.com/react-dom@18/umd/react-dom.production.min.js',
  'dayjs.min.js':               'https://unpkg.com/dayjs@1/dayjs.min.js',
  'icons.umd.js':               'https://unpkg.com/@ant-design/icons@5/dist/index.umd.js',
  'antd.min.js':                'https://unpkg.com/antd@5/dist/antd.min.js',
  'reset.css':                  'https://unpkg.com/antd@5/dist/reset.css',
};

async function main() {
  // 1) 下载依赖
  for (const [file, url] of Object.entries(deps)) {
    const r = await fetch(url);
    if (!r.ok) throw new Error(`下载失败 ${url} -> HTTP ${r.status}`);
    const buf = Buffer.from(await r.arrayBuffer());
    fs.writeFileSync(path.join(libDir, file), buf);
    console.log(`下载 ${file}  (${(buf.length / 1024).toFixed(1)} KB)`);
  }

  // 2) 组装 demo 源码：IpPlanMap.jsx 的共享主体 + demo-app-part.jsx
  const ipPlanMap = fs.readFileSync(path.join(__dirname, 'IpPlanMap.jsx'), 'utf8');
  const start = ipPlanMap.indexOf('/* =========================================================');
  const end = ipPlanMap.indexOf('export default IpPlanMap;');
  if (start < 0 || end < 0) throw new Error('IpPlanMap.jsx 结构异常：找不到共享主体边界');
  const sharedBody = ipPlanMap.slice(start, end).replace(/\n\s*\n\s*$/, '\n');
  const demoPart = fs.readFileSync(path.join(__dirname, 'demo-app-part.jsx'), 'utf8');
  // 浏览器环境没有 antdIcons 导入名，这里映射到 icons.umd.js 的全局 window.icons
  const demoJsx = 'const antdIcons = window.icons;\n' + sharedBody + '\n' + demoPart;
  fs.writeFileSync(path.join(__dirname, 'demo.jsx'), demoJsx);
  console.log(`组装 demo.jsx  (${(demoJsx.length / 1024).toFixed(1)} KB)`);

  // 3) 预编译 demo.jsx -> demo.app.js
  const babelSrc = await (await fetch('https://unpkg.com/@babel/standalone/babel.min.js')).text();
  const sandbox = { window: {} };
  sandbox.window.globalThis = sandbox.window;
  vm.createContext(sandbox);
  vm.runInContext(babelSrc, sandbox);
  const Babel = sandbox.Babel || sandbox.window.Babel;

  const out = Babel.transform(demoJsx, { presets: [['react', { runtime: 'classic' }]], filename: 'demo.jsx' });
  fs.writeFileSync(path.join(__dirname, 'demo.app.js'), out.code);
  console.log(`预编译 demo.app.js  (${(out.code.length / 1024).toFixed(1)} KB)`);

  console.log('\n完成。直接双击 index.html 即可（无需联网）。');
}

main().catch(e => { console.error('构建失败：', e.message); process.exit(1); });
