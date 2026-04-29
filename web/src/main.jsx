import React, { useEffect, useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  AlertCircle,
  Bot,
  CheckCircle2,
  Clock3,
  Code2,
  FileCode2,
  GitBranch,
  Loader2,
  Maximize2,
  Minimize2,
  Moon,
  PanelRightOpen,
  Plus,
  RefreshCcw,
  SearchCode,
  SendHorizontal,
  Sparkles,
  Sun,
  UserRound,
  X
} from "lucide-react";
import "./styles.css";

const starterPrompts = [
  "这个项目的核心流程是什么？",
  "主要接口有哪些？",
  "diff --git a/internal/service/answer.go b/internal/service/answer.go\n@@ -1,3 +1,3 @@\n- 返回英文回答\n+ 返回中文回答"
];

const indexSteps = [
  "任务创建",
  "索引处理中",
  "可提问"
];

const statusText = {
  idle: "未开始",
  pending: "等待索引",
  indexing: "索引中",
  ready: "已就绪",
  failed: "索引失败"
};

const defaultQuestion = "这个项目的消息消费主流程是什么？";

function App() {
  const [repoURL, setRepoURL] = useState("https://github.com/1KURA-hub/course-select");
  const [repo, setRepo] = useState(null);
  const [input, setInput] = useState(defaultQuestion);
  const [messages, setMessages] = useState([]);
  const [activeCitations, setActiveCitations] = useState([]);
  const [busy, setBusy] = useState(false);
  const [statusMessage, setStatusMessage] = useState("可以导入一个公开 GitHub 仓库。");
  const [theme, setTheme] = useState("light");
  const [repoPopoverOpen, setRepoPopoverOpen] = useState(false);
  const [aboutOpen, setAboutOpen] = useState(false);
  const [evidenceOpen, setEvidenceOpen] = useState(false);
  const [evidenceSize, setEvidenceSize] = useState("normal");
  const [pendingIntent, setPendingIntent] = useState("ask");
  const [lastIntent, setLastIntent] = useState("ask");
  const messageEndRef = useRef(null);
  const repoMenuRef = useRef(null);

  const currentStatus = repo?.status || "idle";
  const isIndexing = currentStatus === "pending" || currentStatus === "indexing";
  const canAsk = repo?.id && repo?.status === "ready";

  useEffect(() => {
    if (!repo?.id) return;
    if (repo.status !== "pending" && repo.status !== "indexing") return;
    const timer = setInterval(() => {
      refreshRepo(repo.id).catch((err) => setStatusMessage(err.message));
    }, 2000);
    return () => clearInterval(timer);
  }, [repo?.id, repo?.status]);

  useEffect(() => {
    messageEndRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [messages.length, busy]);

  useEffect(() => {
    if (!repoPopoverOpen) return;
    function handlePointerDown(event) {
      if (!repoMenuRef.current?.contains(event.target)) {
        setRepoPopoverOpen(false);
      }
    }
    function handleKeyDown(event) {
      if (event.key === "Escape") {
        setRepoPopoverOpen(false);
      }
    }
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [repoPopoverOpen]);

  async function request(path, body) {
    const response = await fetch(path, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    });
    const json = await readJSON(response);
    if (!response.ok) throw new Error(json.error || `请求失败，HTTP ${response.status}`);
    return json;
  }

  async function readJSON(response) {
    const text = await response.text();
    if (!text) {
      if (!response.ok) throw new Error(`服务端返回空响应，HTTP ${response.status}`);
      return {};
    }
    try {
      return JSON.parse(text);
    } catch {
      throw new Error(`服务端返回的不是 JSON：${text.slice(0, 120)}`);
    }
  }

  async function refreshRepo(repoID = repo?.id) {
    if (!repoID) return;
    const response = await fetch(`/api/repos/${repoID}`);
    const json = await readJSON(response);
    if (!response.ok) throw new Error(json.error || `刷新失败，HTTP ${response.status}`);
    setRepo(json);
    setStatusMessage(`仓库 #${json.id}: ${renderStatus(json.status)}${json.error_message ? " - " + json.error_message : ""}`);
  }

  async function indexRepository() {
    try {
      setBusy(true);
      setRepoPopoverOpen(true);
      setStatusMessage("正在创建索引任务...");
      const nextRepo = await request("/api/repos", { repo_url: repoURL });
      setRepo(nextRepo);
      await refreshRepo(nextRepo.id);
    } catch (err) {
      setStatusMessage(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function submitMessage() {
    const value = input.trim();
    if (!value) return;
    if (!canAsk) {
      appendAssistant("请先完成仓库索引，状态变成“已就绪”后再提问或分析。", [], "ask");
      setRepoPopoverOpen(true);
      return;
    }

    const intent = detectIntent(value);
    setPendingIntent(intent);
    setLastIntent(intent);

    const userMessage = {
      id: crypto.randomUUID(),
      role: "user",
      content: value,
      type: intent
    };
    setMessages((items) => [...items, userMessage]);
    setInput("");
    setBusy(true);

    try {
      if (intent === "impact") {
        const data = await request("/api/impact", {
          repository_id: repo.id,
          diff_text: value
        });
        appendAssistant(formatImpact(data), data.citations || [], "impact");
      } else {
        const data = await request("/api/ask", {
          repository_id: repo.id,
          question: value
        });
        appendAssistant(data.answer || "暂无回答。", data.citations || [], "ask");
      }
    } catch (err) {
      appendAssistant(err.message, [], intent);
    } finally {
      setBusy(false);
    }
  }

  function appendAssistant(content, citations, type = "ask") {
    const next = {
      id: crypto.randomUUID(),
      role: "assistant",
      content,
      citations,
      type
    };
    setMessages((items) => [...items, next]);
    setActiveCitations(citations);
    setLastIntent(type);
  }

  function showCitations(citations, type) {
    setActiveCitations(citations || []);
    setLastIntent(type || "ask");
    setEvidenceOpen(true);
  }

  function newChat() {
    setMessages([]);
    setActiveCitations([]);
    setEvidenceOpen(false);
    setInput(defaultQuestion);
  }

  const repoMeta = useMemo(() => {
    return [
      { label: "仓库 ID", value: repo?.id ? `#${repo.id}` : "--" },
      { label: "扫描文件", value: repo?.file_count || 0 },
      { label: "代码分片", value: repo?.chunk_count || 0 },
      { label: "索引耗时", value: formatDuration(repo?.index_duration_ms || 0) }
    ];
  }, [repo]);

  return (
    <div className="app" data-theme={theme}>
      <aside className="rail">
        <button className="rail-brand" onClick={() => setAboutOpen(true)} title="项目介绍">
          <Sparkles size={22} />
        </button>

        <nav className="rail-nav" aria-label="主要功能">
          <button onClick={newChat} title="新对话">
            <Plus size={21} />
            <span>新建</span>
          </button>
          <button className={repoPopoverOpen ? "active" : ""} onClick={() => setRepoPopoverOpen((open) => !open)} title="仓库">
            <GitBranch size={21} />
            <span>仓库</span>
          </button>
          <button className={evidenceOpen ? "active" : ""} onClick={() => setEvidenceOpen(!evidenceOpen)} title="代码依据">
            <PanelRightOpen size={21} />
            <span>依据</span>
          </button>
        </nav>

        <button className="theme-toggle rail-theme" onClick={() => setTheme(theme === "light" ? "dark" : "light")} title="切换主题">
          {theme === "light" ? <Moon size={19} /> : <Sun size={19} />}
        </button>
      </aside>

      <main className="chat-shell">
        <header className="chat-header">
          <div className="repo-menu-anchor" ref={repoMenuRef}>
            <button className="repo-chip" onClick={() => setRepoPopoverOpen((open) => !open)}>
              <GitBranch size={16} />
              <span>{repoName(repoURL)}</span>
              <em className={`mini-status status-${currentStatus}`}>{renderStatus(currentStatus)}</em>
            </button>
            {repoPopoverOpen && (
              <RepoPopover
                repoURL={repoURL}
                setRepoURL={setRepoURL}
                repo={repo}
                currentStatus={currentStatus}
                isIndexing={isIndexing}
                busy={busy}
                statusMessage={statusMessage}
                repoMeta={repoMeta}
                onIndex={indexRepository}
                onRefresh={() => refreshRepo()}
                onClose={() => setRepoPopoverOpen(false)}
              />
            )}
          </div>

          <div className="header-title">
            <h2>代码仓库 RAG 助手</h2>
          </div>

          <div className="header-actions">
            <button className="evidence-toggle" onClick={() => setEvidenceOpen(!evidenceOpen)}>
              <PanelRightOpen size={16} />
              代码依据 {activeCitations.length > 0 ? activeCitations.length : ""}
            </button>
            <button className="theme-toggle header-theme" onClick={() => setTheme(theme === "light" ? "dark" : "light")} title="切换主题">
              {theme === "light" ? <Moon size={17} /> : <Sun size={17} />}
            </button>
          </div>
        </header>

        <section className="message-list">
          <div className="message-frame">
            {messages.length === 0 ? (
              <WelcomeState
                canAsk={canAsk}
                onOpenRepo={() => setRepoPopoverOpen(true)}
                onPick={(question) => setInput(question)}
              />
            ) : (
              messages.map((message) => (
                <MessageBubble
                  key={message.id}
                  message={message}
                  onShowCitations={() => showCitations(message.citations, message.type)}
                />
              ))
            )}
            {busy && (
              <div className="message-row assistant-row">
                <div className="avatar assistant-avatar"><Bot size={17} /></div>
                <div className="message assistant-message loading-message">
                  <span className="dots"><i /><i /><i /></span>
                  {pendingIntent === "impact" ? "正在解析 diff 并分析影响" : "正在检索代码并生成回答"}
                </div>
              </div>
            )}
            <div ref={messageEndRef} />
          </div>
        </section>

        <footer className="composer-wrap">
          {!canAsk && (
            <button className="composer-hint" onClick={() => setRepoPopoverOpen(true)}>
              <GitBranch size={15} />
              请先完成仓库索引
            </button>
          )}
          <div className="composer">
            <textarea
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="询问代码逻辑，或粘贴 git diff 分析影响..."
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  submitMessage();
                }
              }}
            />
            <button onClick={submitMessage} disabled={busy || !input.trim() || !canAsk}>
              <SendHorizontal size={18} />
            </button>
          </div>
        </footer>
      </main>

      <EvidenceDrawer
        citations={activeCitations}
        intent={lastIntent}
        open={evidenceOpen}
        size={evidenceSize}
        onSizeChange={setEvidenceSize}
        onClose={() => setEvidenceOpen(false)}
      />

      {aboutOpen && <AboutDialog onClose={() => setAboutOpen(false)} />}
    </div>
  );
}

function AboutDialog({ onClose }) {
  return (
    <div className="about-layer">
      <button className="about-scrim" onClick={onClose} aria-label="关闭项目介绍" />
      <section className="about-dialog">
        <header>
          <div className="about-mark"><Sparkles size={24} /></div>
          <div>
            <span className="eyebrow">Code RAG Assistant</span>
            <h2>代码仓库 RAG 助手</h2>
            <p>导入 GitHub 仓库后，系统会扫描代码、切分代码片段、生成 embedding 并写入 PostgreSQL + pgvector。</p>
          </div>
          <button className="icon-button" onClick={onClose} title="关闭">
            <X size={18} />
          </button>
        </header>
        <div className="about-grid">
          <div>
            <strong>代码问答</strong>
            <span>根据问题检索相关代码片段，再生成带代码依据的中文回答。</span>
          </div>
          <div>
            <strong>影响分析</strong>
            <span>粘贴 git diff 后自动分析影响模块、风险点和建议测试。</span>
          </div>
          <div>
            <strong>可追溯依据</strong>
            <span>每次回答都可以打开右侧代码依据，查看命中的文件、行号和代码内容。</span>
          </div>
        </div>
      </section>
    </div>
  );
}

function RepoPopover({
  repoURL,
  setRepoURL,
  repo,
  currentStatus,
  isIndexing,
  busy,
  statusMessage,
  repoMeta,
  onIndex,
  onRefresh,
  onClose
}) {
  return (
    <div className="repo-popover">
      <div className="repo-popover-head">
        <div>
          <span className="eyebrow">Repository</span>
          <h1>仓库上下文</h1>
          <p>导入 GitHub 仓库后，系统会分片、向量化并写入 pgvector。</p>
        </div>
        <button className="icon-button" onClick={onClose} title="关闭仓库面板">
          <X size={18} />
        </button>
      </div>

      <div className="repo-control">
        <label className="field-label">GitHub 仓库地址</label>
        <input
          className="repo-input"
          value={repoURL}
          onChange={(e) => setRepoURL(e.target.value)}
          placeholder="https://github.com/owner/repo"
        />
        <div className="side-actions">
          <button className="primary-action" onClick={onIndex} disabled={busy || isIndexing}>
            {isIndexing ? <Loader2 className="spin" size={16} /> : <SearchCode size={16} />}
            {isIndexing ? "索引中" : repo?.id ? "重新索引" : "开始索引"}
          </button>
          <button className="ghost-action" onClick={onRefresh} disabled={!repo?.id}>
            <RefreshCcw size={16} />刷新
          </button>
        </div>
      </div>

      <div className="repo-status-block">
        <div className={`status-pill status-${currentStatus}`}>
          {statusIcon(currentStatus)}
          {renderStatus(currentStatus)}
        </div>
        <p className="status-message">{statusMessage}</p>
      </div>

      <IndexStepper status={currentStatus} />

      <div className="metric-grid">
        {repoMeta.map((item) => (
          <div className="metric" key={item.label}>
            <strong>{item.value}</strong>
            <span>{item.label}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function MessageBubble({ message, onShowCitations }) {
  const isUser = message.role === "user";
  const intentLabel = message.type === "impact" ? "影响分析" : "代码问答";
  return (
    <div className={`message-row ${isUser ? "user-row" : "assistant-row"}`}>
      {!isUser && <div className="avatar assistant-avatar"><Bot size={17} /></div>}
      <div className={`message ${isUser ? "user-message" : "assistant-message"}`}>
        {!isUser && <span className="message-label">{intentLabel}</span>}
        <RichText content={message.content} />
        {!isUser && message.citations?.length > 0 && (
          <button className="citation-link" onClick={onShowCitations}>
            <FileCode2 size={14} />
            查看 {message.citations.length} 个代码依据
          </button>
        )}
      </div>
      {isUser && <div className="avatar user-avatar"><UserRound size={17} /></div>}
    </div>
  );
}

function WelcomeState({ onPick, onOpenRepo, canAsk }) {
  return (
    <div className="welcome">
      <div className="welcome-icon"><Code2 size={30} /></div>
      <h2>{canAsk ? "开始询问代码逻辑" : "先导入仓库，再开始代码问答"}</h2>
      <p>普通问题会走代码问答；粘贴 git diff 会自动进入变更影响分析。代码依据可以从右上角打开查看。</p>
      <div className="rag-flow">
        <span>GitHub Repo</span>
        <i>→</i>
        <span>Code Chunk</span>
        <i>→</i>
        <span>Embedding</span>
        <i>→</i>
        <span>pgvector</span>
        <i>→</i>
        <span>Answer</span>
      </div>
      <div className="starter-grid">
        {!canAsk && <button className="primary-starter" onClick={onOpenRepo}>打开仓库索引</button>}
        {starterPrompts.map((question) => (
          <button key={question} onClick={() => onPick(question)}>{question.split("\n")[0]}</button>
        ))}
      </div>
    </div>
  );
}

function RichText({ content }) {
  const lines = String(content || "").split("\n");
  return (
    <div className="message-content">
      {lines.map((line, index) => {
        if (!line.trim()) return <br key={index} />;
        if (line.startsWith("## ")) return <h3 key={index}>{line.replace(/^##\s+/, "")}</h3>;
        if (line.startsWith("- ")) return <p className="bullet-line" key={index}>{line}</p>;
        return <p key={index}>{line}</p>;
      })}
    </div>
  );
}

function IndexStepper({ status }) {
  const state = status || "idle";
  const activeIndex = state === "idle" ? -1 : state === "pending" ? 0 : state === "indexing" ? 1 : 2;
  return (
    <div className="stepper">
      {indexSteps.map((step, index) => {
        const done = state === "ready" || (state === "indexing" && index < activeIndex);
        const active = (state === "pending" && index === 0) || (state === "indexing" && index === activeIndex);
        const failed = state === "failed" && index === activeIndex;
        return (
          <div className={`step ${done ? "done" : ""} ${active ? "active" : ""} ${failed ? "failed" : ""}`} key={step}>
            <span>{done ? <CheckCircle2 size={12} /> : active ? <Loader2 className="spin" size={12} /> : failed ? <AlertCircle size={12} /> : <Clock3 size={12} />}</span>
            <em>{step}</em>
          </div>
        );
      })}
    </div>
  );
}

function EvidenceDrawer({ citations, intent, open, size, onSizeChange, onClose }) {
  const fullscreen = size === "fullscreen";
  const [selectedIndex, setSelectedIndex] = useState(0);
  const [previewItem, setPreviewItem] = useState(null);

  useEffect(() => {
    setSelectedIndex(0);
    setPreviewItem(null);
  }, [citations]);

  return (
    <>
      <button className={`drawer-scrim ${open ? "open" : ""}`} onClick={onClose} aria-label="关闭代码依据" />
      <aside className={`evidence-drawer ${open ? "open" : ""} evidence-${size}`}>
        <div className="evidence-header">
          <div>
            <span className="eyebrow">{intent === "impact" ? "Impact Evidence" : "RAG Evidence"}</span>
            <h2>{intent === "impact" ? "影响依据" : "代码依据"}</h2>
            <p>当前回答的 {citations.length} 个片段</p>
          </div>
          <div className="evidence-actions">
            <button className="text-button" onClick={() => onSizeChange(size === "wide" ? "normal" : "wide")} title={size === "wide" ? "恢复普通宽度" : "加宽查看"}>
              {size === "wide" ? <Minimize2 size={16} /> : <PanelRightOpen size={16} />}
              {size === "wide" ? "普通" : "加宽"}
            </button>
            <button className="text-button strong" onClick={() => onSizeChange(fullscreen ? "normal" : "fullscreen")} title={fullscreen ? "退出全屏" : "全屏查看"}>
              {fullscreen ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
              {fullscreen ? "退出" : "全屏"}
            </button>
            <button className="icon-button" onClick={onClose} title="关闭代码依据">
              <X size={18} />
            </button>
          </div>
        </div>
        <div className="evidence-list">
          {citations.length === 0 && (
            <div className="empty-evidence">
              <FileCode2 size={28} />
              <p>暂无代码依据。完成一次问答或影响分析后，这里会展示检索命中的代码片段。</p>
            </div>
          )}
          {citations.map((item, index) => (
            <CitationCard
              item={item}
              index={index}
              selected={index === selectedIndex}
              onSelect={() => {
                setSelectedIndex(index);
                setPreviewItem(item);
              }}
              key={`${item.file_path}-${item.start_line}-${index}`}
            />
          ))}
        </div>
      </aside>
      {previewItem && (
        <CodePreviewModal item={previewItem} onClose={() => setPreviewItem(null)} />
      )}
    </>
  );
}

function CodePreviewModal({ item, onClose }) {
  return (
    <div className="code-preview-layer">
      <button className="code-preview-scrim" onClick={onClose} aria-label="关闭代码片预览" />
      <section className="code-preview">
        <header className="code-preview-head">
          <div>
            <span>代码片段预览</span>
            <h3>{item.symbol_name || "未命名代码片段"}</h3>
            <p>{item.file_path}:{item.start_line}-{item.end_line}</p>
          </div>
          <button className="icon-button" onClick={onClose} title="关闭预览">
            <X size={18} />
          </button>
        </header>
        <CodeBlock content={item.content} startLine={item.start_line} />
      </section>
    </div>
  );
}

function CitationCard({ item, index, selected, onSelect }) {
  const [expanded, setExpanded] = useState(index === 0);
  const lines = String(item.content || "").split("\n");
  const showFull = expanded;
  const visibleLines = showFull ? lines : lines.slice(0, 12);
  return (
    <article className={`evidence-item ${selected ? "selected" : ""}`}>
      <button className="evidence-summary" onClick={onSelect}>
        <div>
          {selected && <small>当前选中片段</small>}
          <strong>{item.symbol_name || "未命名代码片段"}</strong>
          <span>{item.file_path}:{item.start_line}-{item.end_line}</span>
        </div>
        <em>{formatScore(item.score)}</em>
      </button>
      <div className="code-wrap">
        <CodeBlock content={visibleLines.join("\n")} startLine={item.start_line} />
      </div>
      {lines.length > 12 && !selected && (
        <button className="expand-code" onClick={() => setExpanded(!expanded)}>
          {expanded ? "收起代码" : `展开剩余 ${lines.length - 12} 行`}
        </button>
      )}
    </article>
  );
}

function CodeBlock({ content, startLine = 1 }) {
  return (
    <div className="code-block">
      {String(content || "").split("\n").map((line, index) => (
        <span className="code-line" key={`${startLine}-${index}`}>
          <span className="code-line-no">{startLine + index}</span>
          <span className="code-line-code">{highlightCodeLine(line)}</span>
        </span>
      ))}
    </div>
  );
}

function highlightCodeLine(line) {
  const keywords = new Set([
    "break", "case", "chan", "const", "continue", "default", "defer", "else", "fallthrough",
    "for", "func", "go", "goto", "if", "import", "interface", "map", "package", "range",
    "return", "select", "struct", "switch", "type", "var"
  ]);
  const builtins = new Set([
    "any", "bool", "byte", "comparable", "complex64", "complex128", "error", "float32", "float64",
    "int", "int8", "int16", "int32", "int64", "rune", "string", "uint", "uint8", "uint16",
    "uint32", "uint64", "uintptr", "nil", "true", "false", "make", "new", "append", "len",
    "cap", "copy", "delete", "panic", "recover"
  ]);
  const tokenPattern = /(\/\/.*|"(?:\\.|[^"\\])*"|`[^`]*`|'(?:\\.|[^'\\])*'|\b\d+(?:\.\d+)?\b|\b[A-Za-z_]\w*\b|[:=+\-*/%<>!&|.^~]+|[{}()[\],.;])/g;
  const value = String(line || "");
  const nodes = [];
  let lastIndex = 0;
  let match;

  while ((match = tokenPattern.exec(value)) !== null) {
    const token = match[0];
    if (match.index > lastIndex) {
      nodes.push(value.slice(lastIndex, match.index));
    }

    const rest = value.slice(match.index + token.length);
    if (token.startsWith("//")) {
      nodes.push(<span className="tok-comment" key={nodes.length}>{token}</span>);
    } else if (/^["'`]/.test(token)) {
      nodes.push(<span className="tok-string" key={nodes.length}>{token}</span>);
    } else if (/^\d/.test(token)) {
      nodes.push(<span className="tok-number" key={nodes.length}>{token}</span>);
    } else if (keywords.has(token)) {
      nodes.push(<span className="tok-keyword" key={nodes.length}>{token}</span>);
    } else if (builtins.has(token)) {
      nodes.push(<span className="tok-type" key={nodes.length}>{token}</span>);
    } else if (/^[A-Za-z_]\w*$/.test(token) && rest.trimStart().startsWith("(")) {
      nodes.push(<span className="tok-func" key={nodes.length}>{token}</span>);
    } else if (/^[:=+\-*/%<>!&|.^~]+$/.test(token)) {
      nodes.push(<span className="tok-operator" key={nodes.length}>{token}</span>);
    } else {
      nodes.push(token);
    }
    lastIndex = match.index + token.length;
  }

  if (lastIndex < value.length) {
    nodes.push(value.slice(lastIndex));
  }

  return nodes.map((node, index) => {
    if (typeof node === "string") return <React.Fragment key={index}>{node}</React.Fragment>;
    return React.cloneElement(node, { key: index });
  });
}

function detectIntent(text) {
  const value = String(text || "").trim();
  if (
    value.includes("diff --git") ||
    value.includes("\n@@") ||
    value.includes("@@ ") ||
    value.includes("--- a/") ||
    value.includes("+++ b/") ||
    /^index [0-9a-f]{7,}\.\.[0-9a-f]{7,}/m.test(value)
  ) {
    return "impact";
  }
  return "ask";
}

function renderStatus(status) {
  return statusText[status] || status;
}

function statusIcon(status) {
  if (status === "ready") return <CheckCircle2 size={15} />;
  if (status === "failed") return <AlertCircle size={15} />;
  if (status === "pending" || status === "indexing") return <Loader2 className="spin" size={15} />;
  return <Clock3 size={15} />;
}

function repoName(url) {
  const value = String(url || "").replace(/\/$/, "");
  const parts = value.split("/");
  if (parts.length >= 2) return parts.slice(-2).join("/");
  return "选择仓库";
}

function formatDuration(ms) {
  if (!ms) return "0s";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function formatScore(score) {
  if (typeof score !== "number" || Number.isNaN(score)) return "--";
  return score.toFixed(3);
}

function formatImpact(data) {
  const lines = [
    "## 变更总结",
    data.summary || "暂无总结。",
    "",
    "## 影响模块",
    listText(data.impacted_modules),
    "",
    "## 风险点",
    listText(data.risks),
    "",
    "## 建议测试",
    listText(data.suggested_tests)
  ];
  return lines.join("\n");
}

function listText(items) {
  if (!items || items.length === 0) return "暂无。";
  return items.map((item) => `- ${item}`).join("\n");
}

createRoot(document.getElementById("root")).render(<App />);
