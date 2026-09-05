import { StrictMode, Suspense, createContext, lazy, useContext, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { apiBase, assetSrc, generationStageLabel, generationStatusLabel, newIdempotencyKey, priceLabel, request, refreshSession, setAccessToken, statusLabel, type AdminList, type AuditLog, type AuthPayload, type GenerationJob, type GenerationJobList, type Product, type ProductList, type PromptPreview, type User } from './api';
import { AccountCenter, CheckoutPage, FavoritesPage, OrderDetailPage, OrdersPage, ProductActions, SandboxPage } from './accountPages';
import { ErrorBoundary } from './components/ErrorBoundary';
import { useQuery } from './hooks/useQuery';
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

function Home() { const [status, setStatus] = useState('检查中'); useEffect(() => { fetch(`${apiBase}/healthz`).then(r => setStatus(r.ok ? 'API 正常' : 'API 异常')).catch(() => setStatus('API 未连接')); }, []); return <main><p className="eyebrow">AIGC 3D PLATFORM</p><h1>谷子交易与 3D 展示平台</h1><p className="lead">账户、商品、收藏、个人中心、模拟订单、交易沙盒、网页 3D 预览和 AI 生成任务已接入。登录后可下单、模拟支付发货，并用虚拟资金做即时买卖。</p><div className="status">{status}</div></main>; }
function GenerationWorkspace() {
  const navigate = useNavigate();
  const [prompt, setPrompt] = useState('a collectible figure');
  const [productType, setProductType] = useState('手办');
  const [jobs, setJobs] = useState<GenerationJob[]>([]);
  const [activeId, setActiveId] = useState('');
  const [preview, setPreview] = useState<PromptPreview | null>(null);
  const [finalPrompt, setFinalPrompt] = useState('');
  const [error, setError] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const active = jobs.find(job => job.id === activeId) ?? jobs[0] ?? null;
  const mergeJob = (next: GenerationJob) => setJobs(current => {
    const exists = current.some(job => job.id === next.id);
    return exists ? current.map(job => job.id === next.id ? next : job) : [next, ...current];
  });
  const loadJob = (id: string) => request<{ job: GenerationJob }>(`/api/v1/generation-jobs/${id}`).then(body => mergeJob(body.job));
  const load = () => request<GenerationJobList>('/api/v1/generation-jobs?page=1&page_size=20').then(body => {
    setJobs(body.items);
    setActiveId(current => {
      const nextId = current && body.items.some(job => job.id === current) ? current : (body.items[0]?.id ?? '');
      if (nextId) void loadJob(nextId);
      return nextId;
    });
  }).catch(reason => setError(reason instanceof Error ? reason.message : '加载失败'));
  useEffect(() => { void load(); }, []);
  useEffect(() => {
    if (!active || ['SUCCEEDED', 'FAILED', 'CANCELED'].includes(active.status)) return;
    const timer = window.setInterval(() => {
      loadJob(active.id).catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [active?.id, active?.status]);
  const previewPrompt = async (event: FormEvent) => {
    event.preventDefault(); setError(''); setSubmitting(true);
    try {
      const body = await request<PromptPreview>('/api/v1/generation-jobs/prompt-preview', {
        method: 'POST', body: JSON.stringify({ prompt, product_type: productType }),
      });
      setPreview(body); setFinalPrompt(body.optimized_prompt);
    } catch (reason) { setError(reason instanceof Error ? reason.message : '优化失败'); } finally { setSubmitting(false); }
  };
  const create = async (event: FormEvent) => {
    event.preventDefault();
    if (!preview) return;
    setError(''); setSubmitting(true);
    try {
      const body = await request<{ job: GenerationJob }>('/api/v1/generation-jobs', {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: JSON.stringify({ prompt_preview_id: preview.id, final_prompt: finalPrompt, provider: 'hy3d', copyright_confirmed: true }),
      });
      mergeJob(body.job); setActiveId(body.job.id); setPreview(null); setFinalPrompt('');
    } catch (reason) { setError(reason instanceof Error ? reason.message : '创建失败'); } finally { setSubmitting(false); }
  };
  const act = async (path: 'cancel' | 'retry') => {
    if (!active) return; setError('');
    try {
      const body = await request<{ job: GenerationJob }>(`/api/v1/generation-jobs/${active.id}/${path}`, { method: 'POST' });
      mergeJob(body.job);
    } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败'); }
  };
  const model = active?.outputs.find(output => output.format === 'glb' && output.content_url);
  return (
    <main className="wide">
      <p className="eyebrow">WORKSPACE</p>
      <h1>AI 工作台</h1>
      <p className="lead">提交提示词后会先用术语库和 LLM 优化，再交给混元 HY-3D 生成静态 GLB。页面每 2 秒轮询状态。</p>
      <form className="auth-card publish-form" onSubmit={preview ? create : previewPrompt}>
        <label>提示词<textarea value={prompt} onChange={e => { setPrompt(e.target.value); setPreview(null); }} maxLength={2000} required /></label>
        <label>商品类型<input value={productType} onChange={e => { setProductType(e.target.value); setPreview(null); }} maxLength={64} required /></label>
        {preview && <>
          <p className="muted">优化来源：{preview.source} · 版本：{preview.template_version || '默认'} · 有效期至 {new Date(preview.expires_at).toLocaleString()}</p>
          <label>确认后的 Prompt<textarea value={finalPrompt} onChange={e => setFinalPrompt(e.target.value)} maxLength={1024} required /></label>
          <details><summary>查看结构化参数与 RAG 摘要</summary><pre>{JSON.stringify({ structured_prompt: preview.structured_prompt, rag_context: preview.rag_context }, null, 2)}</pre></details>
        </>}
        {error && <p className="error" role="alert">{error}</p>}
        <button disabled={submitting}>{submitting ? (preview ? '创建中…' : '优化中…') : (preview ? '确认并创建生成任务' : '预览优化 Prompt')}</button>
      </form>
      <section className="generation-layout">
        <div className="job-list">
          {jobs.length === 0 && <p className="muted">还没有生成任务。</p>}
          {jobs.map(job => (
            <button key={job.id} className={`job-item${job.id === active?.id ? ' is-active' : ''}`} onClick={() => { setActiveId(job.id); void loadJob(job.id); }}>
              <strong>{generationStatusLabel(job.status)}</strong>
              <span>{job.raw_prompt}</span>
              <em>{job.progress}%</em>
            </button>
          ))}
        </div>
        {active && (
          <div className="auth-card publish-form">
            <p className="muted">{generationStatusLabel(active.status)} · {generationStageLabel(active.stage)} · 第 {active.attempt}/{active.max_attempts} 次</p>
            <div className="viewer-progress" role="status"><span>{active.progress}%</span><i style={{ width: `${active.progress}%` }} /></div>
            {active.optimized_prompt && <p className="muted">优化后 Prompt：{active.optimized_prompt}</p>}
            {active.error && <p className="error" role="alert">{active.error.message}</p>}
            <div className="viewer-actions">
              {(active.status === 'QUEUED' || active.status === 'RUNNING') && <button className="secondary" onClick={() => void act('cancel')}>取消</button>}
              {active.status === 'FAILED' && active.attempt < active.max_attempts && <button onClick={() => void act('retry')}>重试</button>}
              {active.status === 'SUCCEEDED' && model && <button onClick={() => navigate(`/sell?job=${encodeURIComponent(active.id)}&prompt=${encodeURIComponent(active.raw_prompt)}&productType=${encodeURIComponent(active.product_type || productType)}`)}>带入发布</button>}
            </div>
            {model
              ? <Suspense fallback={<div className="viewer-progress" role="status"><span>正在准备 3D 预览</span></div>}><ModelViewer model={{ id: model.id, kind: 'MODEL', mime_type: model.mime_type, size_bytes: model.size_bytes, sha256: '', original_name: `${active.id}.glb`, content_url: model.content_url! }} compact /></Suspense>
              : <p className="muted">生成完成后将在这里预览 GLB。</p>}
          </div>
        )}
      </section>
    </main>
  );
}

function Market() {
  const [keyword, setKeyword] = useState('');
  const [query, setQuery] = useState('');
  const { data, error, loading } = useQuery<ProductList>(`/api/v1/products?page=1&page_size=20${query}`);
  return <main className="wide"><p className="eyebrow">MARKET</p><h1>商品市场</h1><form className="toolbar" onSubmit={event => { event.preventDefault(); setQuery(keyword.trim() ? `&keyword=${encodeURIComponent(keyword.trim())}` : ''); }}><input value={keyword} onChange={e => setKeyword(e.target.value)} placeholder="搜索商品标题或描述" /><button>搜索</button></form>{error && <p className="error" role="alert">{error}</p>}{loading && <p className="muted" role="status">正在加载商品…</p>}{!loading && data && data.items.length === 0 && <p className="muted">暂无已发布商品。</p>}<div className="card-grid">{data?.items.map(product => <Link className="product-card" to={`/products/${product.id}`} key={product.id}>{product.images[0] ? <img src={assetSrc(product.images[0])} alt={product.title} /> : <div className="placeholder-cover">暂无图片</div>}<strong>{product.title}</strong><span>{priceLabel(product.price_cents)}</span><em>{product.ip_name} · {product.category}{product.model ? ' · 含 3D' : ''}</em></Link>)}</div></main>;
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
      <ProductActions productId={product.id} title={product.title} />
    </main>
  );
}

function Publish() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const jobId = params.get('job')?.trim() ?? '';
  const [title, setTitle] = useState(() => (params.get('prompt') ?? '').slice(0, 120));
  const [description, setDescription] = useState(() => params.get('prompt') ?? '');
  const [price, setPrice] = useState('129.00');
  const [ipName, setIpName] = useState('原创');
  const [category, setCategory] = useState(() => params.get('productType') || '手办');
  const [condition, setCondition] = useState('全新');
  const [stock, setStock] = useState('1'); const [product, setProduct] = useState<Product | null>(null);
  const [error, setError] = useState(''); const [submitting, setSubmitting] = useState(false);
  const create = async (event: FormEvent) => {
    event.preventDefault(); setError(''); setSubmitting(true);
    try {
      const payload: Record<string, unknown> = { title, description, price_cents: Math.round(Number(price) * 100), ip_name: ipName, category, condition, stock: Number(stock), transaction_type: 'SALE' };
      if (jobId) payload.generation_job_id = jobId;
      const body = await request<{ product: Product }>('/api/v1/products', { method: 'POST', body: JSON.stringify(payload) });
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
  return <main className="wide"><p className="eyebrow">SELL</p><h1>发布商品</h1>{jobId && !product && <p className="muted">已从生成任务带入 GLB，创建草稿后仍需补至少 1 张图片才能发布。</p>}{!product ? <form className="auth-card publish-form" onSubmit={create}><label>标题<input value={title} onChange={e => setTitle(e.target.value)} minLength={2} maxLength={120} required /></label><label>描述<textarea value={description} onChange={e => setDescription(e.target.value)} required rows={5} /></label><label>价格（元）<input value={price} onChange={e => setPrice(e.target.value)} type="number" min="0.01" step="0.01" required /></label><label>IP / 系列<input value={ipName} onChange={e => setIpName(e.target.value)} required /></label><label>分类<input value={category} onChange={e => setCategory(e.target.value)} required /></label><label>成色<input value={condition} onChange={e => setCondition(e.target.value)} required /></label><label>库存<input value={stock} onChange={e => setStock(e.target.value)} type="number" min="0" required /></label>{error && <p className="error" role="alert">{error}</p>}<button disabled={submitting}>{submitting ? '创建中…' : jobId ? '创建草稿并绑定模型' : '创建草稿'}</button></form> : <section className="auth-card publish-form"><p className="muted">{product.model ? '草稿已绑定生成模型，请再上传 1–6 张图片后发布。也可替换 GLB。' : '草稿已创建，请上传 1–6 张图片，可选上传一个不超过 20 MB 的 GLB。上传后可直接预览 3D 模型。'}</p><label>商品图片<input type="file" accept="image/jpeg,image/png,image/webp,.jpg,.jpeg,.png,.webp" onChange={e => { const file = e.target.files?.[0]; if (file) void upload('images', file); e.target.value = ''; }} /></label><div className="gallery">{product.images.map(image => <img key={image.id} src={assetSrc(image)} alt={image.original_name} />)}</div><label>GLB 模型（可选）<input type="file" accept=".glb,model/gltf-binary" onChange={e => { const file = e.target.files?.[0]; if (file) void upload('model', file); e.target.value = ''; }} /></label>{product.model && <Suspense fallback={<div className="viewer-progress" role="status"><span>正在准备 3D 预览</span></div>}><ModelViewer model={product.model} fallbackImages={product.images} compact /></Suspense>}{error && <p className="error" role="alert">{error}</p>}<button disabled={submitting || product.images.length < 1} onClick={() => void publish()}>{submitting ? '发布中…' : '发布商品'}</button></section>}</main>;
}

function MyListings() {
  const { data, error, loading } = useQuery<ProductList>('/api/v1/products/mine');
  return <section><div className="toolbar"><h2>我的发布</h2><Link className="account-link" to="/sell">发布商品</Link></div>{error && <p className="error" role="alert">{error}</p>}{loading && <p className="muted" role="status">正在加载我的发布…</p>}{!loading && data && data.items.length === 0 && <p className="muted">还没有商品，先发布一个草稿吧。</p>}<div className="card-grid">{data?.items.map(product => <Link className="product-card" to={`/products/${product.id}`} key={product.id}>{product.images[0] ? <img src={assetSrc(product.images[0])} alt={product.title} /> : <div className="placeholder-cover">暂无图片</div>}<strong>{product.title}</strong><span>{statusLabel(product.status)} · {priceLabel(product.price_cents)}</span></Link>)}</div></section>;
}

function AdminPage() {
  const [users, setUsers] = useState<AdminList<User> | null>(null); const [products, setProducts] = useState<AdminList<Product> | null>(null); const [jobs, setJobs] = useState<AdminList<GenerationJob> | null>(null); const [logs, setLogs] = useState<AdminList<AuditLog> | null>(null); const [error, setError] = useState('');
  const load = async () => { try { const [u, p, j, l] = await Promise.all([request<AdminList<User>>('/api/v1/admin/users'), request<AdminList<Product>>('/api/v1/admin/products'), request<AdminList<GenerationJob>>('/api/v1/admin/generation-jobs'), request<AdminList<AuditLog>>('/api/v1/admin/audit-logs')]); setUsers(u); setProducts(p); setJobs(j); setLogs(l); } catch (reason) { setError(reason instanceof Error ? reason.message : '加载失败'); } };
  useEffect(() => { void load(); }, []);
  const action = async (path: string) => { try { await request(path, { method: 'POST' }); await load(); } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败'); } };
  return <main className="wide"><p className="eyebrow">ADMIN CONSOLE</p><h1>管理后台</h1><p className="lead">仅管理员可见。商品下架、失败任务重试均会记录操作者、前后状态和请求 ID。</p>{error && <p className="error" role="alert">{error}</p>}<section className="split-grid"><div className="auth-card"><h2>用户（{users?.total ?? '…'}）</h2>{users?.items.map(user => <p className="profile-row" key={user.id}><strong>{user.username}</strong><span>{user.roles.map(role => role.code).join(', ')}</span></p>)}</div><div className="auth-card"><h2>商品（{products?.total ?? '…'}）</h2>{products?.items.map(product => <p className="profile-row" key={product.id}><strong>{product.title}</strong><span>{statusLabel(product.status)} {product.status === 'PUBLISHED' && <button onClick={() => void action(`/api/v1/admin/products/${product.id}/off-shelf`)}>下架</button>}</span></p>)}</div></section><section className="auth-card"><h2>失败生成任务（{jobs?.total ?? '…'}）</h2>{jobs?.items.map(job => <p className="profile-row" key={job.id}><strong>{job.raw_prompt}</strong><span>{job.error?.message ?? '失败'} {job.attempt < job.max_attempts && <button onClick={() => void action(`/api/v1/admin/generation-jobs/${job.id}/retry`)}>重试</button>}</span></p>)}</section><section className="auth-card"><h2>审计日志（{logs?.total ?? '…'}）</h2>{logs?.items.map(log => <p className="profile-row" key={log.id}><strong>{log.action}</strong><span>{log.target_type}/{log.target_id} · {log.request_id}</span></p>)}</section></main>;
}
function AuthPage() {
  const [mode, setMode] = useState<'login' | 'register'>('login'); const [identifier, setIdentifier] = useState(''); const [username, setUsername] = useState(''); const [email, setEmail] = useState(''); const [password, setPassword] = useState(''); const [error, setError] = useState(''); const [submitting, setSubmitting] = useState(false);
  const auth = useAuth(); const navigate = useNavigate(); const location = useLocation();
  if (auth.user) return <Navigate to="/me" replace />;
  const submit = async (event: FormEvent) => { event.preventDefault(); setError(''); setSubmitting(true); try { if (mode === 'login') await auth.login(identifier, password); else await auth.register(username, email, password); navigate((location.state as { from?: string } | null)?.from ?? '/me', { replace: true }); } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败'); } finally { setSubmitting(false); } };
  return <main className="auth-shell"><section className="auth-card"><p className="eyebrow">ACCOUNT</p><h1>{mode === 'login' ? '欢迎回来' : '创建账户'}</h1><p className="muted">{mode === 'login' ? '使用用户名或邮箱登录' : '一个账户可同时购买、发布和创建模型'}</p><form onSubmit={submit}>{mode === 'login' ? <label>用户名或邮箱<input value={identifier} onChange={e => setIdentifier(e.target.value)} required autoComplete="username" /></label> : <><label>用户名<input value={username} onChange={e => setUsername(e.target.value)} minLength={3} maxLength={32} required autoComplete="username" /></label><label>邮箱（可选）<input value={email} onChange={e => setEmail(e.target.value)} type="email" autoComplete="email" /></label></>}<label>密码<input value={password} onChange={e => setPassword(e.target.value)} type="password" minLength={8} required autoComplete={mode === 'login' ? 'current-password' : 'new-password'} /></label>{error && <p className="error" role="alert">{error}</p>}<button disabled={submitting}>{submitting ? '处理中…' : mode === 'login' ? '登录' : '注册并登录'}</button></form><button className="text-button" onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(''); }}>{mode === 'login' ? '没有账户？立即注册' : '已有账户？返回登录'}</button></section></main>;
}
function Protected({ children }: { children: ReactNode }) { const auth = useAuth(); const location = useLocation(); if (auth.loading) return <main><p className="lead">正在恢复登录状态…</p></main>; return auth.user ? children : <Navigate to="/login" state={{ from: location.pathname }} replace />; }
function Profile() {
  const auth = useAuth();
  const navigate = useNavigate();
  const logout = async () => { await auth.logout(); navigate('/login', { replace: true }); };
  return <><AccountCenter onLogout={logout} /><div className="wide-wrap"><MyListings /></div></>;
}
function Navigation() { const auth = useAuth(); const isAdmin = auth.user?.roles.some(role => role.code === 'ADMIN'); return <nav><div className="nav-links"><Link to="/">首页</Link><Link to="/market">商品市场</Link><Link to="/sell">发布商品</Link><Link to="/favorites">收藏</Link><Link to="/orders">模拟订单</Link><Link to="/sandbox">交易沙盒</Link><Link to="/workspace/generation">AI 工作台</Link><Link to="/me">个人中心</Link>{isAdmin && <Link to="/admin">管理后台</Link>}</div><Link className="account-link" to={auth.user ? '/me' : '/login'}>{auth.user?.username ?? '登录 / 注册'}</Link></nav>; }
function Footer() { return <footer>AIGC 3D Platform</footer>; }
function App() { return <AuthProvider><Navigation/><Routes><Route path="/" element={<Home/>}/><Route path="/login" element={<AuthPage/>}/><Route path="/market" element={<Market/>}/><Route path="/sell" element={<Protected><Publish/></Protected>}/><Route path="/products/:id" element={<ProductDetail/>}/><Route path="/workspace/generation" element={<Protected><GenerationWorkspace/></Protected>}/><Route path="/favorites" element={<Protected><FavoritesPage/></Protected>}/><Route path="/checkout" element={<Protected><CheckoutPage/></Protected>}/><Route path="/orders" element={<Protected><OrdersPage/></Protected>}/><Route path="/orders/:id" element={<Protected><OrderDetailPage/></Protected>}/><Route path="/sandbox" element={<Protected><SandboxPage/></Protected>}/><Route path="/me" element={<Protected><Profile/></Protected>}/><Route path="/admin" element={<Protected><AdminPage/></Protected>}/></Routes><Footer/></AuthProvider>; }
createRoot(document.getElementById('root')!).render(<StrictMode><BrowserRouter><App/></BrowserRouter></StrictMode>);
