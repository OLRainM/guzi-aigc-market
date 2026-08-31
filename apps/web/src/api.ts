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
export type GenerationOutput = { id: string; output_type: string; format: string; mime_type: string; size_bytes: number; content_url?: string };
export type GenerationJob = {
  id: string; status: string; stage: string; progress: number; raw_prompt: string; provider: string;
  attempt: number; max_attempts: number; outputs: GenerationOutput[];
  error?: { code: string; message: string; retryable: boolean } | null;
  created_at: string; updated_at: string;
};
export type GenerationJobList = { items: GenerationJob[]; page: number; page_size: number; total: number };

export function newIdempotencyKey() {
  return crypto.randomUUID();
}

export function generationStatusLabel(status: string) {
  return ({ QUEUED: '排队中', RUNNING: '生成中', SUCCEEDED: '已完成', FAILED: '失败', CANCELED: '已取消' } as Record<string, string>)[status] ?? status;
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
