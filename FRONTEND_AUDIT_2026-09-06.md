# 前端代码库审计报告

**审计日期**：2026-09-06  
**审计范围**：`apps/web/src`  
**发现问题数**：39 个

---

## 🔴 高严重度问题（必须立即修复）

### 1. **main.tsx 巨型文件（1,109 行）** —— 部分已解决
**问题**：所有页面组件、路由、上下文都挤在一个文件中  
**影响**：
- 难以维护和协作
- 构建时无法有效分包
- 代码审查困难

**改进方案**：
```typescript
// 拆分结构
src/
  pages/
    Home.tsx
    Market.tsx
    Product.tsx
    Publish.tsx
    Generation.tsx
    Account.tsx
    Orders.tsx
  components/
    Navigation.tsx
    Footer.tsx
    ErrorBoundary.tsx
  contexts/
    AuthContext.tsx
  App.tsx  // 仅保留路由配置
```

### 2. **依赖版本全部使用 `latest`** —— 已解决
**问题**：`package.json` 所有依赖都是 `"latest"`  
**影响**：
- 构建不可复现
- 可能引入破坏性更新
- 团队协作时版本不一致

**立即执行**：
```bash
cd apps/web
npm install  # 生成 package-lock.json
git add package-lock.json
```

修改 `package.json`：
```json
{
  "dependencies": {
    "@react-three/drei": "^9.105.0",
    "@react-three/fiber": "^8.16.0",
    "lucide-react": "^0.263.1",
    "react": "^18.2.0",
    "react-dom": "^18.2.0",
    "react-router-dom": "^6.22.0",
    "three": "^0.163.0"
  }
}
```

### 3. **缺少全局错误边界** —— 已解决
**问题**：任何组件抛出错误都会导致整个应用白屏  
**位置**：`main.tsx` 缺少顶层 `ErrorBoundary`

**实现**：
```typescript
// components/ErrorBoundary.tsx
class ErrorBoundary extends Component<{children: ReactNode}, {error: Error | null}> {
  state = { error: null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  render() {
    if (this.state.error) {
      return <div className="error-page">应用出错，请刷新重试</div>;
    }
    return this.props.children;
  }
}
```

### 4. **API 错误处理不统一** —— 部分已解决
**问题**：每个页面都手动处理 `fetchJson` 错误  
**位置**：Market.tsx:37, Product.tsx:31, Publish.tsx:101 等

**改进**：创建统一的 `useQuery` hook
```typescript
function useQuery<T>(url: string) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const controller = new AbortController();
    fetchJson<T>(url, { signal: controller.signal })
      .then(setData)
      .catch(err => err.name !== 'AbortError' && setError(err.message))
      .finally(() => setLoading(false));
    return () => controller.abort();
  }, [url]);

  return { data, error, loading };
}
```

---

## 🟡 中严重度问题（应尽快处理）

### 5. **组件缺少性能优化** —— 部分已解决
**问题**：
- `Navigation` 和 `Footer` 每次路由切换都重新渲染
- 事件处理器未使用 `useCallback`
- 列表渲染缺少 `key` 优化

**位置**：
- `main.tsx:180-211` (Navigation)
- `main.tsx:1101-1107` (Footer)
- `Market.tsx:88` (产品卡片)

**改进**：
```typescript
const Navigation = memo(() => {
  // 组件代码
});

const handleSubmit = useCallback(async (e: FormEvent) => {
  e.preventDefault();
  // 提交逻辑
}, [依赖项]);
```

### 6. **大量代码重复**
**重复模式**：
- **表单提交逻辑**（8 处）- Login, Register, Publish, Generation 等
- **数据加载模式**（12 处）- useState + useEffect + fetchJson
- **错误显示**（15 处）- `{error && <p className="error">{error}</p>}`

**解决方案**：
```typescript
// hooks/useForm.ts
function useForm<T>(onSubmit: (data: T) => Promise<void>) {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  
  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setLoading(true);
    setError('');
    try {
      await onSubmit(new FormData(e.target) as T);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };
  
  return { handleSubmit, loading, error };
}

// 使用
const { handleSubmit, loading, error } = useForm(async (data) => {
  await fetchJson('/api/v1/login', { method: 'POST', body: data });
});
```

### 7. **类型定义不够严格** —— 部分已解决
**问题**：
- `api.ts:178` 使用 `as Record<string, string>` 类型断言
- `Product.tsx:28` 使用 `any` 类型
- `Generation.tsx:145` 缺少 `GenerationJob` 类型导出

**改进**：
```typescript
// types.ts - 集中管理类型
export interface Product {
  id: string;
  name: string;
  description: string;
  price_yuan: number;
  seller_id: string;
  seller_username: string;
  // ...完整字段
}

export interface GenerationJob {
  id: string;
  status: 'pending' | 'processing' | 'completed' | 'failed';
  prompt: string;
  // ...
}
```

### 8. **轮询逻辑效率低** —— 已解决
**问题**：`Generation.tsx:67-87` 无限轮询，即使任务完成也继续请求  
**改进**：
```typescript
useEffect(() => {
  if (!activeJob || activeJob.status === 'completed' || activeJob.status === 'failed') {
    return; // 终止轮询
  }

  const timer = setInterval(async () => {
    const updated = await fetchJson(`/api/v1/generation-jobs/${activeJob.id}`);
    setActiveJob(updated);
    if (updated.status === 'completed' || updated.status === 'failed') {
      clearInterval(timer); // 显式清除
    }
  }, 3000);

  return () => clearInterval(timer);
}, [activeJob?.id, activeJob?.status]);
```

### 9. **缺少加载状态统一组件** —— 已解决
**问题**：每个页面都写 `{loading && <p>加载中...</p>}`  
**改进**：
```typescript
// components/LoadingSpinner.tsx
export function LoadingSpinner({ fullscreen = false }) {
  return (
    <div className={fullscreen ? 'loading-fullscreen' : 'loading-inline'}>
      <div className="spinner" />
      <span>加载中...</span>
    </div>
  );
}

// 使用
{loading ? <LoadingSpinner /> : <ProductList products={data} />}
```

### 10. **表单验证逻辑分散** —— 部分已解决
**问题**：
- `Publish.tsx:118-121` 手动检查文件数量
- `Generation.tsx:112-114` 手动检查 prompt 长度
- 缺少统一的验证错误提示

**已完成**：
- 新增 `src/hooks/useForm.ts`，统一表单提交中的提交状态、错误清空、异常转换和重复提交保护。
- 生成工作台增加 Prompt/商品类型非空、确认 Prompt 非空、预览过期校验。
- 商品发布增加文本字段、金额和非负整数库存校验。

**仍待处理**：复杂字段规则和跨页面 schema 校验仍未统一；暂不引入 `react-hook-form` + `zod`，避免为当前窄范围改动扩大依赖和迁移面。

---

## 🟢 低严重度问题（可以逐步改进）

### 11. **代码注释不足**
**位置**：`ModelViewer.tsx:39-49` 复杂的资源清理逻辑缺少注释

### 12. **Magic Numbers 未抽离** —— 部分已解决
**示例**：
- `ModelViewer.tsx:67` - `1.7` 缩放比例
- `Generation.tsx:72` - `3000` 轮询间隔
- `api.ts:33` - `401` 状态码

**改进**：
```typescript
const CONFIG = {
  MODEL_SCALE_FACTOR: 1.7,
  POLLING_INTERVAL_MS: 3000,
  HTTP_STATUS: {
    UNAUTHORIZED: 401,
    FORBIDDEN: 403,
  }
};
```

### 13. **可访问性改进** —— 部分已解决
- 按钮缺少 `aria-label`（ModelViewer.tsx:226-231）
- 表单缺少 `<label>` 关联（多处）
- 焦点管理缺失（全屏切换后）
- 颜色对比度未验证（错误消息、标签）

**改进**：
```typescript
<button 
  type="button" 
  className="ghost" 
  onClick={resetView}
  disabled={showFallback}
  aria-label="重置 3D 模型视角"
>
  <RotateCcw size={16} /> 重置视角
</button>
```

### 14. **CSS 类名可以改进**
**问题**：类名不够语义化（`.ghost`, `.muted`）  
**建议**：使用 BEM 或 CSS Modules

### 15. **缺少国际化准备**
**问题**：所有文本硬编码中文  
**建议**：后续可引入 `react-i18next`

---

## 📊 优先级行动清单

### 🚨 本周必须完成

1. ✅ **锁定依赖版本** - 移除 `latest`，生成 `package-lock.json`
2. ✅ **拆分 main.tsx** - 提取为独立页面文件
3. ✅ **添加全局错误边界** - 防止白屏
4. ✅ **创建 useQuery hook** - 统一数据获取逻辑

### 📅 下周优化

5. ✅ 为 Navigation/Footer 添加 `memo`
6. ✅ 提取表单处理 hook（useForm）
7. ✅ 创建基础 UI 组件（LoadingSpinner）
8. ✅ 修复轮询逻辑终止条件

### 🎯 月度改进

9. 引入 React Query 替代手动数据获取
10. 添加表单验证（react-hook-form + zod）
11. 完善可访问性（ARIA 属性）
12. 添加单元测试（Vitest + Testing Library）

---

## 🎯 代码健康度评分

| 维度 | 当前评分 | 目标评分 | 关键问题 |
|------|----------|----------|----------|
| **组件结构** | ⭐⭐ | ⭐⭐⭐⭐ | 巨型文件需拆分 |
| **状态管理** | ⭐⭐⭐ | ⭐⭐⭐⭐ | 缺少统一模式 |
| **性能优化** | ⭐⭐ | ⭐⭐⭐⭐ | 缺少 memo/callback |
| **代码复用** | ⭐⭐ | ⭐⭐⭐⭐ | 大量重复逻辑 |
| **类型安全** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 少量 any/断言 |
| **错误处理** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 不够统一 |
| **可访问性** | ⭐⭐⭐ | ⭐⭐⭐⭐ | 部分支持 |
| **依赖管理** | ⭐ | ⭐⭐⭐⭐⭐ | 使用 latest |

**综合评分：⭐⭐⭐ (3/5)** → **目标：⭐⭐⭐⭐ (4/5)**

---

## 💡 实施建议

### 短期优化（本周）
专注于高严重度问题，特别是依赖锁定和代码拆分。这些改动风险可控且收益明显。

### 中期改进（2-4 周）
逐步提取通用 hooks 和组件，减少代码重复。可以在新功能开发时同步进行。

### 长期规划（1-3 个月）
引入成熟的状态管理和表单处理方案，建立完善的组件库和测试体系。

---

## 审计方法说明

本次审计通过以下方式进行：
1. **静态代码分析**：检查组件结构、类型定义、代码复用
2. **依赖检查**：分析 package.json 版本管理
3. **性能审查**：识别不必要的重渲染和资源浪费
4. **最佳实践对比**：与 React 生态主流实践对照
5. **可访问性检查**：验证 ARIA 属性和语义化标签

---

**报告生成时间**：2026-09-06  
**本轮跟进**：已完成表单提交 Hook、生成与商品发布基础校验；构建与针对性诊断通过。复杂 schema 校验、全量 ARIA 清查和单元测试仍列为后续工作。  
**后续跟进**：建议每月进行一次健康度复查
