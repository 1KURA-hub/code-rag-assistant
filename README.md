# 代码仓库 RAG 助手

一个面向代码仓库理解的 RAG 工具。它可以导入 GitHub 公开仓库，扫描源码并按代码结构切分，通过 embedding 和 PostgreSQL + pgvector 建立可检索索引，然后基于检索到的代码片段回答问题或分析 git diff 的影响范围。

我做这个项目的重点不是企业知识库问答，而是把 RAG 放到代码场景里：让回答能回到具体文件、函数和行号，减少只靠大模型记忆或猜测带来的不确定性。

## 项目能做什么

- 导入 GitHub 公开仓库 URL，并异步完成索引。
- 扫描 `.go`、`.md`、`.yaml`、`.yml`、`.json` 等文件。
- Go 文件优先按函数、方法、类型声明分片，其他文件按行窗口分片。
- 将代码片段、元信息和向量写入 PostgreSQL + pgvector。
- 根据用户问题召回相关代码片段，并生成带代码依据的回答。
- 粘贴 git diff 后，分析影响模块、风险点和建议测试。
- 记录仓库索引状态、文件数、分片数、索引耗时和失败原因。

## 核心链路

仓库索引链路：

```text
GitHub URL
-> 解析 owner/repo
-> 创建或更新仓库记录
-> 后台 goroutine 下载默认分支 ZIP
-> 解压并扫描目标文件
-> Go AST 分片 / 普通行窗口分片
-> 构造 embedding 文本
-> 调用 embedding 模型生成向量
-> 写入 code_chunks
-> 更新仓库状态为 ready
```

代码问答链路：

```text
用户问题
-> 问题 embedding
-> pgvector 召回候选代码片段
-> 根据函数名、文件路径、关键词 rerank
-> 组织 prompt
-> 调用大模型生成回答
-> 返回 answer + citations
```

变更影响分析链路：

```text
git diff
-> 提取变更文件和关键词
-> 检索相关代码片段
-> 组织上下文
-> 输出变更总结、影响模块、风险点和建议测试
```

## 后端设计

### 代码分片

Go 文件使用 `go/parser` 和 `go/ast` 解析源码结构，优先按函数、方法和类型声明生成分片。这样比固定行数切分更容易保留完整语义，例如一个 handler、service 方法或结构体定义通常会落在同一个 chunk 中。

如果 Go 文件解析失败，会降级为普通行窗口分片，避免单个异常文件导致整个仓库索引失败。

### 向量存储

项目只使用 PostgreSQL 作为主数据库，并通过 pgvector 扩展存储代码向量。`repositories` 保存仓库状态和统计信息，`code_chunks` 保存代码片段、文件路径、行号、符号名、源码内容和 embedding 向量。

问答时根据 `repository_id` 限定检索范围，再按向量距离召回相关片段。

### 检索重排

基础召回使用 pgvector 的余弦距离。为了提升代码场景下的命中率，检索会先扩大候选数量，再根据用户问题中的函数名、文件路径和关键词进行 rerank。

例如用户明确询问 `CreateAndIndex` 时，`symbol_name` 命中的分片会获得更高排序权重。

### 异步索引和状态流转

仓库索引通常包含下载、解压、扫描、embedding 和数据库写入，耗时可能较长，所以创建仓库接口不会同步等待索引完成，而是立即返回仓库记录，由后台 goroutine 执行索引。

仓库状态主要包括：

```text
pending  -> 任务已创建，等待后台执行
indexing -> 正在下载、分片、向量化和入库
ready    -> 索引完成，可以问答
failed   -> 首次索引失败，没有可用分片
```

如果重新索引失败，但数据库中仍有旧分片，系统会保持 `ready`，并在 `error_message` 中记录失败原因，继续使用旧索引提供问答能力。

### 缓存和重复任务保护

前端会轮询仓库状态，因此仓库状态查询使用 Redis 做 Cache Aside 缓存：查询时先读 Redis，未命中再查 PostgreSQL 并回写缓存；状态更新后删除缓存，避免读到旧状态。

同一个仓库处于 `pending` 或 `indexing` 时，重复提交不会启动新的索引任务。为了避免服务重启后状态卡死，`pending/indexing` 超过 10 分钟未更新时允许重新提交索引。

## 快速启动

使用 Docker 启动 PostgreSQL、Redis 和应用：

```bash
docker compose up -d --build
```

如果只想本地运行 Go 服务，可以先启动依赖：

```bash
docker compose up -d postgres redis

export POSTGRES_DSN="host=127.0.0.1 user=code_rag password=code_rag dbname=code_rag port=5433 sslmode=disable"
export REDIS_ADDR="127.0.0.1:6379"

go run ./cmd/server
```

打开：

```text
http://127.0.0.1:8090
```

可选的大模型配置：

```bash
export OPENAI_API_KEY="..."
export OPENAI_BASE_URL="https://api.openai.com/v1"
export OPENAI_MODEL="gpt-4o-mini"
export EMBEDDING_MODEL="text-embedding-3-small"
export EMBEDDING_PROVIDER="remote"
export EMBEDDING_BATCH_SIZE="10"
```

也可以使用 OpenAI 兼容接口，例如 DashScope、OpenRouter 等。未配置 API Key 时，项目会使用本地 hash embedding fallback，便于本地跑通流程，但检索质量不等同于真实 embedding 模型。

## API 示例

### 创建或重新索引仓库

```http
POST /api/repos
Content-Type: application/json

{
  "repo_url": "https://github.com/owner/repo"
}
```

同一个 `owner/repo` 重复导入时会复用仓库记录。若当前仓库正在 `pending/indexing`，接口会直接返回当前任务，不重复启动索引。

### 查询仓库状态

```http
GET /api/repos/:id
```

示例响应：

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
  "question": "这个项目的仓库索引流程是什么？"
}
```

返回结果包含回答正文和 citations。每个 citation 会带上文件路径、起止行号、语言、符号名、符号类型、源码内容和相似度分数。

### 变更影响分析

```http
POST /api/impact
Content-Type: application/json

{
  "repository_id": 1,
  "diff_text": "diff --git ..."
}
```

返回结果包含变更总结、影响模块、风险点、建议测试和相关代码依据。

## 当前边界

- 只支持 GitHub 公开仓库和默认分支 ZIP 导入。
- 不支持私有仓库 OAuth、分支选择和增量索引。
- 索引任务目前是进程内 goroutine，不是持久化任务队列。
- 没有多用户权限隔离和仓库归属管理。
- RAG 检索以向量召回和轻量 rerank 为主，没有接入专门的 reranker 模型。
- 不支持自动修改代码，定位是代码理解和影响分析工具。
