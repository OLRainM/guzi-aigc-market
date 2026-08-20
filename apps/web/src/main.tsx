import { StrictMode, createContext, useContext, useEffect, useState, type FormEvent, type ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Link, Navigate, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import './styles.css';

const apiBase = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080';
type Role = { code: string; name: string };
type User = { id: string; username: string; email?: string; status: string; roles: Role[] };
type AuthPayload = { access_token: string; expires_in: number; user: User };
type AuthContextValue = { user: User | null; loading: boolean; login: (identifier: string, password: string) => Promise<void>; register: (username: string, email: string, password: string) => Promise<void>; logout: () => Promise<void> };

const AuthContext = createContext<AuthContextValue | null>(null);
let accessToken = '';
let refreshPromise: Promise<AuthPayload> | null = null;

function refreshSession() {
  if (!refreshPromise) {
    refreshPromise = request<AuthPayload>('/api/v1/auth/refresh', { method: 'POST' }, '').finally(() => { refreshPromise = null; });
  }
  return refreshPromise;
}

async function request<T>(path: string, init: RequestInit = {}, token = accessToken): Promise<T> {
  const response = await fetch(`${apiBase}${path}`, { ...init, credentials: 'include', headers: { 'Content-Type': 'application/json', ...init.headers, ...(token ? { Authorization: `Bearer ${token}` } : {}) } });
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new Error(body?.error?.message ?? '请求失败，请稍后重试');
  }
  return response.status === 204 ? undefined as T : response.json();
}

function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);
  useEffect(() => { refreshSession().then(payload => { accessToken = payload.access_token; setUser(payload.user); }).catch(() => setUser(null)).finally(() => setLoading(false)); }, []);
  const accept = (payload: AuthPayload) => { accessToken = payload.access_token; setUser(payload.user); };
  const login = async (identifier: string, password: string) => accept(await request<AuthPayload>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ identifier, password }) }, ''));
  const register = async (username: string, email: string, password: string) => accept(await request<AuthPayload>('/api/v1/auth/register', { method: 'POST', body: JSON.stringify({ username, email, password }) }, ''));
  const logout = async () => { try { await request('/api/v1/auth/logout', { method: 'POST' }, ''); } finally { accessToken = ''; setUser(null); } };
  return <AuthContext.Provider value={{ user, loading, login, register, logout }}>{children}</AuthContext.Provider>;
}
function useAuth() { const value = useContext(AuthContext); if (!value) throw new Error('AuthProvider missing'); return value; }

function Home() { const [status, setStatus] = useState('检查中'); useEffect(() => { fetch(`${apiBase}/healthz`).then(r => setStatus(r.ok ? 'API 正常' : 'API 异常')).catch(() => setStatus('API 未连接')); }, []); return <main><p className="eyebrow">AIGC 3D PLATFORM</p><h1>谷子交易与 3D 展示平台</h1><p className="lead">账户与权限基础已接入，登录后即可进入个人中心。</p><div className="status">{status}</div></main>; }
function Placeholder({ title }: { title: string }) { return <main><p className="eyebrow">WORKSPACE</p><h1>{title}</h1><p className="lead">该业务模块将在后续阶段实现。</p></main>; }

function AuthPage() {
  const [mode, setMode] = useState<'login' | 'register'>('login'); const [identifier, setIdentifier] = useState(''); const [username, setUsername] = useState(''); const [email, setEmail] = useState(''); const [password, setPassword] = useState(''); const [error, setError] = useState(''); const [submitting, setSubmitting] = useState(false);
  const auth = useAuth(); const navigate = useNavigate(); const location = useLocation();
  if (auth.user) return <Navigate to="/me" replace />;
  const submit = async (event: FormEvent) => { event.preventDefault(); setError(''); setSubmitting(true); try { if (mode === 'login') await auth.login(identifier, password); else await auth.register(username, email, password); navigate((location.state as { from?: string } | null)?.from ?? '/me', { replace: true }); } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败'); } finally { setSubmitting(false); } };
  return <main className="auth-shell"><section className="auth-card"><p className="eyebrow">ACCOUNT</p><h1>{mode === 'login' ? '欢迎回来' : '创建账户'}</h1><p className="muted">{mode === 'login' ? '使用用户名或邮箱登录' : '一个账户可同时购买、发布和创建模型'}</p><form onSubmit={submit}>{mode === 'login' ? <label>用户名或邮箱<input value={identifier} onChange={e => setIdentifier(e.target.value)} required autoComplete="username" /></label> : <><label>用户名<input value={username} onChange={e => setUsername(e.target.value)} minLength={3} maxLength={32} required autoComplete="username" /></label><label>邮箱（可选）<input value={email} onChange={e => setEmail(e.target.value)} type="email" autoComplete="email" /></label></>}<label>密码<input value={password} onChange={e => setPassword(e.target.value)} type="password" minLength={8} required autoComplete={mode === 'login' ? 'current-password' : 'new-password'} /></label>{error && <p className="error" role="alert">{error}</p>}<button disabled={submitting}>{submitting ? '处理中…' : mode === 'login' ? '登录' : '注册并登录'}</button></form><button className="text-button" onClick={() => { setMode(mode === 'login' ? 'register' : 'login'); setError(''); }}>{mode === 'login' ? '没有账户？立即注册' : '已有账户？返回登录'}</button></section></main>;
}
function Protected({ children }: { children: ReactNode }) { const auth = useAuth(); const location = useLocation(); if (auth.loading) return <main><p className="lead">正在恢复登录状态…</p></main>; return auth.user ? children : <Navigate to="/login" state={{ from: location.pathname }} replace />; }
function Profile() { const auth = useAuth(); const roles = auth.user?.roles.map(role => role.name).join('、'); return <main><p className="eyebrow">PROFILE</p><h1>{auth.user?.username}</h1><p className="lead">{auth.user?.email || '未填写邮箱'}</p><div className="profile-row"><span>账户状态</span><strong>{auth.user?.status}</strong></div><div className="profile-row"><span>角色</span><strong>{roles}</strong></div><button className="secondary" onClick={() => void auth.logout()}>退出登录</button></main>; }
function Navigation() { const auth = useAuth(); return <nav><div className="nav-links"><Link to="/">首页</Link><Link to="/market">商品市场</Link><Link to="/workspace/generation">AI 工作台</Link><Link to="/me">个人中心</Link></div><Link className="account-link" to={auth.user ? '/me' : '/login'}>{auth.user?.username ?? '登录 / 注册'}</Link></nav>; }
function Footer() { return <footer><a href="https://beian.miit.gov.cn/" target="_blank" rel="noreferrer">粤ICP备2026057221号</a></footer>; }
function App() { return <AuthProvider><Navigation/><Routes><Route path="/" element={<Home/>}/><Route path="/login" element={<AuthPage/>}/><Route path="/market" element={<Placeholder title="商品市场"/>}/><Route path="/products/:id" element={<Placeholder title="商品详情"/>}/><Route path="/workspace/generation" element={<Protected><Placeholder title="AI 工作台"/></Protected>}/><Route path="/me" element={<Protected><Profile/></Protected>}/></Routes><Footer/></AuthProvider>; }
createRoot(document.getElementById('root')!).render(<StrictMode><BrowserRouter><App/></BrowserRouter></StrictMode>);
