#!/usr/bin/env node
/**
 * Unit tests for i18n-vi-check rules (dính, calque, placeholder, acronym).
 * Run: node --test scripts/i18n-vi-check.test.mjs
 */
import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { checkValue } from "./i18n-vi-check.mjs";

function types(issues) {
  return issues.map((i) => i.type);
}

function hasType(issues, type) {
  return issues.some((i) => i.type === type);
}

describe("i18n-vi-check must-fail cases (review findings)", () => {
  it("catches calque + rest blacklist", () => {
    const v = "Đầu tiên dòng là Issue tiêu đề; rest là mô tả";
    const issues = checkValue("第一行作为 Issue 标题，其余作为描述", v);
    assert.ok(hasType(issues, "calque"), `expected calque in ${JSON.stringify(issues)}`);
    assert.ok(
      hasType(issues, "blacklist") || hasType(issues, "acronym-case"),
      `expected blacklist/acronym for rest: ${JSON.stringify(issues)}`,
    );
  });

  it("catches đầu tiên dòng after SOP", () => {
    const v = "Chọn SOP, đầu tiên dòng là tiêu đề";
    const issues = checkValue("选 SOP，第一行写标题", v);
    assert.ok(hasType(issues, "calque"), `expected calque in ${JSON.stringify(issues)}`);
  });

  it("catches glued tiêunhánh", () => {
    for (const v of ["mục tiêunhánh", "mục tiêunhánh chính"]) {
      const issues = checkValue("目标分支", v);
      assert.ok(hasType(issues, "dính"), `expected dính in ${v}: ${JSON.stringify(issues)}`);
    }
  });

  it("catches glued giaotiêu / đềkhông / là trống", () => {
    const v = "sản phẩm bàn giaotiêu đềkhông thể là trống";
    const issues = checkValue("产物标题不能为空", v);
    assert.ok(hasType(issues, "dính"), `expected dính in ${JSON.stringify(issues)}`);
    assert.ok(hasType(issues, "calque"), `expected calque in ${JSON.stringify(issues)}`);
  });

  it("catches glued phiêntiêu", () => {
    const v = "hãy điền dự án và phiêntiêu đề";
    const issues = checkValue("请填写项目和会话标题", v);
    assert.ok(hasType(issues, "dính"), `expected dính in ${JSON.stringify(issues)}`);
  });

  it("catches placeholder glue and dính on task strings", () => {
    const cases = [
      ["%s 已完成任务：%s", "hoàn tấttác vụ:%s"],
      ["LoopForge: 对齐 %s → %s", "LoopForge: căn chỉnh%s →%s"],
      ["共享状态的%s不能删除，请先取消共享", "dùng chungtrạng thái%skhông thể xoá"],
      ["会话描述：\n%s", "phiênmô tả:%s"],
    ];
    for (const [key, v] of cases) {
      const issues = checkValue(key, v);
      assert.ok(
        hasType(issues, "placeholder") || hasType(issues, "dính"),
        `expected placeholder/dính in ${JSON.stringify(v)} => ${JSON.stringify(issues)}`,
      );
    }
  });

  it("catches mustache name rewrite {{thượng nguồn}} vs {{upstream}}", () => {
    const key = "## 上游节点输出\n\n{{upstream}}";
    const v = "## Đầu ra bước thượng nguồn\n\n{{thượng nguồn}}";
    const issues = checkValue(key, v);
    assert.ok(
      hasType(issues, "placeholder"),
      `expected placeholder for mustache name mismatch: ${JSON.stringify(issues)}`,
    );
  });

  it("catches hiện tại … prefix calque", () => {
    const issues = checkValue("当前版本", "hiện tại phiên bản");
    assert.ok(hasType(issues, "calque"), `expected calque: ${JSON.stringify(issues)}`);
  });

  it("catches Này … / có không khả dụng calque", () => {
    const issues = checkValue(
      "这个项目还没有可用的 SOP",
      "Này dự án có không khả dụng SOPs",
    );
    assert.ok(hasType(issues, "calque"), `expected calque: ${JSON.stringify(issues)}`);
  });

  it("catches letter glued to curly quotes", () => {
    const issues = checkValue(
      "需求「{{issue.title}}」已完成",
      "yêu cầu“{{issue.title}}”đã hoàn tất",
    );
    assert.ok(
      hasType(issues, "quote"),
      `expected quote glue: ${JSON.stringify(issues)}`,
    );
  });
});

describe("i18n-vi-check must-pass cases", () => {
  const good = [
    ["第一行作为 Issue 标题，其余作为描述", "Dòng đầu là tiêu đề Issue; phần còn lại là mô tả"],
    ["选 SOP，第一行写标题", "Chọn SOP, dòng đầu là tiêu đề"],
    ["目标分支", "Nhánh mục tiêu"],
    ["产物标题不能为空", "Tiêu đề artifact không được để trống"],
    ["请填写项目和会话标题", "Hãy điền dự án và tiêu đề phiên"],
    ["%s 已完成任务：%s", "Hoàn tất tác vụ: %s"],
    ["LoopForge: 对齐 %s → %s", "LoopForge: căn chỉnh %s → %s"],
    ["call", "Gọi REST API qua MCP"],
    ["login", "Đăng nhập GitHub"],
  ];

  for (const [key, value] of good) {
    it(`passes: ${value.slice(0, 40)}`, () => {
      // Adjust placeholder parity for shortened samples
      let k = key;
      if (value === "Hoàn tất tác vụ: %s") k = "%s";
      if (value === "Gọi REST API qua MCP") k = "call";
      if (value === "Đăng nhập GitHub") k = "login";
      if (value === "LoopForge: căn chỉnh %s → %s") k = "LoopForge: 对齐 %s → %s";
      const issues = checkValue(k, value);
      assert.deepEqual(issues, [], `unexpected issues for ${value}: ${JSON.stringify(issues)}`);
    });
  }

  it("passes full two-placeholder completion string", () => {
    const issues = checkValue(
      "%s 已完成任务：%s",
      "%s đã hoàn tất tác vụ: %s",
    );
    assert.deepEqual(types(issues), []);
  });

  it("passes exact mustache {{upstream}}", () => {
    const issues = checkValue(
      "## 上游节点输出\n\n{{upstream}}",
      "## Đầu ra bước thượng nguồn\n\n{{upstream}}",
    );
    assert.deepEqual(issues, [], `unexpected: ${JSON.stringify(issues)}`);
  });

  it("passes Phiên bản hiện tại (not hiện tại prefix)", () => {
    const issues = checkValue("当前版本", "Phiên bản hiện tại");
    assert.deepEqual(issues, [], `unexpected: ${JSON.stringify(issues)}`);
  });

  it("passes Hiện không có yêu cầu đang chạy", () => {
    const issues = checkValue("当前没有进行中的需求", "Hiện không có yêu cầu đang chạy");
    assert.deepEqual(issues, [], `unexpected: ${JSON.stringify(issues)}`);
  });

  it("passes spaced curly quotes around mustache", () => {
    const issues = checkValue(
      "需求「{{issue.title}}」已完成",
      "Yêu cầu “{{issue.title}}” đã hoàn tất",
    );
    assert.deepEqual(issues, [], `unexpected: ${JSON.stringify(issues)}`);
  });
});
