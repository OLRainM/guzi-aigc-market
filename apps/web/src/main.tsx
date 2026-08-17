import { StrictMode, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Link, Route, Routes } from 'react-router-dom';
import './styles.css';

const api = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080';
function Home() { const [status, setStatus] = useState('检查中'); useEffect(() => { fetch(`${api}/healthz`).then(r => r.ok ? setStatus('API 正常') : setStatus('API 异常')).catch(() => setStatus('API 未连接')); }, []); return <main><p className="eyebrow">AIGC 3D PLATFORM</p><h1>谷子交易与 3D 展示平台</h1><p className="lead">第一阶段基础设施已就绪，后续将接入商品、资产和 AI 工作台。</p><div className="status">{status}</div></main>; }
function Placeholder({ title }: { title: string }) { return <main><p className="eyebrow">WORKSPACE</p><h1>{title}</h1><p className="lead">该业务模块将在后续阶段实现。</p></main>; }
function App() { return <><nav><Link to="/">首页</Link><Link to="/market">商品市场</Link><Link to="/workspace/generation">AI 工作台</Link><Link to="/me">个人中心</Link></nav><Routes><Route path="/" element={<Home />} /><Route path="/login" element={<Placeholder title="登录" />} /><Route path="/market" element={<Placeholder title="商品市场" />} /><Route path="/products/:id" element={<Placeholder title="商品详情" />} /><Route path="/workspace/generation" element={<Placeholder title="AI 工作台" />} /><Route path="/me" element={<Placeholder title="个人中心" />} /></Routes></>; }
createRoot(document.getElementById('root')!).render(<StrictMode><BrowserRouter><App /></BrowserRouter></StrictMode>);
