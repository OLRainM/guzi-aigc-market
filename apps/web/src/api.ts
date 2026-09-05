export const apiBase = import.meta.env.VITE_API_BASE_URL || '';
export const MAX_MODEL_BYTES = 20 * 1024 * 1024;

export type Role = { code: string; name: string };
export type User = { id: string; username: string; email?: string; status: string; roles: Role[] };
export type AuthPayload = { access_token: string; expires_in: number; user: User };
export type AssetFile = { id: string; kind: string; mime_type: string; size_bytes: number; sha256: string; original_name: string; content_url: string };
export type Product = {
  id: string; seller_id: string; title: string; description: string; price_cents: number; ip_name: string; category: string;
  condition: string; transaction_type: string; shipping_origin?: string; stock: number; preorder_note?: string; status: string;
  cover_asset_id?: string; model_asset_id?: string; images: AssetFile[]; model?: AssetFile | null;
};
export type ProductList = { items: Product[]; page: number; page_size: number; total: number };
export type AdminJob = GenerationJob;
export type AuditLog = { id: string; actor_id: string; action: string; target_type: string; target_id: string; request_id: string; before?: Record<string, unknown>; after?: Record<string, unknown>; created_at: string };
export type AdminList<T> = { items: T[]; page: number; page_size: number; total: number };
export type GenerationOutput = { id: string; output_type: string; format: string; mime_type: string; size_bytes: number; content_url?: string };
export type PromptPreview = {
  id: string; raw_prompt: string; product_type: string; optimized_prompt: string;
  structured_prompt?: Record<string, unknown>; rag_context?: Record<string, unknown>;
  rag_version?: string; template_version?: string; source: string; expires_at: string;
};
export type GenerationJob = {
  id: string; status: string; stage: string; progress: number; raw_prompt: string;
  optimized_prompt?: string | null; product_type?: string; provider: string;
  attempt: number; max_attempts: number; outputs: GenerationOutput[];
  error?: { code: string; message: string; retryable: boolean } | null;
  created_at: string; updated_at: string;
};
export type GenerationJobList = { items: GenerationJob[]; page: number; page_size: number; total: number };
export type Favorite = {
  id: string; product_id: string; folder: string; note?: string;
  snapshot_title: string; snapshot_price_cents: number; snapshot_status: string;
  snapshot_category: string; snapshot_ip_name: string; status: string; status_label: string;
  current_title?: string; current_price_cents?: number; current_status?: string;
  cover_url?: string; available: boolean; created_at: string;
};
export type FavoriteList = { items: Favorite[]; page: number; page_size: number; total: number };
export type FavoriteFolder = { folder: string; count: number };
export type Profile = { user_id: string; display_name: string; bio?: string; phone?: string };
export type Address = {
  id: string; recipient: string; phone: string; province: string; city: string;
  district?: string; detail: string; postal_code?: string; is_default: boolean;
};
export type Preferences = {
  notify_favorite_updates: boolean; notify_trade_events: boolean; notify_system: boolean;
  default_favorite_folder: string; locale: string;
};
export type NotificationItem = { id: string; kind: string; title: string; body: string; link?: string; read_at?: string | null; created_at: string };
export type ActivityItem = { id: string; action: string; target_type?: string; target_id?: string; detail?: string; created_at: string };
export type ProfilePayload = {
  user: User; profile: Profile; preferences: Preferences;
  sandbox: { cash_cents: number; generation: number; reset_count: number };
  stats: { favorites: number; unread_notifications: number; addresses: number; buy_orders?: number; sell_orders?: number };
  };
  export type TradeOrderEvent = { id: string; from_status?: string; to_status: string; actor_role: string; note?: string; created_at: string };
  export type TradeOrder = {
  id: string; buyer_id: string; seller_id: string; product_id: string; address_id: string;
  quantity: number; unit_price_cents: number; amount_cents: number; status: string;
  product_title: string; cover_url?: string; recipient: string; phone: string; address_text: string;
  tracking_no?: string; cancel_reason?: string; paid_at?: string | null; canceled_at?: string | null;
  shipped_at?: string | null; completed_at?: string | null; created_at: string; events?: TradeOrderEvent[];
  };
  export type TradeOrderList = { items: TradeOrder[]; page: number; page_size: number; total: number; role: string };
export type SandboxHolding = {
  id: string; product_id: string; quantity: number; avg_cost_cents: number; title: string;
  current_price_cents: number; market_value_cents: number; unrealized_cents: number;
  cover_url?: string; available: boolean;
};
export type SandboxOrder = {
  id: string; product_id: string; product_title: string; side: string; quantity: number;
  price_cents: number; amount_cents: number; status: string; reject_reason?: string;
  generation: number; created_at: string;
};
export type SandboxSnapshot = {
  account: { cash_cents: number; generation: number; reset_count: number };
  holdings: SandboxHolding[]; orders: SandboxOrder[];
  risk_notice: string; starting_cash_cents: number;
};

export function favoriteStatusLabel(status: string) {
  return ({ ACTIVE: '有效', UPDATED: '已更新', UNAVAILABLE: '不可购买', INVALID: '已失效' } as Record<string, string>)[status] ?? status;
}

export function activityLabel(action: string) {
  return ({
    VIEW_PRODUCT: '浏览商品', FAVORITE_ADD: '加入收藏', FAVORITE_REMOVE: '取消收藏',
    PROFILE_UPDATE: '更新资料', ADDRESS_SAVE: '保存地址', SANDBOX_TRADE: '沙盒交易', SANDBOX_RESET: '重置沙盒',
    ORDER_CREATE: '创建订单', ORDER_PAY: '模拟支付', ORDER_CANCEL: '取消订单', ORDER_SHIP: '模拟发货', ORDER_CONFIRM: '确认收货',
  } as Record<string, string>)[action] ?? action;
}

export function orderStatusLabel(status: string) {
  return ({
    PENDING_PAYMENT: '待支付', PAID: '已支付待发货', SHIPPED: '已发货', COMPLETED: '已完成', CANCELED: '已取消',
  } as Record<string, string>)[status] ?? status;
}

export function newIdempotencyKey() {
  return crypto.randomUUID();
}

export function generationStatusLabel(status: string) {
  return ({ QUEUED: '排队中', RUNNING: '生成中', SUCCEEDED: '已完成', FAILED: '失败', CANCELED: '已取消' } as Record<string, string>)[status] ?? status;
}

export function generationStageLabel(stage: string) {
  return ({
    QUEUED: '排队中',
    OPTIMIZING_PROMPT: '正在优化提示词',
    SUBMITTING_PROVIDER: '正在提交生成',
    GENERATING: '正在生成模型',
    FETCHING_OUTPUT: '正在拉取结果',
    STORING_OUTPUT: '正在保存模型',
    COMPLETED: '已完成',
  } as Record<string, string>)[stage] ?? stage;
}

let accessToken = '';
let refreshPromise: Promise<AuthPayload> | null = null;

export function setAccessToken(token: string) {
  accessToken = token;
}

export function refreshSession() {
  if (!refreshPromise) {
    refreshPromise = request<AuthPayload>('/api/v1/auth/refresh', { method: 'POST' }, '').finally(() => { refreshPromise = null; });
  }
  return refreshPromise;
}

export async function request<T>(path: string, init: RequestInit = {}, token = accessToken): Promise<T> {
  const headers = new Headers(init.headers);
  if (!(init.body instanceof FormData) && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const response = await fetch(`${apiBase}${path}`, { ...init, credentials: 'include', headers });
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new Error(body?.error?.message ?? '请求失败，请稍后重试');
  }
  return response.status === 204 ? undefined as T : response.json();
}

export async function requestBlob(path: string, token = accessToken): Promise<Blob> {
  const headers = new Headers();
  if (token) headers.set('Authorization', `Bearer ${token}`);
  const response = await fetch(`${apiBase}${path}`, { credentials: 'include', headers });
  if (!response.ok) {
    throw new Error('资源加载失败');
  }
  return response.blob();
}

export function assetSrc(asset: AssetFile) {
  return `${apiBase}${asset.content_url}`;
}

export function priceLabel(cents: number) {
  return `¥${(cents / 100).toFixed(2)}`;
}

export function statusLabel(status: string) {
  return ({ DRAFT: '草稿', PUBLISHED: '已发布', OFF_SHELF: '已下架' } as Record<string, string>)[status] ?? status;
}

export function formatMegabytes(bytes: number) {
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}
