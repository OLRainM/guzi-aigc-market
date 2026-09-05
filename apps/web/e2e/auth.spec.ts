import { expect, test } from '@playwright/test';

function uniqueAccount() {
  const suffix = `${Date.now()}${Math.floor(Math.random() * 1000)}`;
  return { username: `e2e${suffix}`.slice(0, 32), email: `e2e${suffix}@example.com`, password: 'E2ePass123!' };
}

test('匿名访问受保护页面会跳转登录', async ({ page }) => {
  await page.goto('/me');
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible();
});

test('注册后刷新页面仍保持登录会话', async ({ page }) => {
  const account = uniqueAccount();
  await page.goto('/login');
  await page.getByRole('button', { name: '没有账户？立即注册' }).click();
  await page.getByLabel('用户名').fill(account.username);
  await page.getByLabel('邮箱（可选）').fill(account.email);
  await page.getByLabel('密码').fill(account.password);
  await page.getByRole('button', { name: '注册并登录' }).click();
  await expect(page).toHaveURL(/\/me$/);
  await expect(page.getByRole('heading', { name: '个人中心' })).toBeVisible();
  await page.reload();
  await expect(page).toHaveURL(/\/me$/);
  await expect(page.getByText(account.username, { exact: true })).toBeVisible();
});
