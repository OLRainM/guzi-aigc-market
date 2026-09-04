import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Link, useLocation, useNavigate, useSearchParams } from 'react-router-dom';
import {
  activityLabel, favoriteStatusLabel, priceLabel, request,
  type ActivityItem, type Address, type Favorite, type FavoriteFolder, type FavoriteList,
  type NotificationItem, type Preferences, type ProfilePayload, type SandboxSnapshot,
} from './api';

function formatTime(value?: string | null) {
  return value ? new Date(value).toLocaleString('zh-CN') : '';
}

function coverSrc(url?: string) {
  return url || '';
}

export function FavoritesPage() {
  const [items, setItems] = useState<Favorite[]>([]);
  const [folders, setFolders] = useState<FavoriteFolder[]>([]);
  const [folder, setFolder] = useState('');
  const [status, setStatus] = useState('');
  const [keyword, setKeyword] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const load = () => {
    const query = new URLSearchParams();
    if (folder) query.set('folder', folder);
    if (status) query.set('status', status);
    if (keyword.trim()) query.set('keyword', keyword.trim());
    request<FavoriteList>(`/api/v1/favorites?${query.toString()}`).then(body => { setItems(body.items); setSelected([]); }).catch(reason => setError(reason instanceof Error ? reason.message : '加载失败'));
    request<{ items: FavoriteFolder[] }>('/api/v1/favorites/folders').then(body => setFolders(body.items)).catch(() => undefined);
  };
  useEffect(() => { load(); }, [folder, status]);
  const toggle = (id: string) => setSelected(current => current.includes(id) ? current.filter(item => item !== id) : [...current, id]);
  const removeOne = async (id: string) => {
    setError('');
    try { await request(`/api/v1/favorites/${id}`, { method: 'DELETE' }); setMessage('已取消收藏'); load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '取消失败'); }
  };
  const batchRemove = async () => {
    if (selected.length === 0) return;
    setError('');
    try { await request('/api/v1/favorites/batch-delete', { method: 'POST', body: JSON.stringify({ ids: selected }) }); setMessage(`已取消 ${selected.length} 条收藏`); load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '批量取消失败'); }
  };
  const ack = async (id: string) => {
    try { await request(`/api/v1/favorites/${id}/ack`, { method: 'POST' }); setMessage('已确认最新状态'); load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '确认失败'); }
  };
  const move = async (id: string, nextFolder: string) => {
    const value = nextFolder.trim();
    if (!value) return;
    try { await request(`/api/v1/favorites/${id}`, { method: 'PATCH', body: JSON.stringify({ folder: value }) }); setMessage('已移动分类'); load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '移动失败'); }
  };
  return (
    <main className="wide">
      <p className="eyebrow">FAVORITES</p>
      <h1>我的收藏</h1>
      <p className="lead">按分类筛选、批量管理，并查看商品更新或失效提示。</p>
      <form className="toolbar" onSubmit={event => { event.preventDefault(); load(); }}>
        <input value={keyword} onChange={e => setKeyword(e.target.value)} placeholder="搜索收藏标题 / IP / 分类" />
        <button>搜索</button>
        <button type="button" className="ghost" disabled={selected.length === 0} onClick={() => void batchRemove()}>批量取消（{selected.length}）</button>
      </form>
      <div className="chip-row">
        <button className={`chip ${folder === '' ? 'is-active' : ''}`} onClick={() => setFolder('')}>全部</button>
        {folders.map(item => <button key={item.folder} className={`chip ${folder === item.folder ? 'is-active' : ''}`} onClick={() => setFolder(item.folder)}>{item.folder} · {item.count}</button>)}
      </div>
      <div className="chip-row">
        {[['','全部状态'],['ACTIVE','有效'],['UPDATED','已更新'],['UNAVAILABLE','不可购买'],['INVALID','已失效']].map(([value, label]) => (
          <button key={value || 'all'} className={`chip ${status === value ? 'is-active' : ''}`} onClick={() => setStatus(value)}>{label}</button>
        ))}
      </div>
      {message && <p className="status" role="status">{message}</p>}
      {error && <p className="error" role="alert">{error}</p>}
      {items.length === 0 && <p className="muted">还没有符合条件的收藏。</p>}
      <div className="card-grid">
        {items.map(item => (
          <article className="product-card favorite-card" key={item.id}>
            <label className="check-row"><input type="checkbox" checked={selected.includes(item.id)} onChange={() => toggle(item.id)} />批量选择</label>
            {item.cover_url ? <img src={coverSrc(item.cover_url)} alt={item.snapshot_title} /> : <div className="placeholder-cover">暂无图片</div>}
            <strong>{item.current_title || item.snapshot_title}</strong>
            <span>{priceLabel(item.current_price_cents || item.snapshot_price_cents)}</span>
            <em className={`badge badge-${item.status.toLowerCase()}`}>{item.status_label || favoriteStatusLabel(item.status)}</em>
            {item.status === 'UPDATED' && <p className="muted">收藏时 {priceLabel(item.snapshot_price_cents)} / {item.snapshot_title}</p>}
            {!item.available && <p className="error">该商品已失效或不可购买，建议取消收藏。</p>}
            <p className="muted">{item.folder}{item.note ? ` · ${item.note}` : ''}</p>
            <div className="inline-actions">
              {item.available ? <Link className="account-link" to={`/products/${item.product_id}`}>查看商品</Link> : <span className="muted">无法打开</span>}
              {item.status !== 'ACTIVE' && <button className="ghost" onClick={() => void ack(item.id)}>已知晓</button>}
              <button className="ghost" onClick={() => void removeOne(item.id)}>取消收藏</button>
            </div>
            <label>移动到分类<input defaultValue={item.folder} onBlur={event => { if (event.target.value !== item.folder) void move(item.id, event.target.value); }} /></label>
          </article>
        ))}
      </div>
    </main>
  );
}

export function AccountCenter({ onLogout }: { onLogout: () => Promise<void> }) {
  const [payload, setPayload] = useState<ProfilePayload | null>(null);
  const [addresses, setAddresses] = useState<Address[]>([]);
  const [notifications, setNotifications] = useState<NotificationItem[]>([]);
  const [activities, setActivities] = useState<ActivityItem[]>([]);
  const [displayName, setDisplayName] = useState('');
  const [bio, setBio] = useState('');
  const [phone, setPhone] = useState('');
  const [prefs, setPrefs] = useState<Preferences | null>(null);
  const [addressForm, setAddressForm] = useState({ recipient: '', phone: '', province: '', city: '', district: '', detail: '', postal_code: '', is_default: true });
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const load = () => {
    request<ProfilePayload>('/api/v1/me/profile').then(body => {
      setPayload(body); setDisplayName(body.profile.display_name); setBio(body.profile.bio || ''); setPhone(body.profile.phone || ''); setPrefs(body.preferences);
    }).catch(reason => setError(reason instanceof Error ? reason.message : '加载失败'));
    request<{ items: Address[] }>('/api/v1/me/addresses').then(body => setAddresses(body.items)).catch(() => undefined);
    request<{ items: NotificationItem[] }>('/api/v1/me/notifications?page=1&page_size=8').then(body => setNotifications(body.items)).catch(() => undefined);
    request<{ items: ActivityItem[] }>('/api/v1/me/activities?page=1&page_size=8').then(body => setActivities(body.items)).catch(() => undefined);
  };
  useEffect(() => { load(); }, []);
  const saveProfile = async (event: FormEvent) => {
    event.preventDefault(); setError('');
    try { await request('/api/v1/me/profile', { method: 'PUT', body: JSON.stringify({ display_name: displayName, bio, phone }) }); setMessage('资料已保存'); load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败'); }
  };
  const savePrefs = async () => {
    if (!prefs) return;
    try { await request('/api/v1/me/preferences', { method: 'PUT', body: JSON.stringify(prefs) }); setMessage('偏好已保存'); load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '保存失败'); }
  };
  const saveAddress = async (event: FormEvent) => {
    event.preventDefault(); setError('');
    try { await request('/api/v1/me/addresses', { method: 'POST', body: JSON.stringify(addressForm) }); setMessage('地址已保存'); setAddressForm({ ...addressForm, recipient: '', phone: '', detail: '' }); load(); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '保存地址失败'); }
  };
  const markRead = async (id?: string) => {
    try {
      if (id) await request(`/api/v1/me/notifications/${id}/read`, { method: 'POST' });
      else await request('/api/v1/me/notifications/read-all', { method: 'POST' });
      load();
    } catch (reason) { setError(reason instanceof Error ? reason.message : '标记失败'); }
  };
  return (
    <main className="wide">
      <p className="eyebrow">PROFILE</p>
      <h1>{payload?.profile.display_name || payload?.user.username}</h1>
      <p className="lead">{payload?.user.email || '未填写邮箱'} · 收藏 {payload?.stats.favorites ?? 0} · 未读通知 {payload?.stats.unread_notifications ?? 0}</p>
      <div className="entry-grid">
        <Link className="product-card" to="/favorites"><strong>我的收藏</strong><span>分类筛选、批量管理、失效提示</span></Link>
        <Link className="product-card" to="/sandbox"><strong>交易沙盒</strong><span>虚拟资金 {priceLabel(payload?.sandbox.cash_cents ?? 0)}</span></Link>
        <Link className="product-card" to="/sell"><strong>我的发布</strong><span>继续编辑或发布商品</span></Link>
      </div>
      {message && <p className="status" role="status">{message}</p>}
      {error && <p className="error" role="alert">{error}</p>}
      <section className="split-grid">
        <form className="auth-card publish-form" onSubmit={saveProfile}>
          <h2>资料编辑</h2>
          <label>显示名称<input value={displayName} onChange={e => setDisplayName(e.target.value)} minLength={2} maxLength={64} required /></label>
          <label>简介<textarea value={bio} onChange={e => setBio(e.target.value)} rows={4} maxLength={280} /></label>
          <label>手机号<input value={phone} onChange={e => setPhone(e.target.value)} /></label>
          <button>保存资料</button>
        </form>
        <section className="auth-card publish-form">
          <h2>偏好设置</h2>
          {prefs && <>
            <label className="check-row"><input type="checkbox" checked={prefs.notify_favorite_updates} onChange={e => setPrefs({ ...prefs, notify_favorite_updates: e.target.checked })} />收藏商品更新通知</label>
            <label className="check-row"><input type="checkbox" checked={prefs.notify_trade_events} onChange={e => setPrefs({ ...prefs, notify_trade_events: e.target.checked })} />沙盒成交通知</label>
            <label className="check-row"><input type="checkbox" checked={prefs.notify_system} onChange={e => setPrefs({ ...prefs, notify_system: e.target.checked })} />系统通知</label>
            <label>默认收藏分类<input value={prefs.default_favorite_folder} onChange={e => setPrefs({ ...prefs, default_favorite_folder: e.target.value })} /></label>
            <button type="button" onClick={() => void savePrefs()}>保存偏好</button>
          </>}
        </section>
      </section>
      <section className="split-grid">
        <form className="auth-card publish-form" onSubmit={saveAddress}>
          <h2>收货地址</h2>
          <label>收件人<input value={addressForm.recipient} onChange={e => setAddressForm({ ...addressForm, recipient: e.target.value })} required /></label>
          <label>手机号<input value={addressForm.phone} onChange={e => setAddressForm({ ...addressForm, phone: e.target.value })} required /></label>
          <label>省 / 市<input value={addressForm.province} onChange={e => setAddressForm({ ...addressForm, province: e.target.value })} required /></label>
          <label>城市<input value={addressForm.city} onChange={e => setAddressForm({ ...addressForm, city: e.target.value })} required /></label>
          <label>详细地址<input value={addressForm.detail} onChange={e => setAddressForm({ ...addressForm, detail: e.target.value })} required /></label>
          <label className="check-row"><input type="checkbox" checked={addressForm.is_default} onChange={e => setAddressForm({ ...addressForm, is_default: e.target.checked })} />设为默认</label>
          <button>新增地址</button>
          <div className="stack-list">{addresses.map(item => <div className="profile-row" key={item.id}><span>{item.recipient} · {item.province}{item.city}{item.detail}</span><strong>{item.is_default ? '默认' : item.phone}</strong></div>)}</div>
        </form>
        <section className="auth-card publish-form">
          <div className="toolbar"><h2>消息通知</h2><button type="button" className="ghost" onClick={() => void markRead()}>全部已读</button></div>
          {notifications.length === 0 && <p className="muted">暂无通知。</p>}
          {notifications.map(item => (
            <article className="notice-item" key={item.id}>
              <strong>{item.title}</strong>
              <p className="muted">{item.body}</p>
              <span>{formatTime(item.created_at)}</span>
              {!item.read_at && <button className="ghost" onClick={() => void markRead(item.id)}>标为已读</button>}
            </article>
          ))}
        </section>
      </section>
      <section className="auth-card publish-form">
        <h2>浏览与操作历史</h2>
        {activities.length === 0 && <p className="muted">还没有操作记录。</p>}
        {activities.map(item => <div className="profile-row" key={item.id}><span>{activityLabel(item.action)} · {item.detail}</span><strong>{formatTime(item.created_at)}</strong></div>)}
      </section>
      <button className="secondary" onClick={() => void onLogout()}>退出登录</button>
    </main>
  );
}

export function SandboxPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const [snapshot, setSnapshot] = useState<SandboxSnapshot | null>(null);
  const [productId, setProductId] = useState(params.get('product') || '');
  const [quantity, setQuantity] = useState('1');
  const [side, setSide] = useState<'BUY' | 'SELL'>('BUY');
  const [risk, setRisk] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const load = () => request<SandboxSnapshot>('/api/v1/sandbox').then(setSnapshot).catch(reason => setError(reason instanceof Error ? reason.message : '加载失败'));
  useEffect(() => { load(); }, []);
  const trade = async (event: FormEvent) => {
    event.preventDefault(); setError('');
    try {
      await request('/api/v1/sandbox/orders', { method: 'POST', body: JSON.stringify({ product_id: productId, side, quantity: Number(quantity), risk_acknowledged: risk }) });
      setMessage(side === 'BUY' ? '买入已成交' : '卖出已成交');
      load();
    } catch (reason) { setError(reason instanceof Error ? reason.message : '交易失败'); }
  };
  const reset = async () => {
    if (!window.confirm('重置后虚拟资金和持仓会回到初始状态，成交记录仍会保留。确认继续？')) return;
    try { const body = await request<SandboxSnapshot>('/api/v1/sandbox/reset', { method: 'POST', body: JSON.stringify({ confirm: true }) }); setSnapshot(body); setMessage('沙盒已重置'); }
    catch (reason) { setError(reason instanceof Error ? reason.message : '重置失败'); }
  };
  const holdingsValue = useMemo(() => snapshot?.holdings.reduce((sum, item) => sum + item.market_value_cents, 0) ?? 0, [snapshot]);
  return (
    <main className="wide">
      <p className="eyebrow">SANDBOX</p>
      <h1>交易沙盒</h1>
      <p className="lead">{snapshot?.risk_notice}</p>
      <div className="entry-grid">
        <div className="product-card"><strong>虚拟资金</strong><span>{priceLabel(snapshot?.account.cash_cents ?? 0)}</span></div>
        <div className="product-card"><strong>持仓市值</strong><span>{priceLabel(holdingsValue)}</span></div>
        <div className="product-card"><strong>重置次数</strong><span>{snapshot?.account.reset_count ?? 0}</span></div>
      </div>
      {message && <p className="status" role="status">{message}</p>}
      {error && <p className="error" role="alert">{error}</p>}
      <form className="auth-card publish-form" onSubmit={trade}>
        <h2>模拟买卖</h2>
        <label>商品 ID<input value={productId} onChange={e => setProductId(e.target.value)} required placeholder="从商品详情页复制或一键带入" /></label>
        <label>方向<select value={side} onChange={e => setSide(e.target.value as 'BUY' | 'SELL')}><option value="BUY">买入</option><option value="SELL">卖出</option></select></label>
        <label>数量<input type="number" min="1" max="99" value={quantity} onChange={e => setQuantity(e.target.value)} required /></label>
        <label className="check-row"><input type="checkbox" checked={risk} onChange={e => setRisk(e.target.checked)} required />我已了解这是虚拟资金，不产生真实支付或发货</label>
        <div className="inline-actions">
          <button>{side === 'BUY' ? '确认买入' : '确认卖出'}</button>
          <button type="button" className="ghost" onClick={() => void reset()}>重置沙盒</button>
          <button type="button" className="ghost" onClick={() => navigate('/market')}>去市场选品</button>
        </div>
      </form>
      <section>
        <h2>当前持仓</h2>
        {snapshot?.holdings.length === 0 && <p className="muted">暂无持仓。</p>}
        <div className="card-grid">{snapshot?.holdings.map(item => (
          <article className="product-card" key={item.id}>
            <strong>{item.title}</strong>
            <span>数量 {item.quantity} · 成本 {priceLabel(item.avg_cost_cents)}</span>
            <em>市值 {priceLabel(item.market_value_cents)} · 浮动 {priceLabel(item.unrealized_cents)}</em>
            {!item.available && <p className="error">标的已失效，仅可等待重置或保留记录。</p>}
            <button className="ghost" onClick={() => { setProductId(item.product_id); setSide('SELL'); }}>卖出此持仓</button>
          </article>
        ))}</div>
      </section>
      <section>
        <h2>成交记录</h2>
        {snapshot?.orders.length === 0 && <p className="muted">还没有成交。</p>}
        {snapshot?.orders.map(order => (
          <div className="profile-row" key={order.id}>
            <span>{order.side === 'BUY' ? '买入' : '卖出'} {order.product_title} ×{order.quantity} · {priceLabel(order.amount_cents)}</span>
            <strong>{order.status === 'FILLED' ? '已成交' : `已拒绝：${order.reject_reason}`} · {formatTime(order.created_at)}</strong>
          </div>
        ))}
      </section>
    </main>
  );
}

export function ProductActions({ productId, title }: { productId: string; title: string }) {
  const location = useLocation();
  const [ready, setReady] = useState(false);
  const [favorited, setFavorited] = useState(false);
  const [favoriteId, setFavoriteId] = useState('');
  const [folder, setFolder] = useState('默认收藏');
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  useEffect(() => {
    request<{ favorited: boolean; favorite?: Favorite }>(`/api/v1/favorites/status?product_id=${productId}`).then(body => {
      setFavorited(body.favorited); setFavoriteId(body.favorite?.id || ''); setReady(true);
    }).catch(() => setReady(false));
    request('/api/v1/me/activities', { method: 'POST', body: JSON.stringify({ action: 'VIEW_PRODUCT', target_id: productId, detail: title }) }).catch(() => undefined);
  }, [productId, title]);
  const toggle = async () => {
    setError('');
    try {
      if (favorited && favoriteId) {
        await request(`/api/v1/favorites/${favoriteId}`, { method: 'DELETE' });
        setFavorited(false); setFavoriteId(''); setMessage('已取消收藏');
      } else {
        const body = await request<{ favorite: Favorite }>('/api/v1/favorites', { method: 'POST', body: JSON.stringify({ product_id: productId, folder }) });
        setFavorited(true); setFavoriteId(body.favorite.id); setMessage('已加入收藏');
      }
    } catch (reason) { setError(reason instanceof Error ? reason.message : '操作失败'); }
  };
  if (!ready) {
    return (
      <section className="action-bar">
        <Link className="account-link" to="/login" state={{ from: location.pathname }}>登录后收藏或进入沙盒交易</Link>
      </section>
    );
  }
  return (
    <section className="action-bar">
      <label>收藏分类<input value={folder} onChange={e => setFolder(e.target.value)} /></label>
      <button type="button" onClick={() => void toggle()}>{favorited ? '取消收藏' : '收藏商品'}</button>
      <Link className="account-link" to={`/sandbox?product=${productId}`}>去沙盒交易</Link>
      {message && <span className="status">{message}</span>}
      {error && <span className="error">{error}</span>}
    </section>
  );
}
