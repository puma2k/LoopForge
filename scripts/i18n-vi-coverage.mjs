#!/usr/bin/env node
/**
 * Scan UI sources for Chinese display strings and verify they translate to
 * Vietnamese via web/i18n-vi.js (exact key, fragment/chunk, runtime patterns).
 *
 * Usage: node scripts/i18n-vi-coverage.mjs
 * Exit 1 when uncovered strings remain (outside allowlist).
 *
 * Test helpers: import { … } from "./i18n-vi-coverage.mjs"
 */
import { readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { createContext, runInContext } from "node:vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(process.env.LF_REPO || join(__dirname, ".."));

const HAN = /[\u3400-\u9fff]/;
const HAN_CHARS = /[\u3400-\u9fff]/;

/** Attrs rewritten by web/i18n.js */
const ATTR_NAMES =
  "title|placeholder|alt|aria-label|aria-placeholder|aria-description|aria-roledescription|data-tip|data-tooltip|data-title";

/**
 * Exact strings that are intentional non-UI (internal keys, comparisons)
 * or out of scope. Each entry needs a reason.
 */
export const ALLOWLIST = [
  {
    text: "本",
    reason: "single-char fragment excluded by i18n.js (≥3 Han); EN-only stub",
  },
  {
    text: "该",
    reason: "single-char fragment excluded by i18n.js (≥3 Han); EN-only stub",
  },
  // Sample fills with empty refs.length / refs.join — not a real runtime string.
  {
    text: "规则仍被  个引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names; real string uses %s holes",
  },
  {
    text: "技能仍被  个引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names; real string uses %s holes",
  },
  {
    text: "沙箱仍被  个引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names; real string uses %s holes",
  },
  {
    text: "资产仍被  个引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names; real string uses %s holes",
  },
  {
    text: "Skill仍被  个Skill引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names",
  },
  {
    text: "Skill仍被  个引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names",
  },
  {
    text: "MCP仍被  个引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names",
  },
  {
    text: "SOP仍被  个引用（），请先解除引用",
    reason: "coverage sample with empty ref count/names",
  },
  {
    text: "规则 ·",
    reason: "truncated HTML-in-JS sample crumb from flows chrome; not standalone UI",
  },
  {
    text: "技能 ·",
    reason: "truncated HTML-in-JS sample crumb from flows chrome; not standalone UI",
  },
  {
    text: "资产 ·",
    reason: "truncated HTML-in-JS sample crumb from flows chrome; not standalone UI",
  },
  {
    text: "%s 成功 / %s 失败${view.firstPass!=null?",
    reason: "broken extract of nested template ternary — incomplete source slice",
  },
  {
    text: ",'保存项目设置时同时清空 Workspace 与 Iteration，已导入的需求不受影响。',",
    reason: "JS array literal slice mis-extracted as display string",
  },
];

/**
 * Path allowlist: unit-test / fixture sources that are not shipped UI.
 * Matched against repo-relative paths.
 */
export const ALLOWLIST_PATHS = [
  {
    re: /_test\.js$/i,
    reason: "web/*_test.js harness fixtures — not shipped UI",
  },
  {
    re: /fixtures?\./i,
    reason: "local conversation / storybook fixtures — not shipped UI",
  },
  {
    re: /\/hui_fake_dom\.js$/i,
    reason: "test DOM stub",
  },
];

const ALLOWLIST_SET = new Set(ALLOWLIST.map((a) => a.text));

export function isPathAllowlisted(relPath) {
  const p = String(relPath || "").replace(/\\/g, "/");
  return ALLOWLIST_PATHS.some((a) => a.re.test(p));
}

const SKIP_DIRS = new Set([
  "monaco",
  "node_modules",
  "vscode-web",
  "native-chat",
  ".git",
]);

export function loadDict(relPath = "web/i18n-vi.js", globalName = "LF_I18N_VI") {
  const code = readFileSync(join(REPO, relPath), "utf8");
  const ctx = createContext({ window: {} });
  runInContext(code, ctx, { filename: relPath });
  const dict = ctx.window[globalName];
  if (!dict || typeof dict !== "object") {
    throw new Error(`Failed to load ${globalName} from ${relPath}`);
  }
  return dict;
}

function walk(dir, pred, out = []) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return out;
  }
  for (const name of entries) {
    if (SKIP_DIRS.has(name)) continue;
    const p = join(dir, name);
    let st;
    try {
      st = statSync(p);
    } catch {
      continue;
    }
    if (st.isDirectory()) walk(p, pred, out);
    else if (pred(p)) out.push(p);
  }
  return out;
}

/** Source files listed in the task brief. */
export function listSourceFiles(repo = REPO) {
  const files = new Set();
  const web = join(repo, "web");
  const add = (p) => files.add(resolve(p));

  add(join(web, "index.html"));
  for (const f of walk(web, (p) => p.endsWith(".html"))) add(f);
  for (const f of walk(join(web, "app"), (p) => p.endsWith(".js"))) add(f);
  for (const name of readdirSync(web)) {
    if (!name.endsWith(".js")) continue;
    if (/^i18n(-|$)/.test(name)) continue;
    add(join(web, name));
  }
  add(join(repo, "chatbot/index.html"));
  for (const f of walk(join(repo, "chatbot/src"), (p) => /\.(js|jsx)$/.test(p))) {
    add(f);
  }
  return [...files].sort();
}

/**
 * Strip // and /* *\/ comments (JS) or HTML comments, preserving string bodies
 * and line numbers (comments replaced with spaces).
 */
export function stripComments(src, isHtml = false) {
  if (isHtml) {
    return src.replace(/<!--[\s\S]*?-->/g, (m) =>
      m.replace(/[^\n]/g, " "),
    );
  }
  let out = "";
  let i = 0;
  let state = "code";
  while (i < src.length) {
    const c = src[i];
    const n = src[i + 1];
    if (state === "code") {
      if (c === "/" && n === "/") {
        state = "line";
        out += "  ";
        i += 2;
        continue;
      }
      if (c === "/" && n === "*") {
        state = "block";
        out += "  ";
        i += 2;
        continue;
      }
      if (c === "'") {
        state = "s";
        out += c;
        i++;
        continue;
      }
      if (c === '"') {
        state = "d";
        out += c;
        i++;
        continue;
      }
      if (c === "`") {
        state = "t";
        out += c;
        i++;
        continue;
      }
      // regex literal: only when plausible (after = ( [ , : ! & | ? { ; or start)
      if (c === "/" && /[=(,:[!&|?{;]\s*$/.test(out)) {
        state = "re";
        out += c;
        i++;
        continue;
      }
      out += c;
      i++;
      continue;
    }
    if (state === "line") {
      if (c === "\n") {
        state = "code";
        out += c;
      } else out += " ";
      i++;
      continue;
    }
    if (state === "block") {
      if (c === "*" && n === "/") {
        state = "code";
        out += "  ";
        i += 2;
        continue;
      }
      out += c === "\n" ? "\n" : " ";
      i++;
      continue;
    }
    if (state === "s" || state === "d") {
      const q = state === "s" ? "'" : '"';
      out += c;
      if (c === "\\" && n != null) {
        out += n;
        i += 2;
        continue;
      }
      if (c === q) state = "code";
      i++;
      continue;
    }
    if (state === "t") {
      out += c;
      if (c === "\\" && n != null) {
        out += n;
        i += 2;
        continue;
      }
      if (c === "`") state = "code";
      i++;
      continue;
    }
    if (state === "re") {
      out += c;
      if (c === "\\" && n != null) {
        out += n;
        i += 2;
        continue;
      }
      if (c === "/" && !isInCharClass(out)) {
        // flags
        while (i + 1 < src.length && /[gimsuyvd]/.test(src[i + 1])) {
          i++;
          out += src[i];
        }
        state = "code";
      }
      i++;
      continue;
    }
  }
  return out;
}

function isInCharClass(out) {
  // Rough: odd number of unescaped [ since last /
  const slash = out.lastIndexOf("/");
  const slice = out.slice(slash + 1);
  let open = 0;
  for (let i = 0; i < slice.length; i++) {
    if (slice[i] === "\\" ) {
      i++;
      continue;
    }
    if (slice[i] === "[") open++;
    else if (slice[i] === "]" && open) open--;
  }
  return open > 0;
}

function unescapeJs(str) {
  return str.replace(/\\([\\'"nrtbfv0`])/g, (_, c) => {
    const map = {
      n: "\n",
      r: "\r",
      t: "\t",
      b: "\b",
      f: "\f",
      v: "\v",
      "0": "\0",
      "\\": "\\",
      "'": "'",
      '"': '"',
      "`": "`",
    };
    return map[c] ?? c;
  });
}

/**
 * Extract quoted / template-literal contents that contain Han characters.
 */
export function extractQuotedStrings(src) {
  const found = [];
  const reQ = /(["'])((?:\\.|(?!\1).)*)\1/g;
  let m;
  while ((m = reQ.exec(src)) !== null) {
    const before = src.slice(Math.max(0, m.index - 60), m.index);
    if (/console\.(log|warn|error|debug|info|trace)\s*\(\s*$/.test(before)) {
      continue;
    }
    if (/new\s+RegExp\s*\(\s*$/.test(before)) continue;
    const raw = unescapeJs(m[2]);
    if (!HAN.test(raw)) continue;
    found.push({ raw, index: m.index, kind: "quote" });
  }
  const reT = /`((?:\\.|[^`\\])*)`/g;
  while ((m = reT.exec(src)) !== null) {
    const before = src.slice(Math.max(0, m.index - 60), m.index);
    if (/console\.(log|warn|error|debug|info|trace)\s*\(\s*$/.test(before)) {
      continue;
    }
    const raw = unescapeJs(m[1]);
    if (!HAN.test(raw)) continue;
    found.push({ raw, index: m.index, kind: "template" });
  }
  return found;
}

export function looksLikeHtml(s) {
  return (
    /<[a-zA-Z][^>]*>/.test(s) &&
    (s.includes("</") ||
      /<[a-zA-Z][^>]*\/>/.test(s) ||
      new RegExp(`(?:${ATTR_NAMES}|class)\\s*=`).test(s))
  );
}

/** Pull display text + i18n attrs out of an HTML fragment. */
export function extractFromHtml(html) {
  const parts = [];
  const attrRe = new RegExp(
    `(?:${ATTR_NAMES})\\s*=\\s*(["'])([^"']*)\\1`,
    "gi",
  );
  let m;
  while ((m = attrRe.exec(html)) !== null) {
    if (HAN.test(m[2])) parts.push({ text: m[2], kind: "attr" });
  }
  const textRe = />([^<>]+)</g;
  while ((m = textRe.exec(html)) !== null) {
    const t = m[1]
      .replace(/&nbsp;/g, " ")
      .replace(/&lt;/g, "<")
      .replace(/&gt;/g, ">")
      .replace(/&amp;/g, "&")
      .replace(/&quot;/g, '"')
      .replace(/\s+/g, " ")
      .trim();
    if (HAN.test(t)) parts.push({ text: t, kind: "text" });
  }
  return parts;
}

/** JSX text nodes: >中文< and attrs already covered by quotes. */
export function extractJsxText(src) {
  const parts = [];
  const re = />([^<>{}\n]*[\u3400-\u9fff][^<>{}\n]*)</g;
  let m;
  while ((m = re.exec(src)) !== null) {
    const t = m[1].replace(/\s+/g, " ").trim();
    if (HAN.test(t)) parts.push({ text: t, index: m.index, kind: "jsx" });
  }
  return parts;
}

/**
 * Normalize template interpolations / printf-style holes for dict lookup.
 */
export function normalizePlaceholders(text) {
  return text
    .replace(/\$\{[^}]+\}/g, "%s")
    .replace(/\{\{[^{}]+\}\}/g, "%s")
    .replace(/\{[^{}]+\}/g, "%s");
}

/**
 * Build a translator that mirrors web/i18n.js (exact → fragments → patterns).
 */
/** Representative fills for label-like ${…} holes when probing runtime coverage. */
export const INTERP_SAMPLES = [
  "规则",
  "技能",
  "沙箱",
  "资产",
  "Skill",
  "MCP",
  "SOP",
];

/**
 * True when a ${…} expression interpolates an asset kind/label.
 * Bare `label` only counts in delete/create confirm templates (assets.js),
 * not machine/CLI pickers (`执行机器：${label}` / `启用 ${label}`).
 */
export function isLabelExpr(expr, raw = "") {
  const e = String(expr || "");
  if (
    /^\s*escapeHTML\s*\(\s*kind\s*\)\s*$/.test(e) ||
    /^\s*kind\s*$/.test(e) ||
    /assetKindLabel\s*\(/.test(e)
  ) {
    return true;
  }
  if (/^\s*label\s*$/.test(e)) {
    return /删除|确认删除|已删除|新建|创建/.test(String(raw || ""));
  }
  return false;
}

/**
 * Only probe sample fills when the template interpolates a display label
 * (e.g. escapeHTML(kind) / assetKindLabel / label), not ids, counts, or control flow.
 */
export function shouldSampleExpand(raw, normalized) {
  if (!raw || !normalized || !/%s/.test(normalized)) return false;
  if (!/\$\{/.test(raw)) return false;
  const exprs = [...String(raw).matchAll(/\$\{([^}]+)\}/g)].map((m) => m[1]);
  if (!exprs.length) return false;
  return exprs.some((e) => isLabelExpr(e, raw));
}

/**
 * Expand ${a?b:c} ternaries whose branches are Chinese string literals into
 * separate templates (e.g. 共享/本项目 + kind → 本项目规则).
 */
export function expandChineseTernaries(raw) {
  const s = String(raw || "");
  const re = /\$\{([^}]*['"][^'"]*[\u3400-\u9fff][^'"]*['"][^}]*)\}/;
  const m = s.match(re);
  if (!m) return [s];
  const branches = [
    ...m[1].matchAll(/'([^']*)'|"([^"]*)"/g),
  ]
    .map((x) => x[1] || x[2])
    .filter((b) => b && HAN.test(b));
  if (branches.length < 2) return [s];
  return branches.flatMap((b) => expandChineseTernaries(s.replace(m[0], b)));
}

/**
 * Fill label-like ${…} with INTERP_SAMPLES; name-like holes → Foo; others → "".
 * Multi-hole templates only expand label holes (review: probe each label hole).
 */
export function sampleFilledTemplates(raw) {
  const normalized = normalizePlaceholders(raw);
  if (!shouldSampleExpand(raw, normalized)) return [];
  const variants = expandChineseTernaries(raw);
  const out = [];
  for (const variant of variants) {
    for (const fill of INTERP_SAMPLES) {
      const filled = String(variant).replace(/\$\{([^}]+)\}/g, (_, expr) => {
        if (isLabelExpr(expr, variant)) return fill;
        // Name / search query holes → Foo so 「%s」 samples stay realistic.
        if (/\.name\b|copy\.page|\bquery\b|assetQuery|trim\(/.test(expr)) {
          return "Foo";
        }
        // assets.js aria go prefix (查看/编辑) — use one representative.
        if (/^\s*go\s*$/.test(expr)) return "查看";
        return "";
      });
      if (HAN.test(filled)) out.push(filled);
    }
  }
  return [...new Set(out)];
}

/**
 * True when ${…} is decorative chrome (icon / spinner / glyph) that becomes a
 * separate DOM node at runtime — the following Chinese is a bare text node.
 */
export function isIconChromeExpr(expr) {
  const e = String(expr || "").trim();
  // ${botIcon(...)} / ${huiIcon(...)} / ${flowPalette.icon(...)} / ${ic(...)}
  // become a separate DOM node; following Han is a bare text node at runtime.
  const m = e.match(/^(?:[\w$]+\.)*([\w$]+)\s*\(/);
  if (!m) return false;
  const name = m[1];
  if (name === "ic" || name === "icon") return true;
  return /(?:Icon|Spinner|Glyph|Lamp)$/.test(name);
}

/**
 * Empty-fill icon/spinner/glyph holes so coverage sees the runtime bare text
 * (e.g. ${huiIcon('lock')}只读… → 只读…).
 */
export function emptyFillIconChrome(raw) {
  const s = String(raw || "");
  if (!/\$\{/.test(s)) return s;
  return s.replace(/\$\{([^}]+)\}/g, (full, expr) =>
    isIconChromeExpr(expr) ? "" : full,
  );
}

/**
 * Probe fills for an already-normalized piece containing %s.
 * Single-hole: INTERP_SAMPLES. Multi-hole pieces are handled via
 * sampleFilledTemplates on the raw template instead (avoids 规则创建规则).
 * When opts.iconPrefix: leading `%s`+Han gets empty fill only (bare runtime
 * text after huiIcon/spinner) — never INTERP_SAMPLES (avoids 规则访问密钥…).
 */
export function sampleFillsForPiece(piece, opts = {}) {
  const text = String(piece || "");
  const holes = (text.match(/%s/g) || []).length;
  if (holes !== 1) return [];
  if (opts.iconPrefix) {
    // Glued `%s`+Han only (e.g. huiIcon()只读). Spaced `${icon} 打开…` /
    // number holes (`${n} 个…`) are handled by emptyFillIconChrome on the
    // raw template — never INTERP_SAMPLES under iconPrefix.
    if (/^%s[\u3400-\u9fff]/.test(text)) {
      return [text.replace(/^%s/, "")];
    }
    return [];
  }
  return INTERP_SAMPLES.map((fill) => text.replace(/%s/g, fill));
}

export function createTranslator(dict) {
  const escape = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const fragments = Object.keys(dict)
    .filter((k) => {
      if ((k.match(/[\u3400-\u9fff]/g) || []).length < 3) return false;
      const val = dict[k];
      return typeof val === "string" && val.length && !HAN_CHARS.test(val);
    })
    .sort((a, b) => b.length - a.length);
  const fragmentREs = [];
  const CHUNK = 200;
  for (let i = 0; i < fragments.length; i += CHUNK) {
    const chunk = fragments.slice(i, i + CHUNK);
    fragmentREs.push(
      new RegExp(
        `(?<![\\u3400-\\u9fff])(?:${chunk.map(escape).join("|")})(?![\\u3400-\\u9fff])`,
        "g",
      ),
    );
  }
  const printfKeys = Object.keys(dict)
    .filter((k) => {
      if (!k.includes("%s")) return false;
      const val = dict[k];
      if (typeof val !== "string" || !val || HAN_CHARS.test(val)) return false;
      const lit = k.replace(/%s/g, "");
      const han = (lit.match(/[\u3400-\u9fff]/g) || []).length;
      if (han >= 2) return true;
      if (han >= 1 && lit.trim().length >= 2) return true;
      return /[A-Za-z]{2,}/.test(lit) && lit.trim().length >= 3;
    })
    .sort((a, b) => b.length - a.length);
  const printfPatterns = printfKeys.map((k) => {
    const parts = k.split("%s");
    let reSrc = "^";
    for (let i = 0; i < parts.length; i++) {
      reSrc += escape(parts[i]);
      if (i < parts.length - 1) reSrc += "([\\s\\S]+?)";
    }
    reSrc += "$";
    return { re: new RegExp(reSrc), key: k };
  });
  function translateSlot(slot) {
    const trimmed = slot && String(slot).trim();
    if (
      trimmed &&
      Object.prototype.hasOwnProperty.call(dict, trimmed) &&
      dict[trimmed] &&
      !HAN_CHARS.test(dict[trimmed])
    ) {
      return String(slot).replace(trimmed, dict[trimmed]);
    }
    return slot;
  }
  function applyPrintf(text) {
    const trimmed = text.trim();
    for (const { re, key } of printfPatterns) {
      const m = trimmed.match(re);
      if (!m) continue;
      let i = 1;
      const filled = dict[key].replace(/%s/g, () => translateSlot(m[i++]));
      return text.replace(trimmed, filled);
    }
    return text;
  }
  const level = { 高: "Cao", 中: "Trung bình", 低: "Thấp" };
  const patterns = [
    [/第\s*(\d+)\s*步[，,]\s*共\s*(\d+)\s*步/g, "Bước $1/$2"],
    [/第\s*(\d+)\s*步(?![\u3400-\u9fff])/g, "Bước $1"],
    [/第\s*(\d+)\s*(?:阶段|Stage)/g, "Giai đoạn $1"],
    [/第\s*(\d+)\s*(?:轮|Round)/g, "Vòng $1"],
    [/ · (高|中|低) · /g, (_, p) => ` · ${level[p]} · `],
    [/入\s*(\d+)\s*\/\s*出\s*(\d+)/g, "Vào $1 / Ra $2"],
    [/(\d{4})年(\d{1,2})月/g, (_, y, m) => `Thg ${m} ${y}`],
    [/(^|[^\d])(\d{1,2})月(?![\u3400-\u9fff])/g, "$1Thg $2"],
    [/共\s*(\d+)/g, "Tổng $1"],
    [/更新于/g, "cập nhật lúc"],
    [/近\s*(\d+)\s*天/g, "Gần $1 ngày"],
    [/最近\s*(\d+)\s*天/g, "$1 ngày gần đây"],
    [/成功率/g, "Tỷ lệ thành công"],
    [/累计/g, "Tổng"],
    [/(\d+)\s*人(?![\u3400-\u9fff])/g, "$1 người"],
    [/(\d+)\s*单(?![\u3400-\u9fff])/g, "$1 yêu cầu"],
    [/(^|[\s·])成功\s+(\d+)/g, "$1Thành công $2"],
    [/(^|[\s·])失败\s+(\d+)/g, "$1Thất bại $2"],
    [/于\s+(\d{1,2}:\d{2})/g, "lúc $1"],
    [/默认[:：]\s*/g, "Mặc định: "],
    [/创建\s+/g, "Tạo "],
    [/LoopForge 云端/g, "LoopForge Cloud"],
    [/云端/g, "Cloud"],
    [/停下(?=[\u3400-\u9fffA-Za-zÀ-ỹ])/g, "Dừng "],
    [/^停下$/g, "Dừng"],
    [/并归档/g, " và lưu trữ"],
    [/按\s*(?=SOP|PR|MR)/g, "Theo "],
    [
      /(\d+(?:\.\d+)?)\s*(次|步|个|台|条)(?![\u3400-\u9fff])/g,
      (_, n) => n,
    ],
  ];

  function translate(text) {
    if (!text || !HAN.test(text)) return text;
    const trimmed = text.trim();
    let out;
    if (
      Object.prototype.hasOwnProperty.call(dict, trimmed) &&
      dict[trimmed] &&
      !HAN_CHARS.test(dict[trimmed])
    ) {
      out = text.replace(trimmed, dict[trimmed]);
    } else {
      out = applyPrintf(text);
    }
    if (HAN.test(out)) {
      for (const re of fragmentREs) {
        out = out.replace(re, (match) => {
          const rep = dict[match];
          return typeof rep === "string" ? rep : match;
        });
      }
      for (const [re, rep] of patterns) out = out.replace(re, rep);
    }
    return out;
  }

  return { translate, fragmentCount: fragments.length, printfCount: printfKeys.length };
}

/**
 * Heuristic: source slice is JS/CSS/selector plumbing, not a user-visible string.
 * Coverage should not require dictionary entries for these.
 */
export function isNonDisplaySource(text) {
  const t = String(text || "");
  if (!t) return true;
  // CSS / query selectors kept as machine strings.
  if (/^[a-z]+(\[[^\]]*\])+$/i.test(t)) return true;
  if (/\[[a-z-]+=/.test(t) && /\]/.test(t) && t.length < 80 && !/\s/.test(t.replace(/\[[^\]]*\]/g, ""))) {
    return true;
  }
  // Obvious JS / TS fragments pulled from broken template extraction.
  if (
    /\b(?:function|const|let|var|return|export|import|await|async|class)\b/.test(t) &&
    /[{};=]/.test(t)
  ) {
    return true;
  }
  if (/\b(?:querySelector|addEventListener|createElement|innerHTML|classList)\b/.test(t)) {
    return true;
  }
  // Mostly code with a tiny Chinese stub (e.g. default string inside a function).
  const han = (t.match(/[\u3400-\u9fff]/g) || []).length;
  if (han > 0 && han / t.length < 0.08 && /[{};()=>]/.test(t) && t.length > 80) {
    return true;
  }
  // Regex bodies / selector fragments / HTML attribute stubs.
  if (/^\^/.test(t) || (/\\s|\\d|\\w/.test(t) && /[|*+?]/.test(t))) return true;
  if (/\[(?:aria-label|data-[\w-]+)=/.test(t)) return true;
  if (/^placeholder=/.test(t) || /<\/?[a-z]+[\s>]/i.test(t) && /\$\{/.test(t)) return true;
  if (/\[language\]/.test(t) || (/\|\|/.test(t) && /%s/.test(t) && t.length < 40)) return true;
  // Broken template tails / HTML stubs from incomplete extractions.
  if (/\$\{[a-zA-Z_][\w.]*\??$/.test(t)) return true;
  if (/\$\{[a-zA-Z_][\w.]*\($/.test(t)) return true; // ${cum(
  if (/\.map\s*\(\s*[a-zA-Z_]\w*\s*=>\s*$/.test(t)) return true;
  if (/^[:'),}\s]/.test(t) && /\$\{|paRow|references\.map/.test(t)) return true;
  if (/^: label\}/.test(t)) return true;
  if (/\$\{/.test(t) && /[\(\{]\s*$/.test(t)) return true;
  return false;
}

export function isFullyTranslated(text, translate) {
  if (!text || !HAN.test(text)) return true;
  if (ALLOWLIST_SET.has(text.trim())) return true;
  if (isNonDisplaySource(text)) return true;
  const direct = translate(text);
  if (!HAN.test(direct)) return true;
  // Placeholder-normalized variant (dynamic templates / printf keys).
  const normalized = normalizePlaceholders(text);
  if (normalized !== text && !HAN.test(translate(normalized))) return true;
  // Do NOT strip %s then translate the leftover stub (创建%s → 创建 → Tạo).
  // That falsely marks templates covered while runtime 创建规则 stays Chinese.
  // Sample fills from extractDisplayStrings catch the runtime shapes instead.
  return false;
}

function lineOf(src, index) {
  return src.slice(0, Math.max(0, index)).split("\n").length;
}

/**
 * Collect display candidates from one source file.
 */
export function extractDisplayStrings(src, { isHtml = false, isJsx = false } = {}) {
  const cleaned = stripComments(src, isHtml);
  const out = [];
  const push = (text, index, kind) => {
    const t = String(text || "").trim();
    if (!t || !HAN.test(t)) return;
    out.push({ text: t, index, kind });
  };

  if (isHtml) {
    for (const p of extractFromHtml(cleaned)) {
      push(p.text, cleaned.indexOf(p.text), `html-${p.kind}`);
    }
  }

  for (const { raw, index, kind } of extractQuotedStrings(cleaned)) {
    let text = raw;
    if (kind === "template") text = normalizePlaceholders(raw);
    const sample = kind === "template" && shouldSampleExpand(raw, text);
    const hasIconChrome =
      kind === "template" &&
      [...String(raw).matchAll(/\$\{([^}]+)\}/g)].some((m) =>
        isIconChromeExpr(m[1]),
      );
    if (looksLikeHtml(text)) {
      for (const p of extractFromHtml(text)) {
        push(p.text, index, `html-in-js-${p.kind}`);
        // Single-%s pieces still get local sample fills (labels).
        if (sample) {
          for (const filled of sampleFillsForPiece(p.text)) {
            push(filled, index, `html-in-js-${p.kind}-sample`);
          }
        } else if (hasIconChrome) {
          // Icon/spinner prefix: empty-fill only (runtime bare Chinese).
          for (const filled of sampleFillsForPiece(p.text, { iconPrefix: true })) {
            push(filled, index, `html-in-js-${p.kind}-icon-empty`);
          }
        }
      }
    } else {
      push(text, index, kind);
      if (sample) {
        for (const filled of sampleFillsForPiece(text)) {
          push(filled, index, `${kind}-sample`);
        }
      } else if (hasIconChrome) {
        for (const filled of sampleFillsForPiece(text, { iconPrefix: true })) {
          push(filled, index, `${kind}-icon-empty`);
        }
      }
    }
    // Label-aware raw expansion (multi-hole + Chinese ternaries → runtime shapes).
    if (sample) {
      for (const filledRaw of sampleFilledTemplates(raw)) {
        const filledText = normalizePlaceholders(filledRaw);
        if (looksLikeHtml(filledText) || looksLikeHtml(filledRaw)) {
          for (const p of extractFromHtml(filledText)) {
            push(p.text, index, `html-in-js-sample`);
          }
        } else {
          push(filledText, index, `${kind}-sample`);
        }
      }
    }
    // Icon/spinner/glyph → empty fill so bare Chinese after chrome is checked.
    if (hasIconChrome) {
      const emptiedRaw = emptyFillIconChrome(raw);
      const emptiedText = normalizePlaceholders(emptiedRaw);
      if (looksLikeHtml(emptiedText) || looksLikeHtml(emptiedRaw)) {
        for (const p of extractFromHtml(emptiedText)) {
          push(p.text, index, `html-in-js-icon-empty`);
        }
      } else if (emptiedText !== text) {
        push(emptiedText, index, `template-icon-empty`);
      }
    }
  }

  if (isJsx) {
    for (const p of extractJsxText(cleaned)) {
      push(p.text, p.index, "jsx");
    }
  }

  return out;
}

/**
 * Scan repo and return uncovered Chinese display strings.
 */
export function findMissing(options = {}) {
  const repo = options.repo || REPO;
  const dict = options.dict || loadDict();
  const { translate } = createTranslator(dict);
  const files = options.files || listSourceFiles(repo);
  const missing = [];
  const seen = new Set();

  for (const file of files) {
    const rel = relative(repo, file).replace(/\\/g, "/");
    if (isPathAllowlisted(rel)) continue;
    let src;
    try {
      src = readFileSync(file, "utf8");
    } catch {
      continue;
    }
    const isHtml = file.endsWith(".html");
    const isJsx = /\.jsx$/.test(file);
    const candidates = extractDisplayStrings(src, { isHtml, isJsx });
    for (const c of candidates) {
      if (isFullyTranslated(c.text, translate)) continue;
      const key = c.text;
      if (seen.has(key)) {
        // Still record first file:line only; count duplicates via seen.
        continue;
      }
      seen.add(key);
      missing.push({
        file: rel,
        line: lineOf(src, c.index),
        text: c.text,
        kind: c.kind,
      });
    }
  }

  return { missing, translate, dict };
}

export function formatMissing(missing) {
  return missing
    .map((m) => `${m.file}:${m.line}: ${JSON.stringify(m.text)}`)
    .join("\n");
}

export function runCoverage() {
  const { missing } = findMissing();
  return missing;
}

function main() {
  const missing = runCoverage();
  if (missing.length === 0) {
    console.log("0 uncovered Chinese UI strings");
    process.exit(0);
  }
  console.error(`${missing.length} uncovered Chinese UI strings`);
  console.error(formatMissing(missing));
  process.exit(1);
}

const isMain =
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (isMain) {
  main();
}
