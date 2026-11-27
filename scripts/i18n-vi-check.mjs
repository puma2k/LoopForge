#!/usr/bin/env node
/**
 * Check web/i18n-vi.js for leftover CJK, empty values, English/Vietnamese
 * word-salad, glued syllables, bad placeholders, and known calques.
 * Exit non-zero when issues remain.
 *
 * Usage: node scripts/i18n-vi-check.mjs
 * Test helpers: import { checkValue } from "./i18n-vi-check.mjs"
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
const TONE_MARKED =
  /[àáảãạằắẳẵặầấẩẫậèéẻẽẹềếểễệìíỉĩịòóỏõọồốổỗộờớởỡợùúủũụừứửữựỳýỷỹỵÀÁẢÃẠẰẮẲẴẶẦẤẨẪẬÈÉẺẼẸỀẾỂỄỆÌÍỈĨỊÒÓỎÕỌỒỐỔỖỘỜỚỞỠỢÙÚỦŨỤỪỨỬỮỰỲÝỶỸỴ]/;
const UNICODE_LETTER = /\p{L}/u;
const LETTER_RUN = /\p{L}+/gu;

/** Must appear exactly in this casing. Lowercase / mixed → acronym-case. */
const ACRONYMS = new Set([
  "REST",
  "API",
  "SSH",
  "MCP",
  "SOP",
  "PR",
  "MR",
  "CLI",
  "URL",
  "JSON",
  "YAML",
  "HTTP",
  "HTTPS",
  "ID",
  "AI",
  "LLM",
  "UTC",
  "CPU",
  "GPU",
  "RAM",
]);

const CALQUES = [
  "đầu tiên dòng",
  "đầu tiên bước",
  "đầu tiên tài khoản",
  "rest là",
  "là trống",
  "Này là",
  "đến này",
  "trong này",
  "trên này",
  "quay lại đến",
  "Mới mật khẩu",
  "tại đã chọn",
  "là đang chờ",
  "và làm không",
  "có không khả dụng",
  "và khác nâng cao",
  "không đã tìm thấy",
  "Mới nhất SOP",
  "Không khác dự án",
];

/** Calques matched with a trailing boundary so "không thể làm" is not hit. */
const CALQUE_RES = [
  {
    re: /không thể là(?=$|\s|[.,;:!?…])/g,
    detail: "không thể là",
  },
  {
    re: /Này .+ có không/g,
    detail: "Này … có không",
  },
  {
    re: /là trong$/g,
    detail: "là trong$",
  },
];

/** Always report — even if mistakenly added to the allowlist. */
const BLACKLIST = [
  "Asking",
  "Writing",
  "returned",
  "destructive",
  "Don't",
  "ended",
  "now",
  "your",
  "the",
  "to",
  "of",
  "this",
  "that",
  "please",
  "create",
  "account",
  "request",
  "understanding",
  "prefer",
  "fall",
  "roll",
  "back",
  "currently",
  "regenerate",
  "repeatedly",
  "interval",
  "placeable",
  "answers",
  "questions",
  "block",
  "start",
  "at",
  "in",
  "on",
  "is",
  "are",
  "and",
  "or",
  "with",
  "for",
  "from",
  "by",
  "not",
  "no",
  "yes",
  "new",
  "old",
  "first",
  "last",
  "next",
  "failed",
  "success",
  "loading",
  "saving",
  "retry",
  "cancel",
  "delete",
  "edit",
  "save",
  "open",
  "close",
  "run",
  "runs",
  "running",
  "step",
  "steps",
  "node",
  "nodes",
  "result",
  "results",
  "error",
  "errors",
  "user",
  "users",
  "project",
  "projects",
  "settings",
  // Round-4 additions
  "rest",
  "Avg",
  "title",
  "description",
  "done",
  "object",
  "Contents",
].map((w) => w.toLowerCase());
const BLACKLIST_SET = new Set(BLACKLIST);

/**
 * Small allowlist: real product / technical terms only.
 * Ordinary English filler must NOT appear here.
 */
const ALLOWLIST_EXACT = [
  "LoopForge",
  "GitHub",
  "GitLab",
  "Git",
  "Docker",
  "Dockerfile",
  "CodeBuddy",
  "Cursor",
  "Codex",
  "Claude",
  "OpenCode",
  "Kimi",
  "DeepSeek",
  "Gemini",
  "Antigravity",
  "ZCode",
  "Pi",
  "Playwright",
  "Chromium",
  "Markdown",
  "OAuth",
  "WeCom",
  "TAPD",
  "Webhook",
  "Agent",
  "Skill",
  "Rule",
  "Bot",
  "Token",
  "token",
  "Prompt",
  "prompt",
  "sandbox",
  "workspace",
  "commit",
  "push",
  "merge",
  "branch",
  "diff",
  "patch",
  "repo",
  "Pipeline",
  "runner",
  "Studio",
  "MySQL",
  "Postgres",
  "Redis",
  "MinIO",
  "Linux",
  "macOS",
  "Windows",
  "Node",
  "Python",
  "Go",
  "Shell",
  "Bash",
  "npm",
  "React",
  "Kubernetes",
  "cron",
  "OK",
  "VI",
  "EN",
  "Tencent",
  "Gongfeng",
  "CNB",
  "Slack",
  "Discord",
  "Telegram",
  "Email",
  "Chrome",
  "Monaco",
  "xterm",
  "iframe",
  "emoji",
  "Enter",
  "Esc",
  "Ctrl",
  "Cmd",
  "Shift",
  "Alt",
  "P50",
  "P90",
  "P95",
  "P99",
  "KB",
  "MB",
  "GB",
  "ms",
  "Issue",
  "Artifact",
  "artifact",
  "Artifacts",
  // Product / protocol nouns used as labels in LoopForge UI
  "Hook",
  "hook",
  "Hooks",
  "hooks",
  "PAT",
  "IDE",
  "Loop",
  "agentflow",
  "worker",
  "Worker",
  "proxy",
  "zip",
  "TB",
  "PATH",
  "alice",
  "web",
  "submit_gate_result",
  "Harness",
  "harness",
  "checkout",
  "fetch",
  "lint",
  "HTML",
  "CSV",
  "KPI",
  "OCI",
  "OTEL",
  "SSO",
  "CI",
  "HEAD",
  "bcrypt",
  "gh",
  "glab",
  "md",
  "xxx",
  // Credential / product nouns used throughout the UI
  "Key",
  "Keys",
  "Code",
  "Plan",
  "Feishu",
  "SQLite",
  "PHP",
  "Anthropic",
  "Messages",
  "Bearer",
  "OTLP",
  "TTL",
  "XHR",
  "axios",
  "origin",
  "master",
  "HOME",
  "TMPDIR",
  "UID",
  "GID",
  "CVM",
  "EPP",
  "Palworld",
  "PM",
  "ts",
  "gmr",
  "production",
  "bin",
  "cd",
  "curl",
  "cookie",
  "servers",
  "mcpServers",
  "instructions",
  "revision",
  "span",
  "sendBeacon",
  "WebSocket",
  "OpenAI",
  "iOA",
  "ck_",
  "docs",
  "Captain",
  "captain",
  "QTA",
  "GPT",
  "Gitea",
  "PEM",
  "GLM",
  "Vibe",
  "CNY",
  "seatbelt",
  "bwrap",
  "AK",
  "SK",
  "Enterprise",
  "Google",
  "Del",
  "png",
  "jpeg",
  "gif",
  "webp",
  "admin",
  "RFC",
  "stdio",
  "Gate",
  "rebase",
  "pdf",
  "OpenAPI",
  // Common plural / variant forms of the same technical terms
  "Agents",
  "Skills",
  "Rules",
  "Bots",
  "Tokens",
  "Webhooks",
  "SOPs",
  "PRs",
  "MRs",
  "APIs",
  "IDs",
  "LLMs",
  "URLs",
  "Prompts",
  "runners",
  "workspaces",
  "sandbox",
  "Sandbox",
  "Workspace",
  "Pipeline",
  "pipelines",
  "commits",
  "branches",
  "repos",
  "patches",
  "diffs",
  // Proper nouns / protocol labels kept in Chinese UI copy
  "VS",
  "localhost",
  "container",
  "Checkers",
  "Completions",
  "ultracode",
  "AppKey",
  "COPY",
  "telemetry",
  "toolchain",
  "CorpId",
  "AppID",
  "Guild",
  "Tab",
  "IM",
  "Avatar",
  "USD",
  "Workflow",
  "PostgreSQL",
  "Terminal",
  "IP",
  "IPv6",
  "BigModel",
  "Integrations",
  "Keys",
  "Messages",
  "Anthropic",
  // Product / protocol nouns used as UI labels (not filler English)
  "App",
  "Client",
  "runtime",
  "baseline",
  "Base", // Base URL
  "tools", // tools/list MCP method
  "list",
  "native", // native sandbox
  "uv",
  "robot",
  "Secret",
  "ding", // DingTalk CorpId prefix
  "viewport",
  "Console",
  "Iteration",
  "overview",
  "logs",
  "demo",
];

const ALLOWLIST_SET = new Set(ALLOWLIST_EXACT);
const ALLOWLIST_LOWER = new Set(
  ALLOWLIST_EXACT.filter((w) => !ACRONYMS.has(w.toUpperCase()) || w !== w.toUpperCase()).map(
    (w) => w.toLowerCase(),
  ),
);
// Keep exact forms for case-sensitive product names; lower lookup for the rest
// except acronyms which are handled separately.
for (const w of ALLOWLIST_EXACT) {
  if (!ACRONYMS.has(w)) ALLOWLIST_LOWER.add(w.toLowerCase());
}
for (const w of BLACKLIST_SET) {
  ALLOWLIST_SET.delete(w);
  ALLOWLIST_LOWER.delete(w);
}

/**
 * Vietnamese tokens that are pure ASCII (no diacritics) and must not be
 * treated as English. Not an English allowlist.
 */
const VI_ASCII = [
  "theo",
  "trong",
  "cho",
  "khi",
  "sau",
  "sang",
  "giao",
  "trang",
  "chi",
  "xem",
  "vai",
  "tin",
  "ghi",
  "chung",
  "thi",
  "tra",
  "sao",
  "ra",
  "truy",
  "qua",
  "gian",
  "kho",
  "thu",
  "xong",
  "danh",
  "doanh",
  "xoay",
  "quanh",
  "im",
  "bay",
  "bong",
  "chai",
  "coi",
  "quy",
  "chia",
  "ngay",
  "thay",
  "tri",
  "tham",
  "do",
  "dung",
  "quay",
  "sung",
  "quan",
  "minh",
  "song",
  "khai",
  "thao",
  "hay",
  "cung",
  "nhanh",
  "nuoc",
  "con",
  "xung",
  "roi",
  "lai",
  "nay",
  "cac",
  "mot",
  "nhung",
  "nhieu",
  "duoc",
  "viec",
  "them",
  "hoac",
  "nhu",
  "vao",
  "tu",
  "cua",
  "va",
  "de",
  "co",
  "se",
  "da",
  "la",
  "bao",
  "cao",
  "cap",
  "dat",
  "gap",
  "giu",
  "goi",
  "gui",
  "hoc",
  "hop",
  "huong",
  "kem",
  "ket",
  "kieu",
  "lam",
  "lay",
  "lien",
  "loai",
  "luc",
  "luon",
  "luong",
  "mat",
  "mau",
  "may",
  "moc",
  "moi",
  "muc",
  "nam",
  "nen",
  "ngoai",
  "nguoi",
  "nhan",
  "nhap",
  "nhat",
  "nhom",
  "noi",
  "phan",
  "phien",
  "phim",
  "phu",
  "phuc",
  "rut",
  "sach",
  "sai",
  "san",
  "sat",
  "sen",
  "so",
  "sua",
  "tach",
  "tam",
  "tang",
  "tao",
  "tat",
  "thanh",
  "thich",
  "thoi",
  "thong",
  "thuc",
  "tiep",
  "tim",
  "tinh",
  "toan",
  "toc",
  "toi",
  "ton",
  "tru",
  "truoc",
  "tuc",
  "tung",
  "tuy",
  "uoc",
  "van",
  "vay",
  "ve",
  "vi",
  "von",
  "vung",
  "vuot",
  "xac",
  "xay",
  "xoa",
  "xu",
  "xuat",
  "xuong",
  "yeu",
  "ban",
  "bang",
  "bat",
  "ben",
  "bi",
  "bien",
  "biet",
  "binh",
  "bo",
  "buoc",
  "ca",
  "cai",
  "cam",
  "can",
  "cang",
  "cat",
  "cau",
  "cay",
  "chan",
  "chap",
  "chat",
  "che",
  "chen",
  "chinh",
  "choi",
  "chon",
  "chu",
  "chua",
  "chuc",
  "chuyen",
  "cong",
  "cot",
  "cu",
  "cuoc",
  "cuoi",
  "cuon",
  "dai",
  "dam",
  "dan",
  "dang",
  "dao",
  "dau",
  "day",
  "dem",
  "den",
  "dep",
  "di",
  "dia",
  "dich",
  "dieu",
  "doc",
  "doi",
  "don",
  "dong",
  "dot",
  "du",
  "dua",
  "duc",
  "dun",
  "duoi",
  "duong",
  "duyet",
  "em",
  "ep",
  "gai",
  "gan",
  "gao",
  "gat",
  "gay",
  "ghe",
  "gia",
  "giam",
  "gieo",
  "gio",
  "gioi",
  "giua",
  "giup",
  "go",
  "goc",
  "gom",
  "gon",
  "got",
  "ha",
  "hai",
  "ham",
  "han",
  "hang",
  "hao",
  "hap",
  "hat",
  "he",
  "hen",
  "hep",
  "het",
  "hieu",
  "hinh",
  "ho",
  "hoa",
  "hoai",
  "hoan",
  "hoang",
  "hoat",
  "hoi",
  "hom",
  "hon",
  "hong",
  "hot",
  "hua",
  "hue",
  "hung",
  "huu",
  "huy",
  "huyen",
  "ich",
  "ke",
  "ken",
  "keo",
  "kep",
  "kha",
  "khac",
  "khach",
  "kham",
  "khan",
  "khang",
  "khao",
  "khat",
  "khe",
  "khen",
  "kheo",
  "khi",
  "khich",
  "khiem",
  "khien",
  "khiep",
  "kho",
  "khoa",
  "khoan",
  "khoang",
  "khoc",
  "khoi",
  "khon",
  "khong",
  "khop",
  "khu",
  "khuc",
  "khung",
  "khuon",
  "khuong",
  "khuy",
  "khuyen",
  "ki",
  "kich",
  "kiem",
  "kien",
  "kiep",
  "kiet",
  "kim",
  "kin",
  "kinh",
  "kip",
  "ky",
  "lac",
  "lai",
  "lan",
  "lang",
  "lanh",
  "lao",
  "lap",
  "lat",
  "lau",
  "le",
  "len",
  "leo",
  "li",
  "lich",
  "liem",
  "liep",
  "liet",
  "lieu",
  "lin",
  "linh",
  "lo",
  "loa",
  "loan",
  "loc",
  "loi",
  "lom",
  "lon",
  "long",
  "lop",
  "lot",
  "lu",
  "lua",
  "luc",
  "lui",
  "lung",
  "luoc",
  "luoi",
  "luom",
  "luot",
  "luu",
  "luy",
  "luyen",
  "ly",
  "ma",
  "mac",
  "mai",
  "mam",
  "man",
  "mang",
  "manh",
  "mao",
  "map",
  "me",
  "mem",
  "men",
  "meo",
  "met",
  "mi",
  "mien",
  "mieu",
  "mim",
  "mo",
  "moc",
  "moi",
  "mom",
  "mon",
  "mong",
  "mot",
  "mu",
  "mua",
  "muc",
  "mui",
  "mun",
  "mung",
  "muoi",
  "muon",
  "muong",
  "my",
  "na",
  "nai",
  "nan",
  "nang",
  "nao",
  "nap",
  "nat",
  "ne",
  "nem",
  "neo",
  "nep",
  "net",
  "nga",
  "ngai",
  "ngam",
  "ngan",
  "ngang",
  "ngao",
  "ngap",
  "ngat",
  "ngay",
  "nghe",
  "nghi",
  "nghia",
  "nghich",
  "nghiem",
  "nghien",
  "nghiep",
  "nghiet",
  "nghin",
  "nghinh",
  "ngo",
  "ngoa",
  "ngoai",
  "ngoan",
  "ngoc",
  "ngoi",
  "ngon",
  "ngong",
  "ngot",
  "ngu",
  "ngua",
  "nguc",
  "ngung",
  "nguoc",
  "nguoi",
  "nguon",
  "nguy",
  "nguyen",
  "nha",
  "nhac",
  "nhai",
  "nham",
  "nhan",
  "nhang",
  "nhao",
  "nhap",
  "nhat",
  "nhau",
  "nhay",
  "nhe",
  "nheo",
  "nhet",
  "nhi",
  "nhiem",
  "nhien",
  "nhiep",
  "nhiet",
  "nhieu",
  "nhim",
  "nhin",
  "nhinh",
  "nhip",
  "nho",
  "nhoc",
  "nhoi",
  "nhom",
  "nhon",
  "nhong",
  "nhot",
  "nhu",
  "nhuc",
  "nhung",
  "nhuoc",
  "nhuom",
  "nhuong",
  "nhuy",
  "nhuyen",
  "ni",
  "niem",
  "nien",
  "nip",
  "no",
  "noi",
  "nom",
  "non",
  "nong",
  "nop",
  "nu",
  "nua",
  "nuc",
  "nui",
  "nung",
  "nuoc",
  "nuoi",
  "nuong",
  "nuot",
  "o",
  "oc",
  "oi",
  "om",
  "on",
  "ong",
  "op",
  "ot",
  "pha",
  "phac",
  "phai",
  "pham",
  "phan",
  "phang",
  "phao",
  "phap",
  "phat",
  "phau",
  "phay",
  "phe",
  "phen",
  "phep",
  "phet",
  "phi",
  "phich",
  "phien",
  "phiep",
  "phiet",
  "phieu",
  "phim",
  "phin",
  "phinh",
  "pho",
  "phoa",
  "phoi",
  "phom",
  "phon",
  "phong",
  "phot",
  "phu",
  "phuc",
  "phun",
  "phung",
  "phuoc",
  "phuong",
  "phuy",
  "phuyen",
  "qua",
  "quai",
  "quan",
  "quang",
  "quat",
  "quay",
  "que",
  "quen",
  "quet",
  "qui",
  "quoc",
  "quy",
  "quyen",
  "quyet",
  "ra",
  "rac",
  "rai",
  "ram",
  "ran",
  "rang",
  "rao",
  "rap",
  "rat",
  "rau",
  "ray",
  "re",
  "ren",
  "reo",
  "ret",
  "ri",
  "ria",
  "rich",
  "riet",
  "rieu",
  "rim",
  "rin",
  "rinh",
  "ro",
  "roi",
  "rom",
  "ron",
  "rong",
  "rot",
  "ru",
  "rua",
  "ruc",
  "rui",
  "rum",
  "run",
  "rung",
  "ruoc",
  "ruoi",
  "ruong",
  "ruot",
  "ruou",
  "ruy",
  "sa",
  "sac",
  "sai",
  "sam",
  "san",
  "sang",
  "sao",
  "sap",
  "sat",
  "sau",
  "say",
  "se",
  "sen",
  "set",
  "si",
  "sich",
  "sieu",
  "sim",
  "sin",
  "sinh",
  "so",
  "soa",
  "soan",
  "soat",
  "soc",
  "soi",
  "som",
  "son",
  "song",
  "sot",
  "su",
  "sua",
  "suc",
  "sui",
  "sum",
  "sun",
  "sung",
  "suoi",
  "suon",
  "suong",
  "suot",
  "suy",
  "ta",
  "tac",
  "tai",
  "tam",
  "tan",
  "tang",
  "tao",
  "tap",
  "tat",
  "tau",
  "tay",
  "te",
  "tem",
  "ten",
  "teo",
  "tep",
  "tet",
  "tha",
  "thac",
  "thai",
  "tham",
  "than",
  "thang",
  "thanh",
  "thao",
  "thap",
  "that",
  "thau",
  "thay",
  "the",
  "them",
  "then",
  "theo",
  "thep",
  "thi",
  "thia",
  "thich",
  "thiem",
  "thien",
  "thiep",
  "thiet",
  "thieu",
  "thin",
  "thinh",
  "tho",
  "thoa",
  "thoai",
  "thoat",
  "thoc",
  "thoi",
  "thom",
  "thon",
  "thong",
  "thot",
  "thu",
  "thua",
  "thuc",
  "thue",
  "thui",
  "thun",
  "thung",
  "thuoc",
  "thuong",
  "thuy",
  "thuyen",
  "ti",
  "tia",
  "tich",
  "tiem",
  "tien",
  "tiep",
  "tiet",
  "tieu",
  "tim",
  "tin",
  "tinh",
  "to",
  "toa",
  "toan",
  "toc",
  "toi",
  "tom",
  "ton",
  "tong",
  "tot",
  "tra",
  "trac",
  "trai",
  "tram",
  "tran",
  "trang",
  "trao",
  "trat",
  "trau",
  "tre",
  "trem",
  "tren",
  "treo",
  "tri",
  "trich",
  "trien",
  "triet",
  "trieu",
  "trim",
  "trin",
  "trinh",
  "tro",
  "troi",
  "trom",
  "tron",
  "trong",
  "trot",
  "tru",
  "trua",
  "truc",
  "trui",
  "trum",
  "trun",
  "trung",
  "truoc",
  "truong",
  "truot",
  "truy",
  "truyen",
  "tu",
  "tua",
  "tuc",
  "tui",
  "tum",
  "tun",
  "tung",
  "tuoc",
  "tuoi",
  "tuon",
  "tuong",
  "tuot",
  "tuy",
  "tuyen",
  "tuyet",
  "ty",
  "u",
  "ua",
  "uc",
  "ui",
  "um",
  "un",
  "ung",
  "uoc",
  "uoi",
  "uon",
  "uong",
  "uot",
  "uu",
  "uy",
  "uyen",
  "uyet",
  "va",
  "vac",
  "vai",
  "vam",
  "van",
  "vang",
  "vao",
  "vap",
  "vat",
  "vay",
  "ve",
  "vem",
  "ven",
  "veo",
  "vet",
  "vi",
  "via",
  "viem",
  "vien",
  "viet",
  "viec",
  "vim",
  "vin",
  "vinh",
  "vo",
  "voc",
  "voi",
  "vom",
  "von",
  "vong",
  "vot",
  "vu",
  "vua",
  "vuc",
  "vui",
  "vun",
  "vung",
  "vuot",
  "vuong",
  "vy",
  "xa",
  "xac",
  "xai",
  "xam",
  "xan",
  "xang",
  "xao",
  "xap",
  "xat",
  "xau",
  "xay",
  "xe",
  "xem",
  "xen",
  "xeo",
  "xep",
  "xet",
  "xi",
  "xich",
  "xim",
  "xin",
  "xinh",
  "xiu",
  "xo",
  "xoa",
  "xoai",
  "xoan",
  "xoc",
  "xoi",
  "xom",
  "xon",
  "xong",
  "xot",
  "xu",
  "xua",
  "xuc",
  "xui",
  "xum",
  "xun",
  "xung",
  "xuoi",
  "xuong",
  "xuy",
  "xuyen",
  "y",
  "yeu",
  "giai",
  "ba",
  "duy",
  "cha",
  "ranh",
  "an",
  "anh",
  "ghim",
].map((w) => w.toLowerCase());
const VI_ASCII_SET = new Set(VI_ASCII);

// Vietnamese syllable: onset + nucleus (+ coda). At most one tone per syllable.
const VI_ONSETS = [
  "ngh",
  "ng",
  "nh",
  "gh",
  "gi",
  "kh",
  "ph",
  "th",
  "tr",
  "ch",
  "qu",
  "b",
  "c",
  "d",
  "đ",
  "g",
  "h",
  "k",
  "l",
  "m",
  "n",
  "p",
  "q",
  "r",
  "s",
  "t",
  "v",
  "x",
  "",
];
const VI_CODAS = ["nh", "ng", "ch", "c", "m", "n", "p", "t", ""];
/** Tone-stripped nuclei (ăâêôơư kept via precomposed forms after NFD strip of tone only). */
const VI_NUCLEI = new Set([
  "a",
  "ă",
  "â",
  "e",
  "ê",
  "i",
  "o",
  "ô",
  "ơ",
  "u",
  "ư",
  "y",
  "ai",
  "ao",
  "au",
  "ay",
  "âu",
  "ây",
  "eo",
  "êu",
  "ia",
  "iê",
  "iu",
  "oa",
  "oă",
  "oe",
  "oi",
  "oo",
  "ôi",
  "ơi",
  "ua",
  "uâ",
  "uê",
  "ui",
  "uo",
  "uô",
  "ươi",
  "ưa",
  "ươ",
  "ưu",
  "uy",
  "uya",
  "uyu",
  "uyê",
  "yê",
  "iêu",
  "yêu",
  "uôi",
  "ươu",
  "oai",
  "oao",
  "oay",
  "uai",
  "uay",
  "oeo",
  "uya",
  "uyu",
  "ia",
  "iê",
  "yê",
  "ưu",
  "ươ",
  "ưa",
]);

function stripToneKeepAmbi(s) {
  // Remove tone marks only (grave/acute/hook/tilde/dot). Keep breve,
  // circumflex, and horn so ăâêôơư stay distinct from aeiou.
  return s
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300\u0301\u0303\u0309\u0323]/g, "")
    .normalize("NFC");
}

function countToneMarks(word) {
  let n = 0;
  for (const ch of word.normalize("NFD")) {
    if (
      ch === "\u0300" ||
      ch === "\u0301" ||
      ch === "\u0303" ||
      ch === "\u0309" ||
      ch === "\u0323"
    ) {
      n += 1;
    }
  }
  return n;
}

function isViSyllable(syll) {
  if (!syll || syll.length > 9) return false;
  const s = syll.toLowerCase();
  // One syllable carries at most one tone.
  if (countToneMarks(s) > 1) return false;
  const folded = stripToneKeepAmbi(s).replace(/đ/g, "đ");
  for (const onset of VI_ONSETS) {
    if (!folded.startsWith(onset)) continue;
    // Prefer longer onsets (list is longest-first).
    const rest = folded.slice(onset.length);
    for (const coda of VI_CODAS) {
      if (coda && !rest.endsWith(coda)) continue;
      if (!coda && /(?:nh|ng|ch|[cmnpt])$/.test(rest) && rest.length > 1) {
        // Still try empty coda; longer coda attempts come first in list.
      }
      const nucleus = rest.slice(0, rest.length - coda.length);
      if (!nucleus) continue;
      if (VI_NUCLEI.has(nucleus)) return true;
    }
  }
  return false;
}

/** True when word with Vietnamese diacritics looks like ≥2 glued syllables. */
function isGluedViWord(word) {
  if (!VI_LETTER.test(word)) return false;
  if (word.length < 4) return false;
  // Mixed Vietnamese + tech/ASCII token (e.g. tảirepository, phiênCLI).
  // - ANY uppercase acronym run (≥2) next to Vietnamese diacritics
  // - lowercase ASCII run ≥5 (repository, sandbox, …)
  // Ignore short lowercase runs that are undiacritic parts of a syllable
  // (ang in đang, ngo in ngoài, …).
  if (/[A-Z]{2,}/.test(word)) return true;
  const lowerRuns = word.match(/[a-z]+/g) || [];
  if (lowerRuns.some((r) => r.length >= 5)) return true;
  // ≥2 tone-marked vowels in one undivided word → glued.
  if (countToneMarks(word) >= 2) return true;
  // Single valid syllable → OK.
  if (isViSyllable(word)) return false;
  // Short tokens are usually one syllable our table may miss — do not flag.
  if (word.length < 6) return false;
  // Segment into ≥2 valid syllables (whole word not a single syllable).
  const lower = word.toLowerCase();
  return canSegmentAll(lower) && !isViSyllable(lower);
}

function canSegmentAll(word) {
  if (!word) return true;
  if (isViSyllable(word)) return true;
  // Prefer shorter left syllables first (avoids false "cóp"/"giaot" nuclei).
  for (let i = 2; i <= Math.min(word.length - 2, 7); i++) {
    const left = word.slice(0, i);
    const right = word.slice(i);
    if (isViSyllable(left) && canSegmentAll(right)) return true;
  }
  return false;
}

const PLACEHOLDER_RE =
  /(\{\{[^{}]+\}\}|\$\{[^{}]+\}|\{[^{}]+\}|%[0-9]*[sdvwqx])/g;

function extractPlaceholders(text) {
  if (typeof text !== "string") return [];
  const out = [];
  let m;
  PLACEHOLDER_RE.lastIndex = 0;
  while ((m = PLACEHOLDER_RE.exec(text)) !== null) {
    out.push({ raw: m[0], index: m.index, end: m.index + m[0].length });
  }
  return out;
}

function isLetterChar(ch) {
  return ch != null && UNICODE_LETTER.test(ch);
}

function isSpaceOrPunctOrEdge(ch, atEdge) {
  if (atEdge || ch == null || ch === "") return true;
  if (/\s/.test(ch)) return true;
  // Punctuation / symbols (not letters or digits)
  if (!isLetterChar(ch) && !/[0-9]/.test(ch)) return true;
  return false;
}

function checkPlaceholders(value) {
  const issues = [];
  const phs = extractPlaceholders(value);
  for (const ph of phs) {
    const before = ph.index === 0 ? null : value[ph.index - 1];
    const after = ph.end >= value.length ? null : value[ph.end];
    // Letter glued to placeholder on either side.
    if (isLetterChar(before) || isLetterChar(after)) {
      issues.push({ type: "placeholder", detail: ph.raw });
      continue;
    }
    // Colon immediately before placeholder without space → require ": %s".
    if (before === ":") {
      issues.push({ type: "placeholder", detail: `:${ph.raw}` });
    }
  }
  return issues;
}

/**
 * Multiset signature for placeholder parity.
 * Mustache `{{name}}` keeps the exact name (do not collapse to `{{}}`).
 * Other forms keep kind multisets: `%s`/`%d`/`%v`/`%w`, `{x}` → `{}`, `${x}` → `${}`.
 */
function placeholderSignature(text) {
  return extractPlaceholders(text).map((p) => {
    if (p.raw.startsWith("{{")) return p.raw; // exact {{name}}
    // Keep full ${expr} so translating identifiers (kind→loại) fails parity.
    if (p.raw.startsWith("${")) return p.raw;
    if (p.raw.startsWith("{")) return "{}";
    return p.raw.replace(/[0-9]+/, ""); // %s / %d / %v / %w
  });
}

function checkPlaceholderParity(key, value) {
  const k = placeholderSignature(key);
  const v = placeholderSignature(value);
  if (k.length !== v.length) {
    return [{ type: "placeholder", detail: `count key=${k.length} value=${v.length}` }];
  }
  // Compare multiset of kinds / exact mustache names
  const sortJoin = (arr) => [...arr].sort().join(",");
  if (sortJoin(k) !== sortJoin(v)) {
    return [{ type: "placeholder", detail: `mismatch key=[${k}] value=[${v}]` }];
  }
  return [];
}

/** Letter glued to curly / CJK quotes: yêu cầu“ / ”đã / 「…」. */
function checkQuoteGlue(value) {
  const issues = [];
  const pairs = [
    ["\u201c", "\u201d"], // “ ”
    ["\u300c", "\u300d"], // 「 」
  ];
  for (const [open, close] of pairs) {
    for (let i = 0; i < value.length; i++) {
      const ch = value[i];
      if (ch === open) {
        const before = i === 0 ? null : value[i - 1];
        if (isLetterChar(before)) {
          issues.push({ type: "quote", detail: `${before}${open}` });
        }
      } else if (ch === close) {
        const after = i + 1 >= value.length ? null : value[i + 1];
        if (isLetterChar(after)) {
          issues.push({ type: "quote", detail: `${close}${after}` });
        }
      }
    }
  }
  return issues;
}

function maskIgnored(value) {
  let s = value;
  s = s.replace(/\$\{[^}]*\}/g, " ");
  s = s.replace(/\{\{[^}]*\}\}/g, " ");
  s = s.replace(/\{[^}]*\}/g, " ");
  s = s.replace(/%[0-9]*[sdvwqx]/g, " ");
  s = s.replace(/`[^`]*`/g, " ");
  s = s.replace(/https?:\/\/\S*/gi, " ");
  s = s.replace(/\b(?:mailto|ssh|git):\/\/\S+/gi, " ");
  s = s.replace(/(?:^|[\s(])(?:\.?\.?\/|~\/)[\w./-]+/g, " ");
  s = s.replace(/\b[\w-]+\.[A-Za-z0-9._-]{1,24}\b/g, " ");
  s = s.replace(/\b[A-Za-z][A-Za-z0-9]*(?:-[A-Za-z0-9]+)+\b/g, " ");
  s = s.replace(/\b[A-Za-z]+(?:_[A-Za-z0-9]+)+\b/g, " ");
  s = s.replace(/\b[a-z]+(?:[A-Z][a-z0-9]*)+\b/g, " ");
  s = s.replace(/\b[A-Z][a-z]+(?:[A-Z][a-z0-9]*)+\b/g, " ");
  s = s.replace(
    /\b(?:rel|src|href|target|class|style|type|name|id|role|title|aria-[a-z-]+)\s*=\s*(?:"[^"]*"|'[^']*'|"|"|'|')/gi,
    " ",
  );
  // Unclosed HTML attribute fragments used as dict keys (e.g. `title="返回`).
  s = s.replace(
    /\b(?:rel|src|href|target|class|style|type|name|id|role|title|aria-[a-z-]+)\s*=\s*"[^"]*$/gi,
    " ",
  );
  s = s.replace(/\bsh\s+-c\s+'[^']*'/g, " ");
  s = s.replace(/\bprintf\s+'[^']*'/g, " ");
  return s;
}

function isVersionToken(word) {
  return /^v\d+(?:\.\d+)*$/i.test(word);
}

function isUnicodeLetter(ch) {
  if (!ch) return false;
  return VI_LETTER.test(ch) || (UNICODE_LETTER.test(ch) && !/[A-Za-z]/.test(ch));
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
    out.push({ word, start, end, masked });
  }
  return out;
}

function isAllowedTerm(word) {
  if (ACRONYMS.has(word)) return true;
  if (ALLOWLIST_SET.has(word)) return true;
  if (ALLOWLIST_LOWER.has(word.toLowerCase()) && !ACRONYMS.has(word.toUpperCase())) {
    return true;
  }
  if (isVersionToken(word)) return true;
  return false;
}

function checkGlued(value) {
  const issues = [];
  LETTER_RUN.lastIndex = 0;
  let m;
  while ((m = LETTER_RUN.exec(value)) !== null) {
    const word = m[0];
    if (isGluedViWord(word)) {
      issues.push({ type: "dính", detail: word });
    }
  }
  return issues;
}

/** Split syllable: "tạon hánh", "mởp hiên", "cóp hiên", "kỳp hiên". */
const SPLIT_PHIEN_NHANH_RE =
  /(?<![\p{L}])\p{L}*\p{L}[np] (?:hánh|hiên)(?![\p{L}])/gu;

/** Known glued stems from broken syllable joins (also /bảnmô/, /ánmô/). */
const GLUED_KNOWN_RE =
  /bảnmô|ánmô|dự ánmô|(?<![\p{L}])(?:vụp|cóp|tạon|mởn|mởp|lạip|tảip|mớip|cón)(?![\p{L}])/gu;

/**
 * Vietnamese letter glued to a Latin/tech token without a space
 * (tảiworkspace, dùngSOP, trênrunner, …).
 * Negative lookbehind skips English compounds like OpenAPI / GitHub.
 */
const GLUED_LATIN_RE =
  /(?<![A-Za-z])\p{Ll}(?:workspace|Workspace|SOP|runner|Runner|MCP|Git|API|CLI|Token|Docker|Agent|PR|MR)\b/gu;

/**
 * Lowercase Vietnamese (incl. ascii vowel) stuck to an uppercase Latin run
 * inside a VI phrase — e.g. dựánX when X starts with capital (rare) /
 * more commonly [a-zà-ỹđ][A-Z] after diacritics: "môTả" style mistakes.
 * Skip pure camelCase ASCII (already masked) by requiring a VI letter.
 */
const GLUED_VI_CAMEL_RE =
  /[àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵđ][A-Z]/gu;

/**
 * Calque 当前X → "hiện tại X" at ANY position.
 * Correct order is "X hiện tại" and must pass.
 */
const HIEN_TAI_X_RE =
  /(?:^|[^\p{L}])hiện tại (?:phiên|nhánh|dự án|yêu cầu|trang|lần chạy|bản|tác tử|bàn làm việc|SOP|repo|kho)(?![\p{L}])/giu;

function checkSplitSyllables(value) {
  const issues = [];
  SPLIT_PHIEN_NHANH_RE.lastIndex = 0;
  let m;
  while ((m = SPLIT_PHIEN_NHANH_RE.exec(value)) !== null) {
    issues.push({ type: "dính", detail: m[0].trim() });
  }
  GLUED_KNOWN_RE.lastIndex = 0;
  while ((m = GLUED_KNOWN_RE.exec(value)) !== null) {
    issues.push({ type: "dính", detail: m[0] });
  }
  GLUED_LATIN_RE.lastIndex = 0;
  while ((m = GLUED_LATIN_RE.exec(value)) !== null) {
    issues.push({ type: "dính", detail: m[0] });
  }
  GLUED_VI_CAMEL_RE.lastIndex = 0;
  while ((m = GLUED_VI_CAMEL_RE.exec(value)) !== null) {
    issues.push({ type: "dính", detail: m[0] });
  }
  return issues;
}

/**
 * Known bad calques / word-order mistakes for 执行机器 and related keys
 * (review findings — must keep failing).
 */
const KNOWN_BAD_VI_RES = [
  {
    keyIncludes: "项目共享执行机器",
    re: /dự án dùng chung máy thực thi/i,
    detail: "dự án dùng chung máy thực thi",
  },
  {
    keyIncludes: "无法加载工作区执行机器",
    re: /máy thực thi bàn làm việc(?!\s+của)/i,
    detail: "máy thực thi bàn làm việc (missing của)",
  },
  {
    keyIncludes: "使用执行机器的账号模型",
    re: /dùng máy thực thi tài khoản mô hình/i,
    detail: "dùng máy thực thi tài khoản mô hình",
  },
  {
    keyIncludes: "等待云端执行机器",
    re: /trong đám mây runner/i,
    detail: "trong đám mây runner",
  },
];

/**
 * 执行机器 must be "máy thực thi" — never runner / máy chạy.
 * Also catch inverted "máy thực thi <noun>" when the Chinese key ends with
 * 版本 / 状态 / 名称 / 类型 / 列表 (should be "<noun> máy thực thi").
 */
function checkExecMachineTerminology(key, value) {
  const issues = [];
  if (typeof key !== "string" || typeof value !== "string") return issues;

  for (const rule of KNOWN_BAD_VI_RES) {
    if (key.includes(rule.keyIncludes) && rule.re.test(value)) {
      issues.push({ type: "calque", detail: rule.detail });
    }
  }

  if (!key.includes("执行机器")) return issues;
  if (/\brunners?\b/i.test(value) || /máy chạy/i.test(value)) {
    issues.push({ type: "term", detail: "执行机器 must be máy thực thi" });
  }
  // Key like 执行机器版本 / 执行机器状态 / … → value must not start with
  // "máy thực thi phiên bản|trạng thái|…"
  if (/执行机器(?:版本|状态|名称|类型|列表)$/.test(key)) {
    if (
      /^máy thực thi\s+(phiên bản|trạng thái|tên|loại|danh sách)\b/i.test(
        value,
      )
    ) {
      issues.push({
        type: "calque",
        detail: "máy thực thi <noun> word order",
      });
    }
  }
  return issues;
}

/**
 * UI terminology: prefer "bàn làm việc"; ban "Không gian làm việc" and
 * standalone workspace / Workspace (env vars and paths are masked first).
 */
function checkTerminology(value) {
  const issues = [];
  if (/Không gian làm việc/i.test(value)) {
    issues.push({ type: "term", detail: "Không gian làm việc" });
  }
  const masked = maskIgnored(value);
  // Allow trailing punctuation (e.g. "workspace.") but not path segments.
  const wsRe = /(?<![\w./$`])\b[Ww]orkspaces?\b(?![\w/])/g;
  let m;
  while ((m = wsRe.exec(masked)) !== null) {
    issues.push({ type: "term", detail: m[0] });
  }
  return issues;
}

/** Broken EN→VI contractions / word-salad, e.g. "Có thể't xóa". */
const BROKEN_CONTRACTION_RE = /Có thể['’]t\b/g;

/** Translated HTML attribute name that breaks markup. */
const BAD_HTML_ATTR_RE = /tiêu đề\s*=/g;

/** Repeated "bản bản" from over-literal preview/build calques. */
const BAN_BAN_RE = /bản\s+bản\b/giu;

/** docs/*.md paths that must stay byte-identical when present in the key. */
const DOCS_MD_RE = /docs\/[a-z0-9._/-]+\.md/gi;

/** Fixed machine IDs that must not be translated when present in the key. */
const MACHINE_IDS = ["af-preview-build", "af-preview-builder"];

/**
 * Structural / fidelity checks that need the source key.
 */
function checkKeyValueFidelity(key, value) {
  const issues = [];
  BROKEN_CONTRACTION_RE.lastIndex = 0;
  if (BROKEN_CONTRACTION_RE.test(value)) {
    issues.push({ type: "broken", detail: "Có thể't" });
  }
  BAD_HTML_ATTR_RE.lastIndex = 0;
  if (BAD_HTML_ATTR_RE.test(value)) {
    issues.push({ type: "html-attr", detail: "tiêu đề=" });
  }
  BAN_BAN_RE.lastIndex = 0;
  if (BAN_BAN_RE.test(value)) {
    issues.push({ type: "calque", detail: "bản bản" });
  }
  // 实例 (platform/run instance) mistranslated as "thể hiện" (display/表现).
  // Case-insensitive: capitalized "Thể hiện…" was slipping past includes().
  if (
    typeof key === "string" &&
    key.includes("实例") &&
    typeof value === "string" &&
    value.toLowerCase().includes("thể hiện")
  ) {
    issues.push({ type: "calque", detail: "thể hiện for 实例" });
  }
  for (const id of MACHINE_IDS) {
    if (key.includes(id) && !value.includes(id)) {
      issues.push({ type: "machine-id", detail: id });
    }
  }
  DOCS_MD_RE.lastIndex = 0;
  const docsInKey = key.match(DOCS_MD_RE) || [];
  for (const path of docsInKey) {
    if (!value.includes(path)) {
      issues.push({ type: "docs-path", detail: path });
    }
  }
  return issues;
}

function checkCalques(value) {
  const issues = [];
  // Calque 当前X → "hiện tại …" at start (lowercase hiện)
  if (/^hiện tại /.test(value)) {
    issues.push({ type: "calque", detail: "hiện tại …" });
  }
  // Calque 当前X mid-string (specific nouns)
  HIEN_TAI_X_RE.lastIndex = 0;
  let hm;
  while ((hm = HIEN_TAI_X_RE.exec(value)) !== null) {
    const detail = hm[0].replace(/^[^\p{L}]+/u, "").trim();
    // Avoid duplicate when already flagged by the prefix rule above
    if (!issues.some((i) => i.type === "calque" && i.detail === detail)) {
      issues.push({ type: "calque", detail: detail || "hiện tại …" });
    }
  }
  // Calque 这/该/本 → "Này …" at start
  if (/^Này /.test(value)) {
    issues.push({ type: "calque", detail: "Này …" });
  }
  for (const calque of CALQUES) {
    if (value.includes(calque)) {
      issues.push({ type: "calque", detail: calque });
    }
  }
  for (const { re, detail } of CALQUE_RES) {
    re.lastIndex = 0;
    if (re.test(value)) {
      issues.push({ type: "calque", detail });
    }
  }
  // "của bạn" immediately after an English (ASCII) word
  const re = /\b([A-Za-z][A-Za-z'’\-]*)\s+của bạn\b/g;
  let m;
  while ((m = re.exec(value)) !== null) {
    issues.push({ type: "calque", detail: `${m[1]} của bạn` });
  }
  return issues;
}

/**
 * Check a single dictionary value. When key is provided, also verify
 * placeholder parity against the Chinese/source key.
 * @returns {{type: string, detail: string}[]}
 */
export function checkValue(key, value) {
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

  issues.push(...checkCalques(value));
  issues.push(...checkSplitSyllables(value));
  issues.push(...checkGlued(value));
  issues.push(...checkTerminology(value));
  issues.push(...checkPlaceholders(value));
  issues.push(...checkQuoteGlue(value));
  if (typeof key === "string") {
    issues.push(...checkPlaceholderParity(key, value));
    issues.push(...checkKeyValueFidelity(key, value));
    issues.push(...checkExecMachineTerminology(key, value));
  } else {
    // Value-only calls still catch broken contractions / bad HTML attrs / bản bản.
    issues.push(...checkKeyValueFidelity("", value));
  }

  for (const { word } of extractAsciiWords(value)) {
    const lower = word.toLowerCase();
    const upper = word.toUpperCase();
    // Acronyms first: exact uppercase OK; wrong case is acronym-case (not blacklist).
    if (ACRONYMS.has(upper)) {
      if (word === upper) continue;
      // Vietnamese question word "ai" / "Ai" (who) — not the AI acronym.
      if (upper === "AI" && (word === "Ai" || word === "ai")) continue;
      issues.push({ type: "acronym-case", detail: word });
      continue;
    }
    // Vietnamese ASCII syllables win over the English blacklist ("to" in "phóng to").
    if (VI_ASCII_SET.has(lower)) continue;
    // Blacklist always wins when a word is on both lists (e.g. Contents, run/user).
    if (BLACKLIST_SET.has(lower)) {
      issues.push({ type: "blacklist", detail: word });
      continue;
    }
    // Product / technical allowlist only after blacklist.
    if (isAllowedTerm(word)) continue;
    if (word.length === 1) continue;
    issues.push({ type: "english", detail: word });
  }
  return issues;
}

export function formatHit(key, value, issue) {
  return `${key} => ${value}  [${issue.type}]`;
}

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

function checkEnNoHan(en) {
  const errors = [];
  for (const [key, value] of Object.entries(en)) {
    if (typeof value === "string" && HAN.test(value)) {
      errors.push({
        key,
        value,
        type: "han",
        detail: "en value contains Han characters",
      });
    }
  }
  return errors;
}

export function runCheck() {
  const vi = loadDict("web/i18n-vi.js", "LF_I18N_VI");
  const en = loadDict("web/i18n-en.js", "LF_I18N_EN");

  const errors = [];
  for (const [key, value] of Object.entries(vi)) {
    for (const issue of checkValue(key, value)) {
      errors.push({ key, value, ...issue });
    }
  }
  errors.push(...checkEnNoHan(en));
  return errors;
}

function main() {
  const errors = runCheck();

  if (errors.length === 0) {
    console.log("0 mixed entries");
    process.exit(0);
  }

  console.error(`${errors.length} mixed entries`);
  for (const e of errors) {
    console.error(formatHit(e.key, e.value, e));
  }
  process.exit(1);
}

const isMain =
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (isMain) {
  main();
}
