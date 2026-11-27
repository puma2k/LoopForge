#!/usr/bin/env node
/**
 * Unit tests for i18n-vi-coverage extraction + translation simulation.
 * Run: node --test scripts/i18n-vi-coverage.test.mjs
 */
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import {
  createTranslator,
  emptyFillIconChrome,
  extractDisplayStrings,
  extractFromHtml,
  extractQuotedStrings,
  isFullyTranslated,
  isIconChromeExpr,
  normalizePlaceholders,
  sampleFillsForPiece,
  stripComments,
} from "./i18n-vi-coverage.mjs";

describe("stripComments", () => {
  it("removes line and block comments but keeps string bodies", () => {
    const src = `
      // 中文注释
      const a = "可见中文";
      /* 块注释中文 */
      const b = '也可见';
    `;
    const out = stripComments(src);
    assert.equal(out.includes("中文注释"), false);
    assert.equal(out.includes("块注释中文"), false);
    assert.equal(out.includes("可见中文"), true);
    assert.equal(out.includes("也可见"), true);
  });
});

describe("extractQuotedStrings", () => {
  it("finds Chinese in quotes and templates", () => {
    const src = `
      const a = "保存成功";
      const b = \`共 \${n} 条\`;
      console.log("调试中文");
    `;
    const found = extractQuotedStrings(stripComments(src));
    const texts = found.map((f) => f.raw);
    assert.ok(texts.some((t) => t.includes("保存成功")));
    assert.ok(texts.some((t) => t.includes("共") && t.includes("条")));
    assert.equal(
      texts.some((t) => t.includes("调试中文")),
      false,
      "console.log should be skipped",
    );
  });
});

describe("extractFromHtml", () => {
  it("pulls text nodes and aria/title attrs", () => {
    const html = `<button title="关闭" aria-label="搜索模型">保存</button>`;
    const parts = extractFromHtml(html).map((p) => p.text);
    assert.deepEqual(new Set(parts), new Set(["关闭", "搜索模型", "保存"]));
  });
});

describe("normalizePlaceholders", () => {
  it("collapses ${…} / {x} / {{x}}", () => {
    assert.equal(normalizePlaceholders("共 ${n} 条"), "共 %s 条");
    assert.equal(normalizePlaceholders("你好 {name}"), "你好 %s");
    assert.equal(normalizePlaceholders("标题 {{issue.title}}"), "标题 %s");
  });
});

describe("createTranslator + isFullyTranslated", () => {
  const dict = {
    保存: "Lưu",
    保存成功: "Đã lưu",
    关闭: "Đóng",
    共: "Tổng",
    条: "",
    无法加载工作区执行机器: "Không thể tải máy thực thi của workspace",
  };
  const { translate } = createTranslator(dict);

  it("exact key hits", () => {
    assert.equal(translate("保存成功"), "Đã lưu");
    assert.equal(isFullyTranslated("保存成功", translate), true);
  });

  it("fragment coverage for composites", () => {
    // 关闭 has only 2 Han → exact match only (still covered).
    assert.equal(isFullyTranslated("关闭", translate), true);
    // Longer key (≥3 Han) applies as a fragment when embedded with ASCII only around it.
    assert.equal(isFullyTranslated("[retry] 无法加载工作区执行机器", translate), true);
  });

  it("reports uncovered Chinese", () => {
    assert.equal(isFullyTranslated("尚未翻译的句子啊", translate), false);
  });

  it("covers placeholder-normalized templates when fragments exist", () => {
    // exact "共 %s 条" not in dict; fragments 共 (≥1 but need ≥3 Han — 共 is 1)
    // So this may still be uncovered unless whole string in dict — document behavior.
    const t = "共 %s 条";
    // 共 and 条 are short; without dict entry stays Han → uncovered is OK for test of negative path
    // Add a full key:
    const dict2 = { ...dict, "共 %s 条": "Tổng %s mục" };
    const { translate: tr2 } = createTranslator(dict2);
    assert.equal(isFullyTranslated(t, tr2), true);
  });

  it("covers runtime-interpolated %s samples via printf patterns", () => {
    const dict2 = {
      ...dict,
      "给%s起个名字": "Đặt tên cho %s",
      "%s内容": "Nội dung %s",
      规则: "Quy tắc",
      Skill: "Skill",
    };
    const { translate: tr2 } = createTranslator(dict2);
    assert.equal(tr2("给规则起个名字"), "Đặt tên cho Quy tắc");
    assert.equal(isFullyTranslated("给规则起个名字", tr2), true);
    assert.equal(tr2("Skill内容"), "Nội dung Skill");
    assert.equal(isFullyTranslated("Skill内容", tr2), true);
    assert.equal(isFullyTranslated("给Skill起个名字", tr2), true);
  });

  it("does not treat collapse of %s stubs as covered (创建%s → 创建)", () => {
    const dict2 = {
      ...dict,
      创建: "Tạo",
      规则: "Quy tắc",
    };
    const { translate: tr2 } = createTranslator(dict2);
    // Old collapse path: 创建%s → stub 创建 → Tạo (false covered). Must stay uncovered.
    assert.equal(isFullyTranslated("创建%s", tr2), false);
    assert.equal(isFullyTranslated("创建规则", tr2), false);
  });

  it("covers 创建%s / 创建规则 when printf key exists", () => {
    const dict2 = {
      ...dict,
      "创建%s": "Tạo %s",
      规则: "Quy tắc",
      创建规则: "Tạo quy tắc",
    };
    const { translate: tr2 } = createTranslator(dict2);
    assert.equal(tr2("创建规则"), "Tạo quy tắc");
    // printf on the template leaves "%s" but no Han → covered
    assert.equal(tr2("创建%s"), "Tạo %s");
    assert.equal(isFullyTranslated("创建%s", tr2), true);
    assert.equal(isFullyTranslated("创建规则", tr2), true);
  });
});

describe("icon/spinner chrome empty-fill", () => {
  it("detects huiIcon / spinner / glyph exprs", () => {
    assert.equal(isIconChromeExpr("huiIcon('lock')"), true);
    assert.equal(isIconChromeExpr("credentialSpinner(13)"), true);
    assert.equal(isIconChromeExpr("credentialGlyph('lock',13)"), true);
    assert.equal(isIconChromeExpr("escapeHTML(kind)"), false);
  });

  it("emptyFillIconChrome strips chrome holes only", () => {
    assert.equal(
      emptyFillIconChrome("${huiIcon('lock')}只读 · 可直接在本项目使用"),
      "只读 · 可直接在本项目使用",
    );
    assert.equal(
      emptyFillIconChrome("${credentialSpinner(13)}正在校验令牌有效性与权限…"),
      "正在校验令牌有效性与权限…",
    );
    assert.equal(
      emptyFillIconChrome("${huiIcon('plus')}创建${escapeHTML(kind)}"),
      "创建${escapeHTML(kind)}",
    );
  });

  it("sampleFillsForPiece empty-fills leading %s+Han only with iconPrefix", () => {
    assert.deepEqual(
      sampleFillsForPiece("%s只读 · 可直接在本项目使用", { iconPrefix: true }),
      ["只读 · 可直接在本项目使用"],
    );
    assert.equal(
      sampleFillsForPiece("%s只读 · 可直接在本项目使用").includes(
        "只读 · 可直接在本项目使用",
      ),
      false,
      "without iconPrefix, do not empty-fill (label probes use INTERP_SAMPLES)",
    );
    assert.equal(
      sampleFillsForPiece("创建%s", { iconPrefix: true }).includes("创建"),
      false,
      "label suffix hole must not empty-fill to stub 创建",
    );
  });

  it("extractDisplayStrings probes bare Chinese after icon chrome", () => {
    const src = `
      actions = \`<span class="asset-badge is-ro">\${huiIcon('lock')}只读 · 可直接在本项目使用</span>\`;
      btn = \`<button>\${huiIcon('plus')}安装到本项目</button>\`;
      line = \`<p>\${credentialSpinner(13)}正在校验令牌有效性与权限…</p>\`;
    `;
    const texts = new Set(extractDisplayStrings(src).map((p) => p.text));
    assert.ok(
      texts.has("只读 · 可直接在本项目使用"),
      `expected bare 只读…, got ${JSON.stringify([...texts])}`,
    );
    assert.ok(
      texts.has("安装到本项目"),
      `expected bare 安装到本项目, got ${JSON.stringify([...texts])}`,
    );
    assert.ok(
      texts.has("正在校验令牌有效性与权限…"),
      `expected bare 正在校验…, got ${JSON.stringify([...texts])}`,
    );
  });

  it("pref-key alone does not cover bare icon text (must-fail)", () => {
    const dict = {
      "%s只读 · 可直接在本项目使用": "%s chỉ đọc · có thể dùng trực tiếp trong dự án này",
      "%s安装到本项目": "%s Cài vào dự án này",
    };
    const { translate } = createTranslator(dict);
    assert.equal(isFullyTranslated("只读 · 可直接在本项目使用", translate), false);
    assert.equal(isFullyTranslated("安装到本项目", translate), false);
    assert.equal(isFullyTranslated("正在校验令牌有效性与权限…", translate), false);
  });
});

describe("extractDisplayStrings samples for ${…}", () => {
  it("emits sample-filled variants for template interpolations", () => {
    const src = `
      const kind = assetKindLabel();
      el.placeholder = \`给\${escapeHTML(kind)}起个名字\`;
      el.setAttribute("aria-label", \`\${escapeHTML(kind)}内容\`);
    `;
    const parts = extractDisplayStrings(src, { isHtml: false });
    const texts = new Set(parts.map((p) => p.text));
    assert.ok(texts.has("给%s起个名字") || [...texts].some((t) => t.includes("起个名字")));
    assert.ok(
      [...texts].some((t) => t === "给规则起个名字" || t === "给Skill起个名字"),
      `expected sample fills, got ${JSON.stringify([...texts])}`,
    );
  });

  it("probes multi-hole label templates without all-same compounds", () => {
    const src = `
      const label = assetKindLabel();
      btn.textContent = \`\${huiIcon('plus')}创建\${escapeHTML(kind)}\`;
      count.innerHTML = \`\${scope==='shared'?'共享':'本项目'}\${escapeHTML(assetKindLabel())}\`;
      if(!await afConfirm({message:\`确认删除\${label}「\${item.name}」？此操作不可恢复。\`}))return;
    `;
    const parts = extractDisplayStrings(src, { isHtml: false });
    const texts = new Set(parts.map((p) => p.text));
    assert.ok(
      [...texts].some((t) => t === "创建规则" || t === "创建技能"),
      `expected 创建规则 sample, got ${JSON.stringify([...texts])}`,
    );
    assert.ok(
      texts.has("本项目规则") || texts.has("共享规则"),
      `expected 本项目/共享规则 sample, got ${JSON.stringify([...texts])}`,
    );
    assert.ok(
      [...texts].some((t) => /确认删除规则「Foo」/.test(t)),
      `expected confirm sample with Foo name, got ${JSON.stringify([...texts])}`,
    );
    assert.equal(
      [...texts].some((t) => t === "规则创建规则"),
      false,
      "must not emit all-same multi-hole compound 规则创建规则",
    );
  });
});

describe("extractDisplayStrings integration", () => {
  it("splits HTML-in-JS templates into display pieces", () => {
    const src = `
      root.innerHTML = \`<div class="x" title="关闭"><b>保存成功</b><p>尚未翻译的句子啊</p></div>\`;
    `;
    const parts = extractDisplayStrings(src, { isHtml: false });
    const texts = parts.map((p) => p.text);
    assert.ok(texts.includes("关闭"));
    assert.ok(texts.includes("保存成功"));
    assert.ok(texts.includes("尚未翻译的句子啊"));
  });

  it("reads HTML file text and attrs", () => {
    const html = `<!doctype html><title>工作台</title><button aria-label="全局导航">进入</button>`;
    const parts = extractDisplayStrings(html, { isHtml: true });
    const texts = new Set(parts.map((p) => p.text));
    assert.ok(texts.has("工作台") || [...texts].some((t) => t.includes("工作台")));
    assert.ok(texts.has("全局导航"));
    assert.ok(texts.has("进入"));
  });
});
