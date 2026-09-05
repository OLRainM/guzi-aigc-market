import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';

async function registerViaAPI(page: import('@playwright/test').Page) {
  const suffix = `${Date.now()}${Math.floor(Math.random() * 1000)}`;
  const response = await page.request.post('/api/v1/auth/register', {
    data: { username: `fail${suffix}`.slice(0, 32), email: `fail${suffix}@example.com`, password: 'E2ePass123!' },
  });
  expect(response.ok()).toBeTruthy();
  return response.json() as Promise<{ access_token: string }>;
}

test('无效订单数量被 API 拒绝且不创建订单', async ({ page }) => {
  const auth = await registerViaAPI(page);
  const response = await page.request.post('/api/v1/orders', {
    headers: { Authorization: `Bearer ${auth.access_token}`, 'Idempotency-Key': randomUUID() },
    data: { product_id: 'not-a-product', address_id: 'not-an-address', quantity: 0 },
  });
  expect(response.status()).toBe(400);
});

test('匿名访问结算页会被保护并跳转登录', async ({ page }) => {
  await page.goto('/checkout');
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible();
});
