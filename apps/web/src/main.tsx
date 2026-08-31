import { StrictMode, Suspense, createContext, lazy, useContext, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useNavigate, useParams } from 'react-router-dom';
import { apiBase, assetSrc, priceLabel, request, refreshSession, setAccessToken, statusLabel, type AuthPayload, type Product, type ProductList, type User } from './api';
import './styles.css';

const ModelViewer = lazy(() => import('./ModelViewer').then(module => ({ default: module.ModelViewer })));

type AuthContextValue = { user: User | null; loading: boolean; login: (identifier: string, password: string) => Promise<void>; register: (username: string, email: string, password: string) => Promise<void>; logout: () => Promise<void> };
const AuthContext = createContext<AuthContextValue | null>(null);

function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  useEffect(() => { refreshSession().then(payload => { setAccessToken(payload.access_token); setUser(payload.user); }).catch(() => setUser(null)).finally(() => setLoading(false)); }, []);
  const accept = (payload: AuthPayload) => { setAccessToken(payload.access_token); setUser(payload.user); };
  const login = async (identifier: string, password: string) => accept(await request<AuthPayload>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ identifier, password }) }, ''));
  const register = async (username: string, email: string, password: string) => accept(await request<AuthPayload>('/api/v1/auth/register', { method: 'POST', body: JSON.stringify({ username, email, password }) }, ''));
  const logout = async () => { try { await request('/api/v1/auth/logout', { method: 'POST' }, ''); } finally { setAccessToken(''); setUser(null); } };
  return <AuthContext.Provider value={{ user, loading, login, register, logout }}>{children}</AuthContext.Provider>;
}
function useAuth() { const value = useContext(AuthContext); if (!value) throw new Error('AuthProvider missing'); return value; }

function Home() { const [status, setStatus] = useState('检查中'); useEffect(() => { fetch(`${apiBase}/healthz`).then(r => setStatus(r.ok ? 'API 正常' : 'API 异常')).catch(() => setStatus('API 未连接')); }, []); return <main><p className="eyebrow">AIGC 3D PLATFORM</p><h1>谷子交易与 3D 展示平台</h1><p className="lead">账户、商品、图片/GLB 上传和网页 3D 预览已接入。登录后可发布商品并在详情页旋转查看模型。</p><div className="status">{status}</div></main>; }
function Placeholder({ title }: { title: string }) { return <main><p className="eyebrow">WORKSPACE</p><h1>{title}</h1><p className="lead">该业务模块将在后续阶段实现。</p></main>; }

function Market() {
  const [data, setData] = useState<ProductList | null>(null);
  const [keyword, setKeyword] = useState('');
  const [error, setError] = useState('');
  const load = (query = '') => { request<ProductList>(`/api/v1/products?page=1&page_size=20${query}`).then(setData).catch(reason => setError(reason instanceof Error ? reason.message : '加载失败')); };
  useEffect(() => { load(); }, []);
  return <main className="wide"><p className="eyebrow">MARKET</p><h1>商品市场</h1><form className="toolbar" onSubmit={event => { event.preventDefault(); load(keyword.trim() ? `&keyword=${encodeURIComponent(keyword.trim())}` : ''); }}><input value={keyword} onChange={e => setKeyword(e.target.value)} placeholder="搜索商品标题或描述" /><button>搜索</button></form>{error && <p className="error" role="alert">{error}</p>}{data && data.items.length === 0 && <p className="muted">暂无已发布商品。</p>}<div className="card-grid">{data?.items.map(product => <Link className="product-card" to={`/products/${product.id}`} key={product.id}>{product.images[0] ? <img src={assetSrc(product.images[0])} alt={product.title} /> : <div className="placeholder-cover">暂无图片</div>}<strong>{product.title}</strong><span>{priceLabel(product.price_cents)}</span><em>{product.ip_name} · {product.category}{product.model ? ' · 含 3D' : ''}</em></Link>)}</div></main>;
}

function ProductDetail() {
  const { id } = useParams();
  const [product, setProduct] = useState<Product | null>(null);
  const [error, setError] = useState('');
  useEffect(() => { if (!id) return; request<{ product: Product }>(`/api/v1/products/${id}`).then(body => setProduct(body.product)).catch(reason => setError(reason instanceof Error ? reason.message : '加载失败')); }, [id]);
  if (error) return <main><p className="error" role="alert">{error}</p></main>;
  if (!product) return <main><p className="lead">正在加载商品…</p></main>;
  return (
    <main className="wide">
      <p className="eyebrow">{product.ip_name}</p>
      <h1>{product.title}</h1>
      <p className="lead">{priceLabel(product.price_cents)} · {product.condition} · {statusLabel(product.status)}</p>
      {product.model
        ? <Suspense fallback={<div className="viewer-progress" role="status"><span>正在准备 3D 预览</span></div>}><ModelViewer model={product.model} fallbackImages={product.images} /></Suspense>
        : <div className="gallery">{product.images.map(image => <img key={image.id} src={assetSrc(image)} alt={image.original_name} />)}{product.images.length === 0 && <div className="placeholder-cover">暂无图片</div>}</div>}
      {product.model && product.images.length > 0 && (
        <div className="gallery">{product.images.map(image => <img key={image.id} src={assetSrc(image)} alt={image.original_name} />)}</div>
      )}
      <p className="lead">{product.description}</p>
    </main>
  );
}

function Publish() {
  const navigate = useNavigate();
  const [title, setTitle] = useState(''); const [description, setDescription] = useState(''); const [price, setPrice] = useState('129.00');
  const [ipName, setIpName] = useState(''); const [category, setCategory] = useState('手办'); const [condition, setCondition] = useState('全新');
  const [stock, setStock] = useState('1'); const [product, setProduct] = useState<Product | null>(null);
  const [error, setError] = useState(''); const [submitting, setSubmitting] = useState(false);
  const create = async (event: FormEvent) => {
    event.preventDefault(); setError(''); setSubmitting(true);
    try {
      const body = await request<{ product: Product }>('/api/v1/products', { method: 'POST', body: JSON.stringify({ title, description, price_cents: Math.round(Number(price) * 100), ip_name: ipName, category, condition, stock: Number(stock), transaction_type: 'SALE' }) });
      setProduct(body.product);
    } catch (reason) { setError(reason instanceof Error ? reason.message : '创建失败'); } finally { setSubmitting(false); }
  };
  const upload = async (kind: 'images' | 'model', file: File) => {
    if (!product) return; setError('');
    if (kind === 'model' && file.size > 20 * 1024 * 1024) {
      setError('GLB 不能超过 20 MB');
      return;
    }
    const data = new FormData(); data.append('file', file);
    try {
      const body = await request<{ product: Product }>(`/api/v1/products/${product.id}/${kind}`, { method: 'POST', body: data });
      setProduct(body.product);
    } catch (reason) { setError(reason instanceof Error ? reason.message : '上传失败'); }
  };
  const publish = async () => {
    if (!product) return; setError(''); setSubmitting(true);
    try { await request(`/api/v1/products/${product.id}/publish`, { method: 'POST' }); navigate(`/products/${product.id}`); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '发布失败'); } finally { setSubmitting(false); }
  };
  return <main className="wide"><p className="eyebrow">SELL</p><h1>发布商品</h1>{!product ? <form className="auth-card publish-form" onSubmit={create}><label>标题<input value={title} onChange={e => setTitle(e.target.value)} minLength={2} maxLength={120} required /></label><label>描述<textarea value={description} onChange={e => setDescription(e.target.value)} required rows={5} /></label><label>价格（元）<input value={price} onChange={e => setPrice(e.target.value)} type="number" min="0.01" step="0.01" required /></label><label>IP / 系列<input value={ipName} onChange={e => setIpName(e.target.value)} required /></label><label>分类<input value={category} onChange={e => setCategory(e.target.value)} required /></label><label>成色<input value={condition} onChange={e => setCondition(e.target.value)} required /></label><label>库存<input value={stock} onChange={e => setStock(e.target.value)} type="number" min="0" required /></label>{error && <p className="error" role="alert">{error}</p>}<button disabled={submitting}>{submitting ? '创建中…' : '创建草稿'}</button></form> : <section className="auth-card publish-form"><p className="muted">草稿已创建，请上传 1–6 张图片，可选上传一个不超过 20 MB 的 GLB。上传后可直接预览 3D 模型。</p><label>商品图片<input type="file" accept="image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp" onChange={e => { const file = e.target.files?.[0]; if (file) void upload('images', file); e.target.value = ''; }} /></label><div className="gallery">{product.images.map(image => <img key={image.id} src={assetSrc(image)} alt={image.original_name} />)}</div><label>GLB 模型（可选）<input type="file" accept=".glb,model/gltf-binary" onChange={e => { const file = e.target.files?.[0]; if (file) void upload('model', file); e.target.value = ''; }} /></label>{product.model && <Suspense fallback={<div className="viewer-progress" role="status"><span>正在准备 3D 预览</span></div>}><ModelViewer model={product.model} fallbackImages={product.images} compact /></Suspense>}{error && <p className="error" role="alert">{error}</p>}<button disabled={submitting || product.images.length < 1} onClick={() => void publish()}>{submitting ? '发布中…' : '发布商品'}</button></section>}</main>;
}

function MyListings() {
  const [data, setData] = useState<ProductList | null>(null);
  const [error, setError] = useState('');
  useEffect(() => { request<ProductList>('/api/v1/products/mine').then(setData).catch(reason => setError(reason instanceof Error ? reason.message : '加载失败')); }, []);
  return <section><div className="toolbar"><h2>我的发布</h2><Link className="account-link" to="/sell">发布商品</Link></div>{error && <p className="error" role="alert">{error}</p>}{data && data.items.length === 0 && <p className="muted">还没有商品，先发布一个草稿吧。</p>}<div className="card-grid">{data?.items.map(product => <Link className="product-card" to={`/products/${product.id}`} key={product.id}>{product.images[0] ? <img src={assetSrc(product.images[0])} alt={product.title} /> : <div className="placeholder-cover">暂无图片</div>}<strong>{product.title}</strong><span>{statusLabel(product.status)} · {priceLabel(product.price_cents)}</span></Link>)}</div></section>;
}

function AuthPage() {
  const [mode, setMode] = useState<'login' | 'register'>('login'); const [identifier, setIdentifier] = useState(''); const [username, setUsername] = useState(''); const [email, setEmail] = useState(''); const [password, setPassword] = useState(''); const [error, setError] = useState(''); const [submitting, setSubmitting] = useState(false);
  const auth = useAuth(); const navigate = useNavigate(); const location = useLocation();
  if (auth.user) return <Navigate to="/me" replace />;
  const submit = async (event: FormEvent) => { event.preventDefault(); setError(''); setSubmitting(true); try { if (mode === 'login') await auth.login(identifier, password); else await auth.register(username, email, password); navigate((location.state as { from?: string } | null)?.from ?? '/me', { replace: true }); } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败'); } finally { setSubmitting(false); } };
  return <main className="auth-shell"><section className="auth-card"><p className="eyebrow">ACCOUNT</p><h1>{mode === 'login' ? '欢迎回来' : '创建账户'}</h1><p className="muted">{mode === 'login' ? '使用用户名或邮箱登录' : '一个账户可同时购买、发布和创建模型'}</p><form onSubmit={submit}>{mode === 'login' ? <label>用户名或邮箱<input value={identifier} onChange={e => setIdentifier(e.target.value)} required autoComplete="username" /></label> : <><label>用户名<input value={username} onChange={e => setUsername(e.target.value)} minLength={3} maxLength={32} required autoComplete="username" /></label><label>邮箱（可选）<input value={email} onChange={e => setEmail(e.target.value)} type="email" autoComplete="email" /></label></>}<label>密码<input value={password} onChange={e => setPassword(e.target.value)} type="password" minLength={8} required autoComplete={mode === 'login' ? 'current-password' : 'new-password'} /></label>{error && <p className="error" role="alert">{error}</p>}<button disabled={submitting}>{submitting ? '处理中…' : mode === 'login' ? '登录' : '注册并登录'}</button></form><button className="text-button" onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(''); }}>{mode === 'login' ? '没有账户？立即注册' : '已有账户？返回登录'}</button></section></main>;
}
function Protected({ children }: { children: ReactNode }) { const auth = useAuth(); const location = useLocation(); if (auth.loading) return <main><p className="lead">正在恢复登录状态…</p></main>; return auth.user ? children : <Navigate to="/login" state={{ from: location.pathname }} replace />; }
function Profile() { const auth = useAuth(); const roles = auth.user?.roles.map(role => role.name).join('、'); return <main className="wide"><p className="eyebrow">PROFILE</p><h1>{auth.user?.username}</h1><p className="lead">{auth.user?.email || '未填写邮箱'}</p><div className="profile-row"><span>账户状态</span><strong>{auth.user?.status}</strong></div><div className="profile-row"><span>角色</span><strong>{roles}</strong></div><MyListings /><button className="secondary" onClick={() => void auth.logout()}>退出登录</button></main>; }
function Navigation() { const auth = useAuth(); return <nav><div className="nav-links"><Link to="/">首页</Link><Link to="/market">商品市场</Link><Link to="/sell">发布商品</Link><Link to="/workspace/generation">AI 工作台</Link><Link to="/me">个人中心</Link></div><Link className="account-link" to={auth.user ? '/me' : '/login'}>{auth.user?.username ?? '登录 / 注册'}</Link></nav>; }
function Footer() { return <footer>AIGC 3D Platform</footer>; }
function App() { return <AuthProvider><Navigation/><Routes><Route path="/" element={<Home/>}/><Route path="/login" element={<AuthPage/>}/><Route path="/market" element={<Market/>}/><Route path="/sell" element={<Protected><Publish/></Protected>}/><Route path="/products/:id" element={<ProductDetail/>}/><Route path="/workspace/generation" element={<Protected><Placeholder title="AI 工作台"/></Protected>}/><Route path="/me" element={<Protected><Profile/></Protected>}/></Routes><Footer/></AuthProvider>; }
createRoot(document.getElementById('root')!).render(<StrictMode><BrowserRouter><App/></BrowserRouter></StrictMode>);
