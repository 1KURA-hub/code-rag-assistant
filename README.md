# 代码仓库 RAG 助手

一个代码仓库理解与变更影响分析工具。系统可以导入 GitHub 公开仓库，扫描代码文件，按 Go 函数/类型或通用行窗口切分代码，生成 embedding 后写入 PostgreSQL + pgvector，并支持代码问答、diff 影响分析和引用代码片段展示。

这个项目不是企业知识库问答，而是一个代码仓库分析工具。核心重点是后端链路：

```text
GitHub 仓库导入
-> 代码扫描
-> 函数/类型级分片
-> embedding
-> pgvector 存储
-> 相似度检索
-> 组织 prompt
-> 问答 / 变更影响分析
-> 引用代码证据
```

## 功能

- 导入 GitHub 公开仓库 URL。
- 下载默认分支 ZIP，解压并扫描 `.go`、`.md`、`.yaml`、`.yml`、`.json` 文件。
- Go 代码优先按函数、方法、类型切分；其他文件按行窗口切分。
- 支持 OpenAI 兼容 embedding 接口；未配置 API Key 时使用本地 hash embedding fallback。
- 使用 PostgreSQL + pgvector 存储和检索代码向量。
- 代码问答返回回答和引用代码片段。
- diff 影响分析返回变更总结、影响模块、风险点、建议测试和引用代码。
- 仓库状态记录文件数、分片数、索引耗时和索引时间。
- 单页中文工作台用于演示。

## 快速启动

```bash
docker compose up -d postgres

export POSTGRES_DSN="host=127.0.0.1 user=code_rag password=code_rag dbname=code_rag port=5433 sslmode=disable"

# 可选：启用大模型回答和真实 embedding
# export OPENAI_API_KEY="..."
# export OPENAI_BASE_URL="https://api.openai.com/v1"
# export OPENAI_MODEL="gpt-4o-mini"
# export EMBEDDING_MODEL="text-embedding-3-small"

go run ./cmd/server
```

打开：

```text
http://127.0.0.1:8090
```

## API

### 创建或重新索引仓库

```http
POST /api/repos
Content-Type: application/json

{
  "repo_url": "https://github.com/owner/repo"
}
```

同一个 `owner/repo` 重复导入时会复用仓库记录并重新索引。

### 查询仓库状态

```http
GET /api/repos/:id
```

响应包含：

```json
{
  "id": 1,
  "repo_url": "https://github.com/owner/repo",
  "owner": "owner",
  "name": "repo",
  "status": "ready",
  "error_message": "",
  "file_count": 42,
  "chunk_count": 168,
  "index_duration_ms": 3500,
  "indexed_at": "2026-04-27T12:00:00Z"
}
```

### 代码问答

```http
POST /api/ask
Content-Type: application/json

{
  "repository_id": 1,
  "question": "这个项目的消息消费主流程是什么？"
}
```

### 变更影响分析

```http
POST /api/impact
Content-Type: application/json

{
  "repository_id": 1,
  "diff_text": "diff --git ..."
}
```

## 设计要点

- 为什么代码 RAG 需要分片：分片粒度决定召回质量；Go 文件按函数/方法/类型分片比按固定行数更容易命中业务语义。
- 为什么需要 pgvector：代码块 embedding 存在 PostgreSQL 中，通过向量距离找与问题或 diff 最相关的代码。
- 为什么回答带 citations：回答必须能回到具体文件、行号和函数，避免纯大模型幻觉。
- 变更影响分析怎么做：先从 diff 解析文件路径和关键词，再检索相关代码，最后输出影响模块、风险点和建议测试。
- 为什么保留本地 embedding fallback：在没有 API Key 的环境中仍可完成基础检索；生产环境可以配置真实 embedding 模型。

## 当前范围

V1 只支持 GitHub 公开仓库和默认分支 ZIP 导入。不支持私有仓库 OAuth、分支选择、增量索引、多用户权限、Agent 自动改代码。
