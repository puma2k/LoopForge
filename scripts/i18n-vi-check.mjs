#!/usr/bin/env node
/**
 * Check web/i18n-vi.js for leftover CJK, empty values, English/Vietnamese
 * word-salad, and known calque patterns. Exit non-zero when issues remain.
 *
 * Usage: node scripts/i18n-vi-check.mjs
 */
import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createContext, runInContext } from "node:vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(process.env.LF_REPO || join(__dirname, ".."));

const HAN = /[\u3400-\u9fff]/;
const CJK_PUNCT = /[\u3000-\u303f\uff01-\uff60]/;
const ASCII_WORD = /\b[A-Za-z][A-Za-z'’\-]*\b/g;
const VI_LETTER =
  /[ÀÁẢÃẠĂẰẮẲẴẶÂẦẤẨẪẬÈÉẺẼẸÊỀẾỂỄỆÌÍỈĨỊÒÓỎÕỌÔỒỐỔỖỘƠỜỚỞỠỢÙÚỦŨỤƯỪỨỬỮỰỲÝỶỸỴĐàáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵđ]/;
const UNICODE_LETTER = /\p{L}/u;

const CALQUES = [
  "đến này",
  "trong này dự án",
  "trên này",
  "Này là",
  "quay lại đến",
  "Mới mật khẩu",
  "đầu tiên bước",
  "tại đã chọn",
  "là đang chờ",
  "và làm không",
  "đến tại",
  "tại nhiều nhất",
  "tại thời gian",
  "Thêm đến ",
  "Avg ",
  "avg ",
  "e.g.",
  "backfill",
  "bởi avg",
];

/** Always report — even if mistakenly added to the allowlist. */
const BLACKLIST = [
  "Asking", "Writing", "returned", "destructive", "Don't", "ended", "now",
  "your", "the", "to", "of", "this", "that", "please", "create", "account",
  "request", "understanding", "prefer", "fall", "roll", "back", "currently",
  "regenerate", "repeatedly", "interval", "placeable", "answers", "questions",
  "block", "start", "at", "in", "on", "is", "are", "and", "or", "with", "for",
  "from", "by", "not", "no", "yes", "new", "old", "first", "last", "next",
  "failed", "success", "loading", "saving", "retry", "cancel", "delete",
  "edit", "save", "open", "close", "run", "runs", "running", "step", "steps",
  "node", "nodes", "result", "results", "error", "errors", "user", "users",
  "project", "projects", "settings",
].map((w) => w.toLowerCase());
const BLACKLIST_SET = new Set(BLACKLIST);

/**
 * Proper nouns / product terms that may stay in English.
 * Extend only for genuine terms — never ordinary English filler.
 */
const ALLOWLIST = [
  // Required by task
  "LoopForge", "API", "Key", "Keys", "MCP", "Git", "GitHub", "GitLab", "SSH",
  "HTTPS", "HTTP", "token", "tokens", "Token", "model", "models", "Agent",
  "Agents", "SOP", "SOPs", "Docker", "Dockerfile", "PR", "PRs", "MR", "MRs",
  "CodeBuddy", "Cursor", "Codex", "Claude", "Code", "OpenCode", "Kimi", "Pi",
  "DeepSeek", "Gemini", "Antigravity", "ZCode", "Harness", "CLI", "Bot",
  "Bots", "Webhook", "WeCom", "TAPD", "Skill", "Skills", "Rule", "Rules",
  "Playwright", "Chromium", "Markdown", "JSON", "YAML", "URL", "ID", "IDE",
  "VS", "OAuth", "LLM", "AI", "Prompt", "prompt", "sandbox", "Sandbox",
  "workspace", "Workspace", "branch", "commit", "push", "merge", "pull",
  "fetch", "diff", "patch", "repo", "Pipeline", "pipeline", "runner", "Runner",
  "Studio", "Loop", "Prometheus", "Grafana", "OpenTelemetry", "OTLP", "MySQL",
  "Postgres", "Redis", "MinIO", "S3", "Linux", "macOS", "Windows", "Mac",
  "Node", "Python", "Go", "Shell", "Bash", "npm", "Vite", "React",
  "Kubernetes", "k8s", "cron", "Cron", "UTC", "OK", "VI", "EN", "Tencent",
  "Gongfeng", "CNB", "Feishu", "Slack", "Discord", "Telegram", "Email",
  "email", "Chrome", "Firefox", "Safari", "Monaco", "xterm", "iframe",
  "emoji", "tab", "Tab", "Enter", "Esc", "Ctrl", "Cmd", "Shift", "Alt",
  "P50", "P90", "P95", "P99", "QPS", "RPS", "KB", "MB", "GB", "ms", "CPU",
  "GPU", "RAM", "v",
  // Extra genuine product / protocol / UI nouns used as labels
  "Main", "Trace", "Traces", "Canvas", "OpenAI", "iOA", "Owner", "Cloud",
  "KPI", "PDF", "CSV", "SQL", "HTML", "CSS", "JS", "TS", "SDK", "JWT", "SSL",
  "TLS", "PEM", "OpenSSH", "Bug", "Issue", "Issues", "CDN", "DNS", "IP", "UI",
  "UX", "QA", "CI", "CD", "SHA", "HMAC", "TB", "TODO", "GPT", "ECharts", "XML",
  "SVG", "PNG", "JPEG", "JPG", "GIF", "WebSocket", "SSE", "REST", "RPC", "gRPC",
  "Nginx", "Apache", "Ubuntu", "Debian", "Alpine", "PHP", "Java", "Kotlin",
  "Swift", "Ruby", "Rust", "TypeScript", "JavaScript", "Kafka", "MongoDB",
  "Elasticsearch", "AWS", "GCP", "Azure", "Webhooks", "Compose", "Podman",
  "Helm", "Terraform", "Ansible", "Jenkins", "Jira", "Notion", "Figma",
  "Sentry", "Datadog", "Jaeger", "Zipkin", "Loki", "Tempo", "PostgreSQL",
  "MariaDB", "SQLite", "Anthropic", "Ollama", "Copilot", "ChatGPT", "SSO",
  "OIDC", "LDAP", "RBAC", "TTL", "UUID", "UTF", "ASCII", "Unicode", "Regex",
  "regex", "Schema", "schema", "DOM", "PWA", "SPA", "VPN", "SMTP", "SFTP",
  "SSD", "HDD", "NVMe", "USB", "KVM", "QEMU", "VMware", "Pulumi", "Django",
  "Flask", "FastAPI", "axios", "wget", "curl", "jq", "sed", "awk", "grep",
  "ssh", "scp", "rsync", "tmux", "vim", "nvim", "sudo", "kubectl", "pip",
  "cargo", "gcc", "make", "cmake", "yarn", "pnpm", "bun", "deno", "conda",
  "poetry", "nvm", "maven", "gradle", "eslint", "prettier", "jest", "vitest",
  "cypress", "puppeteer", "README", "LICENSE", "CHANGELOG", "AGENTS",
  "CONTRIBUTING", "SECURITY", "FAQ", "WIP", "RFC", "ADR", "MVP", "POC", "PoC",
  "LTS", "GA", "RC", "alpha", "beta", "stable", "latest", "main", "master",
  "develop", "hotfix", "release", "tag", "tags", "fork", "upstream", "origin",
  "remote", "clone", "stash", "rebase", "blame", "log", "status", "checkout",
  "reset", "revert", "squash", "HEAD", "base64", "gzip", "tar", "toml", "ini",
  "env", "yml", "webp", "ico", "woff", "mp4", "webm", "WebRTC", "WebGL",
  "CAPTCHA", "CSRF", "XSS", "SSRF", "RCE", "DoS", "DDoS", "WAF", "CVE",
  "OWASP", "GDPR", "i18n", "l10n", "a11y", "RTL", "LTR", "DHCP", "NTP",
  "BusyBox", "musl", "glibc", "clang", "apk", "deb", "rpm", "msi", "exe",
  "dmg", "snap", "flatpak", "venv", "webpack", "rollup", "babel", "swc",
  "prisma", "drizzle", "mongoose", "ioredis", "bullmq", "RabbitMQ", "NATS",
  "Homebrew", "apt", "yum", "brew", "zsh", "fish", "powershell", "systemd",
  "journalctl", "htop", "SELinux", "AppArmor", "eBPF", "BPF", "Hy", "CNY",
  "USD", "Prompts", "Artifact", "Artifacts", "Playbook", "Playbooks",
  "Workbench", "Dashboard", "Dashboards", "Board", "Templates", "Designer",
  "Metrics", "Usage", "Reports", "Domains", "Connect", "Runners",
  "Environments", "Sandboxes", "Capabilities", "Roles", "Flows", "Delivery",
  "Insights", "Build", "Work", "Platform", "Personal", "Extensions",
  "Knowledge", "Assets", "Access", "Management", "Overview", "Profile",
  "Accounts", "Guide", "Docs", "Help", "About", "Feedback", "Support",
  "Health", "Ready", "Live", "Dark", "Light", "Theme", "Locale", "Timezone",
  "Language", "Avg", "Max", "Min", "Sum", "Count", "Marketplace", "Backlog",
  "Workflow", "Workflows", "Timeout", "Product", "Hook", "Hooks", "Executor",
  "Secret", "Secrets", "Logs", "Worker", "Workers", "Domain", "Ticket",
  "Tickets", "metadata", "daemon", "heartbeat", "proxy", "clipboard", "alias",
  "Baseline", "baseline", "Rework", "rework", "Trend", "Role", "Brief", "Plan",
  "Coding", "GLM", "EPP", "Palworld", "MCPs", "agentflow", "stdout", "stderr",
  "stdin", "localhost", "hostname", "payload", "header", "headers", "cookie",
  "cookies", "session", "Bearer", "Basic", "scope", "scopes", "repository",
  "repositories", "subdir", "monorepo", "workdir", "cache", "Cache",
  "frontend", "backend", "DevOps", "SRE", "SLA", "SLO", "latency",
  "throughput", "Throughput", "bandwidth", "script", "scripts", "Script",
  "Scripts", "preview", "Preview", "demo", "Demo", "mock", "snapshot",
  "coverage", "Coverage", "lint", "review", "Review", "approvals", "Approvals",
  "comment", "Comments", "thread", "Thread", "Threads", "host", "Host",
  "Hosts", "image", "Image", "Images", "container", "Container", "Containers",
  "volume", "Volume", "network", "Network", "ingress", "Ingress", "service",
  "Service", "Services", "endpoint", "Endpoint", "Endpoints", "route", "Route",
  "Routes", "router", "Router", "middleware", "Middleware", "handler",
  "Handler", "plugin", "Plugin", "Plugins", "extension", "Extension",
  "module", "Module", "Modules", "package", "Package", "Packages", "library",
  "Library", "framework", "Framework", "runtime", "Runtime", "toolchain",
  "sdk", "GUI", "TUI", "site", "Site", "Sites", "portal", "Portal", "console",
  "Console", "terminal", "Terminal", "pty", "PTY", "tty", "TTY", "stdio",
  "test", "tests", "Test", "Tests", "e2e", "E2E", "unit", "Unit",
  "integration", "Integration", "smoke", "Smoke", "regression", "Regression",
  "canary", "Canary", "deploy", "Deploy", "deployment", "Deployment",
  "Release", "Releases", "Hotfix", "Patch", "Patches", "Changelog", "ok",
  "vs", "etc", "eg", "vd", "avg", "max", "min", "sum", "id", "idx", "uuid",
  "guid", "url", "uri", "urn", "http", "https", "ws", "wss", "tcp", "udp",
  "quic", "grpc", "json", "yaml", "yml", "toml", "xml", "html", "css", "scss",
  "sql", "nosql", "NoSQL", "graphql", "GraphQL", "md", "txt", "bin", "zip",
  "tar", "gz", "png", "jpg", "jpeg", "gif", "svg", "webp", "ico", "mp3",
  "mp4", "webm", "wav", "ogg", "pdf", "doc", "docx", "xls", "xlsx", "ppt",
  "pptx", "csv", "tsv", "woff", "woff2", "ttf", "otf", "Server", "Client",
  "Gateway", "Base", "online", "offline", "webhook", "webhooks", "Maintainer",
  "Admin", "Captain", "Crew", "Room", "sub-SOP", "Sub-SOP", "send-back",
  "Legacy", "China", "gmr", "alice", "Control", "plane", "Attribution",
  "Software", "Completeness", "Repos", "fine-grained", "credentials",
  "happy-path", "serialize", "instructions", "revision", "backfill",
  "marketplace", "gh", "glab", "classic", "transport", "span", "Responses",
  "Messages", "bcrypt", "Gitea", "native", "Vibe", "seatbelt", "bwrap",
  "Enterprise", "Matcher", "Google", "Del", "Gate", "thinking", "Device",
  "Flow", "OCI", "OTEL", "SkillHub", "PAT", "App", "done", "filesystem",
  "tmpfs", "telemetry", "polling", "Prebuild", "scheduler", "guard",
  "resolve", "ours", "theirs", "hash", "Swimlane", "iteration", "abort",
  "tools", "payments", "root", "Directory", "Working", "medium", "low",
  "none", "high", "Header", "Instance", "English", "Chinese", "Vietnamese", "production", "web", "Contents", "object", "servers", "array", "static", "name", "title", "description", "edges", "output", "task", "provider", "Pull", "group", "OCI", "SkillHub", "OTEL",
  "xxx", // placeholder example token in docs
].map((w) => w.toLowerCase());

/**
 * Vietnamese tokens that are pure ASCII (no diacritics) and must not be
 * treated as English. Not an English allowlist.
 */
const VI_ASCII = [
  "theo", "trong", "cho", "khi", "sau", "sang", "giao", "trang", "chi",
  "xem", "vai", "tin", "ghi", "chung", "thi", "tra", "sao", "ra", "truy",
  "qua", "gian", "kho", "thu", "xong", "danh", "quy", "chia", "ngay",
  "thay", "tri", "tham", "do", "dung", "quay", "sung", "quan", "minh",
  "song", "khai", "thao", "hay", "cung", "nhanh", "nuoc", "con", "xung",
  "roi", "lai", "nay", "cac", "mot", "nhung", "nhieu", "duoc", "viec",
  "them", "hoac", "nhu", "vao", "tu", "cua", "va", "de", "co", "se", "da",
  "la", "bao", "cao", "cap", "dat", "gap", "giu", "goi", "gui", "hoc",
  "hop", "huong", "kem", "ket", "kieu", "lam", "lay", "lien", "loai",
  "luc", "luon", "luong", "mat", "mau", "may", "moc", "moi", "muc", "nam",
  "nen", "ngoai", "nguoi", "nhan", "nhap", "nhat", "nhom", "noi", "phan",
  "phien", "phim", "phu", "phuc", "rut", "sach", "sai", "san", "sat",
  "sen", "so", "sua", "tach", "tam", "tang", "tao", "tat", "thanh",
  "thich", "thoi", "thong", "thuc", "tiep", "tim", "tinh", "toan", "toc",
  "toi", "ton", "tru", "truoc", "tuc", "tung", "tuy", "uoc", "van", "vay",
  "ve", "vi", "von", "vung", "vuot", "xac", "xay", "xoa", "xu", "xuat",
  "xuong", "yeu", "ban", "bang", "bat", "ben", "bi", "bien", "biet",
  "binh", "bo", "buoc", "ca", "cai", "cam", "can", "cang", "cat", "cau",
  "cay", "chan", "chap", "chat", "che", "chen", "chinh", "choi", "chon",
  "chu", "chua", "chuc", "chuyen", "cong", "cot", "cu", "cuoc", "cuoi",
  "cuon", "dai", "dam", "dan", "dang", "dao", "dau", "day", "dem", "den",
  "dep", "di", "dia", "dich", "dieu", "doc", "doi", "don", "dong", "dot",
  "du", "dua", "duc", "dun", "duoi", "duong", "duyet", "em", "ep", "gai",
  "gan", "gao", "gat", "gay", "ghe", "gia", "giam", "gieo", "gio", "gioi",
  "giua", "giup", "go", "goc", "gom", "gon", "got", "ha", "hai", "ham",
  "han", "hang", "hao", "hap", "hat", "he", "hen", "hep", "het", "hieu",
  "hinh", "ho", "hoa", "hoai", "hoan", "hoang", "hoat", "hoi", "hom",
  "hon", "hong", "hot", "hua", "hue", "hung", "huu", "huy", "huyen",
  "ich", "ke", "ken", "keo", "kep", "kha", "khac", "khach", "kham",
  "khan", "khang", "khao", "khat", "khe", "khen", "kheo", "khi", "khich",
  "khiem", "khien", "khiep", "kho", "khoa", "khoan", "khoang", "khoc",
  "khoi", "khon", "khong", "khop", "khu", "khuc", "khung", "khuon",
  "khuong", "khuy", "khuyen", "ki", "kich", "kiem", "kien", "kiep",
  "kiet", "kim", "kin", "kinh", "kip", "ky", "lac", "lai", "lan", "lang",
  "lanh", "lao", "lap", "lat", "lau", "le", "len", "leo", "li", "lich",
  "liem", "liep", "liet", "lieu", "lin", "linh", "lo", "loa", "loan",
  "loc", "loi", "lom", "lon", "long", "lop", "lot", "lu", "lua", "luc",
  "lui", "lung", "luoc", "luoi", "luom", "luot", "luu", "luy", "luyen",
  "ly", "ma", "mac", "mai", "mam", "man", "mang", "manh", "mao", "map",
  "me", "mem", "men", "meo", "met", "mi", "mien", "mieu", "mim", "mo",
  "moc", "moi", "mom", "mon", "mong", "mot", "mu", "mua", "muc", "mui",
  "mun", "mung", "muoi", "muon", "muong", "my", "na", "nai", "nan",
  "nang", "nao", "nap", "nat", "ne", "nem", "neo", "nep", "net", "nga",
  "ngai", "ngam", "ngan", "ngang", "ngao", "ngap", "ngat", "ngay",
  "nghe", "nghi", "nghia", "nghich", "nghiem", "nghien", "nghiep",
  "nghiet", "nghin", "nghinh", "ngo", "ngoa", "ngoai", "ngoan", "ngoc",
  "ngoi", "ngon", "ngong", "ngot", "ngu", "ngua", "nguc", "ngung",
  "nguoc", "nguoi", "nguon", "nguy", "nguyen", "nha", "nhac", "nhai",
  "nham", "nhan", "nhang", "nhao", "nhap", "nhat", "nhau", "nhay", "nhe",
  "nheo", "nhet", "nhi", "nhiem", "nhien", "nhiep", "nhiet", "nhieu",
  "nhim", "nhin", "nhinh", "nhip", "nho", "nhoc", "nhoi", "nhom", "nhon",
  "nhong", "nhot", "nhu", "nhuc", "nhung", "nhuoc", "nhuom", "nhuong",
  "nhuy", "nhuyen", "ni", "niem", "nien", "nip", "no", "noi", "nom",
  "non", "nong", "nop", "nu", "nua", "nuc", "nui", "nung", "nuoc", "nuoi",
  "nuong", "nuot", "o", "oc", "oi", "om", "on", "ong", "op", "ot", "pha",
  "phac", "phai", "pham", "phan", "phang", "phao", "phap", "phat", "phau",
  "phay", "phe", "phen", "phep", "phet", "phi", "phich", "phien", "phiep",
  "phiet", "phieu", "phim", "phin", "phinh", "pho", "phoa", "phoi",
  "phom", "phon", "phong", "phot", "phu", "phuc", "phun", "phung",
  "phuoc", "phuong", "phuy", "phuyen", "qa", "qu", "qua", "quai", "quan",
  "quang", "quat", "quay", "que", "quen", "quet", "qui", "quoc", "quy",
  "quyen", "quyet", "ra", "rac", "rai", "ram", "ran", "rang", "rao",
  "rap", "rat", "rau", "ray", "re", "ren", "reo", "ret", "ri", "ria",
  "rich", "riet", "rieu", "rim", "rin", "rinh", "ro", "roi", "rom", "ron",
  "rong", "rot", "ru", "rua", "ruc", "rui", "rum", "run", "rung", "ruoc",
  "ruoi", "ruong", "ruot", "ruou", "ruy", "sa", "sac", "sai", "sam", "san",
  "sang", "sao", "sap", "sat", "sau", "say", "se", "sen", "set", "si",
  "sich", "sieu", "sim", "sin", "sinh", "so", "soa", "soan", "soat",
  "soc", "soi", "som", "son", "song", "sot", "su", "sua", "suc", "sui",
  "sum", "sun", "sung", "suoi", "suon", "suong", "suot", "suy", "ta",
  "tac", "tai", "tam", "tan", "tang", "tao", "tap", "tat", "tau", "tay",
  "te", "tem", "ten", "teo", "tep", "tet", "tha", "thac", "thai", "tham",
  "than", "thang", "thanh", "thao", "thap", "that", "thau", "thay", "the",
  "them", "then", "theo", "thep", "thi", "thia", "thich", "thiem",
  "thien", "thiep", "thiet", "thieu", "thin", "thinh", "tho", "thoa",
  "thoai", "thoat", "thoc", "thoi", "thom", "thon", "thong", "thot",
  "thu", "thua", "thuc", "thue", "thui", "thun", "thung", "thuoc",
  "thuong", "thuy", "thuyen", "ti", "tia", "tich", "tiem", "tien", "tiep",
  "tiet", "tieu", "tim", "tin", "tinh", "to", "toa", "toan", "toc", "toi",
  "tom", "ton", "tong", "tot", "tra", "trac", "trai", "tram", "tran",
  "trang", "trao", "trat", "trau", "tre", "trem", "tren", "treo", "tri",
  "trich", "trien", "triet", "trieu", "trim", "trin", "trinh", "tro",
  "troi", "trom", "tron", "trong", "trot", "tru", "trua", "truc", "trui",
  "trum", "trun", "trung", "truoc", "truong", "truot", "truy", "truyen",
  "tu", "tua", "tuc", "tui", "tum", "tun", "tung", "tuoc", "tuoi", "tuon",
  "tuong", "tuot", "tuy", "tuyen", "tuyet", "ty", "u", "ua", "uc", "ui",
  "um", "un", "ung", "uoc", "uoi", "uon", "uong", "uot", "uu", "uy",
  "uyen", "uyet", "va", "vac", "vai", "vam", "van", "vang", "vao", "vap",
  "vat", "vay", "ve", "vem", "ven", "veo", "vet", "vi", "via", "viem",
  "vien", "viet", "viec", "vim", "vin", "vinh", "vo", "voc", "voi", "vom",
  "von", "vong", "vot", "vu", "vua", "vuc", "vui", "vun", "vung", "vuot",
  "vuong", "vy", "xa", "xac", "xai", "xam", "xan", "xang", "xao", "xap",
  "xat", "xau", "xay", "xe", "xem", "xen", "xeo", "xep", "xet", "xi",
  "xich", "xim", "xin", "xinh", "xiu", "xo", "xoa", "xoai", "xoan", "xoc",
  "xoi", "xom", "xon", "xong", "xot", "xu", "xua", "xuc", "xui", "xum",
  "xun", "xung", "xuoi", "xuong", "xuy", "xuyen", "y", "yeu",
  "giai", "ba", "duy", "cha", "ranh", "traqua", "modeldanh",
  "an", "Anh", "ghim", "Ghim",
].map((w) => w.toLowerCase());

const ALLOWLIST_SET = new Set(ALLOWLIST);
const VI_ASCII_SET = new Set(VI_ASCII);
for (const w of BLACKLIST_SET) ALLOWLIST_SET.delete(w);

function loadDict(relPath, globalName) {
  const code = readFileSync(join(REPO, relPath), "utf8");
  const ctx = createContext({ window: {} });
  runInContext(code, ctx, { filename: relPath });
  const dict = ctx.window[globalName];
  if (!dict || typeof dict !== "object") {
    throw new Error(`Failed to load ${globalName} from ${relPath}`);
  }
  return dict;
}

function maskIgnored(value) {
  let s = value;
  s = s.replace(/\$\{[^}]*\}/g, " ");
  s = s.replace(/\{[^}]*\}/g, " ");
  s = s.replace(/%[0-9]*[sdif]/g, " ");
  s = s.replace(/`[^`]*`/g, " ");
  s = s.replace(/https?:\/\/\S+/gi, " ");
  s = s.replace(/\b(?:mailto|ssh|git):\/\/\S+/gi, " ");
  s = s.replace(/(?:^|[\s(])(?:\.?\.?\/|~\/)[\w./-]+/g, " ");
  s = s.replace(/\b[\w-]+\.[A-Za-z0-9._-]{1,24}\b/g, " ");
  // kebab-case technical ids / package paths (generic-backend, feature/my-feature)
  s = s.replace(/\b[A-Za-z][A-Za-z0-9]*(?:-[A-Za-z0-9]+)+\b/g, " ");
  s = s.replace(/\b[A-Za-z]+(?:_[A-Za-z0-9]+)+\b/g, " ");
  s = s.replace(/\b[a-z]+(?:[A-Z][a-z0-9]*)+\b/g, " ");
  s = s.replace(/\b[A-Z][a-z]+(?:[A-Z][a-z0-9]*)+\b/g, " ");
  // HTML attribute fragments commonly embedded in dictionary values
  s = s.replace(/\b(?:rel|src|href|target|class|style|type|name|id|role|aria-[a-z-]+)\s*=\s*(?:"[^"]*"|'[^']*'|"|"|'|')/gi, " ");
  // Shell one-liners kept as command examples
  s = s.replace(/\bsh\s+-c\s+'[^']*'/g, " ");
  s = s.replace(/\bprintf\s+'[^']*'/g, " ");
  return s;
}

function isAllCapsShort(word) {
  return /^[A-Z]{1,6}$/.test(word);
}

function isUnicodeLetter(ch) {
  if (!ch) return false;
  return (
    VI_LETTER.test(ch) ||
    (UNICODE_LETTER.test(ch) && !/[A-Za-z]/.test(ch))
  );
}

function extractAsciiWords(value) {
  const masked = maskIgnored(value);
  const out = [];
  ASCII_WORD.lastIndex = 0;
  let m;
  while ((m = ASCII_WORD.exec(masked)) !== null) {
    const word = m[0];
    const start = m.index;
    const end = start + word.length;
    if (isUnicodeLetter(masked[start - 1]) || isUnicodeLetter(masked[end])) {
      continue;
    }
    out.push(word);
  }
  return out;
}

function checkValue(_key, value) {
  const issues = [];
  if (typeof value !== "string") {
    issues.push({ type: "non-string", detail: typeof value });
    return issues;
  }
  if (!value.trim()) {
    issues.push({ type: "empty", detail: "empty value" });
    return issues;
  }
  if (HAN.test(value)) {
    issues.push({ type: "han", detail: "contains Han characters" });
  }
  if (CJK_PUNCT.test(value)) {
    issues.push({ type: "cjk-punct", detail: "contains CJK punctuation" });
  }
  for (const calque of CALQUES) {
    if (value.includes(calque)) {
      issues.push({ type: "calque", detail: calque });
    }
  }
  for (const word of extractAsciiWords(value)) {
    const lower = word.toLowerCase();
    if (BLACKLIST_SET.has(lower)) {
      issues.push({ type: "blacklist", detail: word });
      continue;
    }
    if (isAllCapsShort(word)) continue;
    if (ALLOWLIST_SET.has(lower)) continue;
    if (VI_ASCII_SET.has(lower)) continue;
    if (word.length === 1) continue;
    issues.push({ type: "english", detail: word });
  }
  return issues;
}

function main() {
  const vi = loadDict("web/i18n-vi.js", "LF_I18N_VI");
  loadDict("web/i18n-en.js", "LF_I18N_EN");

  const errors = [];
  for (const [key, value] of Object.entries(vi)) {
    for (const issue of checkValue(key, value)) {
      errors.push({ key, value, ...issue });
    }
  }

  if (errors.length === 0) {
    console.log("0 mixed entries");
    process.exit(0);
  }

  console.error(`${errors.length} mixed entries`);
  const byType = {};
  for (const e of errors) {
    byType[e.type] = (byType[e.type] || 0) + 1;
  }
  console.error("by type:", JSON.stringify(byType));

  const words = {};
  for (const e of errors) {
    if (e.type === "english" || e.type === "blacklist") {
      words[e.detail] = (words[e.detail] || 0) + 1;
    }
  }
  console.error(
    "top words:",
    Object.entries(words)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 50)
      .map(([w, n]) => `${w}:${n}`)
      .join(", "),
  );

  const limit = Number(process.env.LF_I18N_SHOW || 80);
  for (const e of errors.slice(0, limit)) {
    console.error(
      `- [${e.type}] ${e.detail}\n  key: ${JSON.stringify(e.key)}\n  val: ${JSON.stringify(e.value)}`,
    );
  }
  if (errors.length > limit) {
    console.error(`… and ${errors.length - limit} more`);
  }
  process.exit(1);
}

main();
