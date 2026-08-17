# 二次元谷子与 3D Prompt 术语库

本目录为“用户自然语言 → RAG 检索 → Prompt 优化 → 预览图/文本生成 3D”准备。词库覆盖谷子品类、角色人设、外貌、服装题材、画风、构图、3D 技术约束以及材质工艺。

## 目录结构

```text
source/terminology.json             人工维护的规范词源数据
data/terms.jsonl                    一术语一文档，可直接送入向量库
data/categories.json                类别统计
data/manifest.json                  版本、数量和校验摘要
schema/term.schema.json             JSON Schema
rules/compatibility.json            互斥、默认补全和风险规则
prompts/                            Prompt 优化、预览图和 3D 模板
scripts/build_knowledge_base.py    从 source 构建 JSONL
scripts/validate_knowledge_base.py 无第三方依赖的校验脚本
```

## 构建和校验

在本目录执行：

```powershell
py scripts/build_knowledge_base.py
py scripts/validate_knowledge_base.py
```

构建脚本不会生成 embedding。应用运行时使用项目统一的 embedding 模型，对 `data/terms.jsonl` 的 `content` 生成向量，并将 `metadata` 原样保存为过滤字段。

## 推荐的向量库字段

| 字段 | 用途 |
| --- | --- |
| `id` | 由规范词派生的稳定主键，便于增量更新 |
| `content` | 用于 embedding 的自包含中文文本 |
| `metadata.category` | 按商品、角色、风格或 3D 技术过滤 |
| `metadata.prompt_targets` | 区分 `text_to_3d` 和 `preview_image` |
| `metadata.retrieval_keywords` | BM25/关键词召回 |
| `metadata.prompt_fragments` | 直接提供给 Prompt 优化器的片段 |
| `metadata.risk_tags` | 版权、未成年指向和内容审核 |

建议采用混合检索：关键词/BM25 召回缩写和中日英别名，向量召回口语描述，最后按 `category`、`prompt_targets` 和 `risk_tags` 做过滤与重排。

## 版本规则

修改 `source/terminology.json` 后重新构建，提交 `data/terms.jsonl`、`data/categories.json` 和 `data/manifest.json`。删除或重命名术语时保留旧 ID 的迁移记录，避免线上向量库产生孤儿记录。ID 由规范词的 SHA-1 前8位派生，新增或重排其他词条不会改变已有 ID。
