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

  it("catches split syllables phiên/nhánh and bảnmô glue", () => {
    const cases = [
      "tạon hánh",
      "mởp hiên ↗",
      "chưa cóp hiên bảnmô tả",
      "phiên bảnmô tả",
      "không thể tạo tác vụ định kỳp hiên",
    ];
    for (const v of cases) {
      const issues = checkValue("x", v);
      assert.ok(
        hasType(issues, "dính"),
        `expected dính in ${JSON.stringify(v)}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("catches mid-string hiện tại X calque", () => {
    const cases = [
      ["打开当前会话 ↗", "mở hiện tại phiên ↗"],
      ["搜索当前项目的需求…", "tìm kiếm hiện tại dự án yêu cầu…"],
      ["无法确认当前分支：%w", "không thể xác nhận hiện tại nhánh: %w"],
    ];
    for (const [key, v] of cases) {
      const issues = checkValue(key, v);
      assert.ok(
        hasType(issues, "calque"),
        `expected calque in ${JSON.stringify(v)}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("catches Không gian làm việc terminology", () => {
    const issues = checkValue("LoopForge 工作台", "LoopForge · Không gian làm việc");
    assert.ok(
      hasType(issues, "term"),
      `expected term: ${JSON.stringify(issues)}`,
    );
  });

  it("catches broken Có thể't contraction", () => {
    for (const v of ["Có thể't xóa", "Có thể't xem trước: thiếu đường dẫn tệp"]) {
      const issues = checkValue("无法删除", v);
      assert.ok(
        hasType(issues, "broken"),
        `expected broken in ${JSON.stringify(v)}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("catches translated af-preview machine IDs", () => {
    const key =
      "平台节点「构建静态站点」不在角色列表里：id 固定为 af-preview-build，agentId 为 af-preview-builder。";
    const v =
      "ID cố định là af-xem trước-bản dựng và agentId là af-xem trước-builder.";
    const issues = checkValue(key, v);
    assert.ok(
      hasType(issues, "machine-id"),
      `expected machine-id: ${JSON.stringify(issues)}`,
    );
  });

  it("catches translated docs/*.md path", () => {
    const key =
      "协议及脚本见仓库 docs/external-workflow-usage.md。";
    const v = "Xem docs/external-quy trình-usage.md cho giao thức.";
    const issues = checkValue(key, v);
    assert.ok(
      hasType(issues, "docs-path"),
      `expected docs-path: ${JSON.stringify(issues)}`,
    );
  });

  it("catches tiêu đề= HTML attribute translation", () => {
    const issues = checkValue(
      '" title="前端预览" src="',
      '" tiêu đề="Xem trước giao diện" src="',
    );
    assert.ok(
      hasType(issues, "html-attr"),
      `expected html-attr: ${JSON.stringify(issues)}`,
    );
  });

  it("catches bản bản repetition", () => {
    const issues = checkValue("预览构建", "Xem trước bản bản dựng");
    assert.ok(
      hasType(issues, "calque"),
      `expected calque bản bản: ${JSON.stringify(issues)}`,
    );
  });

  it("catches standalone workspace terminology with trailing punct", () => {
    const issues = checkValue("这个会话还没有工作区。", "Phiên này chưa có workspace.");
    assert.ok(
      hasType(issues, "term"),
      `expected term workspace: ${JSON.stringify(issues)}`,
    );
  });

  it("catches mục 3 bad VI values (dính / máy thực thi)", () => {
    const bad = [
      ["项目共享执行机器", "dự án dùng chung máy thực thi"],
      ["无法加载工作区执行机器", "Không thể tải máy thực thi bàn làm việc"],
      ["使用执行机器的账号模型", "dùng máy thực thi tài khoản mô hình"],
      ["执行机器版本", "máy thực thi phiên bản"],
      ["等待云端执行机器", "Đang chờ trong đám mây runner"],
      ["暂无项目说明", "chưa có dự ánmô tả"],
    ];
    for (const [key, v] of bad) {
      const issues = checkValue(key, v);
      assert.ok(
        issues.length > 0,
        `expected issues for ${JSON.stringify(v)}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("catches JS identifier rewrite inside ${…}", () => {
    const issues = checkValue(
      "给${escapeHTML(kind)}起个名字",
      "Đặt tên cho ${escapeHTML(loại)}",
    );
    assert.ok(
      hasType(issues, "placeholder"),
      `expected placeholder parity for kind→loại: ${JSON.stringify(issues)}`,
    );
  });

  it("catches không đã tìm thấy / Mới nhất SOP calques", () => {
    const bad = [
      ["资产不存在或已删除", "Tài nguyên không đã tìm thấy hoặc đã xóa"],
      ["未找到该需求", "Yêu cầu không đã tìm thấy"],
      ["未找到最新 SOP", "Mới nhất SOP không đã tìm thấy"],
      ["其他项目还没有共享", "Không khác dự án có dùng chung"],
      ["运行不存在", "Chạy không đã tìm thấy"],
    ];
    for (const [key, v] of bad) {
      const issues = checkValue(key, v);
      assert.ok(
        hasType(issues, "calque"),
        `expected calque in ${JSON.stringify(v)}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("blacklist wins over allowlist when a word is on both", () => {
    // "Contents" is blacklisted; even a product-looking sentence must fail.
    const issues = checkValue(
      "管理员尚未配置 GitHub 授权时，也可以手动填写有 Contents 和 Pull requests 读写权限的 PAT",
      "Khi quản trị viên chưa cấu hình ủy quyền GitHub, cũng có thể điền thủ công PAT có quyền đọc/ghi Contents và yêu cầu kéo.",
    );
    assert.ok(
      hasType(issues, "blacklist"),
      `expected blacklist Contents: ${JSON.stringify(issues)}`,
    );
  });

  it("catches VI letter glued to latin tech token", () => {
    for (const v of ["tảiworkspace", "dùngSOP", "trênrunner", "mởGit"]) {
      const issues = checkValue("x", v);
      assert.ok(
        hasType(issues, "dính"),
        `expected dính in ${v}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("catches runner/máy chạy when key has 执行机器", () => {
    for (const v of ["Gỡ runner", "Cấu hình runner", "Dùng máy chạy"]) {
      const issues = checkValue("移除执行机器", v);
      assert.ok(
        hasType(issues, "term"),
        `expected term in ${v}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("catches thể hiện mistranslation when key contains 实例", () => {
    const cases = [
      [
        "这是这个实例的第一个账号，拥有全部管理权限，默认项目也归它。之后队友可以自己注册，开关在「平台管理」里。",
        "Đây là tài khoản đầu tiên trên thể hiện này. Tài khoản có toàn quyền quản trị.",
      ],
      ["个实例执行中", " thể hiện đang chạy"],
      ["个实例等待门禁", " thể hiện đang chờ cổng duyệt"],
      // Capitalized form must fail too (UI often starts a sentence with Thể hiện).
      ["当前实例", "Thể hiện hiện tại"],
      ["本实例未启用 scheduler", "Thể hiện này chưa bật bộ lập lịch"],
    ];
    for (const [key, v] of cases) {
      const issues = checkValue(key, v);
      assert.ok(
        hasType(issues, "calque"),
        `expected calque thể hiện for 实例 in ${JSON.stringify(v)}: ${JSON.stringify(issues)}`,
      );
      assert.ok(
        issues.some((i) => i.detail.includes("thể hiện")),
        `expected thể hiện detail: ${JSON.stringify(issues)}`,
      );
    }
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

  it("passes fixed phiên/nhánh and bàn làm việc strings", () => {
    const good = [
      ["创建分支", "Tạo nhánh"],
      ["打开会话 ↗", "Mở phiên ↗"],
      ["暂无版本说明", "Chưa có mô tả phiên bản"],
      ["版本说明", "Mô tả phiên bản"],
      ["打开当前会话 ↗", "Mở phiên hiện tại ↗"],
      ["无法确认当前分支：%w", "Không thể xác nhận nhánh hiện tại: %w"],
      ["当前版本", "Phiên bản hiện tại"],
      ["当前没有进行中的需求", "Hiện không có yêu cầu đang chạy"],
      ["LoopForge 工作台", "LoopForge · Bàn làm việc"],
      ["%s：继续已有分支 %s，MR 目标主分支 %s", "Tiếp tục nhánh đã có %s"],
    ];
    for (const [key, value] of good) {
      let k = key;
      if (value === "Tiếp tục nhánh đã có %s") k = "%s";
      const issues = checkValue(k, value);
      assert.deepEqual(
        issues,
        [],
        `unexpected issues for ${value}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("passes fixed Can't / af-preview / docs / title= / bản strings", () => {
    const good = [
      ["无法删除", "Không thể xóa"],
      ["无法预览：缺少文件路径", "Không thể xem trước: thiếu đường dẫn tệp"],
      ["无法预览：未选中技能", "Không thể xem trước: chưa chọn Skill"],
      [
        "平台节点「构建静态站点」不在角色列表里：id 固定为 af-preview-build，agentId 为 af-preview-builder。",
        "ID cố định là af-preview-build và agentId là af-preview-builder.",
      ],
      [
        "协议及脚本见仓库 docs/external-workflow-usage.md。",
        "Xem docs/external-workflow-usage.md cho giao thức và tập lệnh.",
      ],
      ['" title="前端预览" src="', '" title="Xem trước giao diện" src="'],
      ['" title="删除">', '" title="Xóa">'],
      ['" title="打开运行', '" title="Mở lần chạy'],
      ["预览构建", "Xem trước bản dựng"],
      ["这个会话还没有工作区。", "Phiên này chưa có bàn làm việc."],
    ];
    for (const [key, value] of good) {
      const issues = checkValue(key, value);
      assert.deepEqual(
        issues,
        [],
        `unexpected issues for ${value}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("passes mục 3 suggested máy thực thi / grammar fixes", () => {
    const good = [
      ["项目共享执行机器", "Máy thực thi dùng chung của dự án"],
      ["无法加载工作区执行机器", "Không thể tải máy thực thi của bàn làm việc"],
      ["使用执行机器的账号模型", "Dùng mô hình theo tài khoản trên máy thực thi"],
      ["执行机器版本", "Phiên bản máy thực thi"],
      ["等待云端执行机器", "Đang chờ máy thực thi trên đám mây"],
      ["暂无项目说明", "Chưa có mô tả dự án"],
    ];
    for (const [key, value] of good) {
      const issues = checkValue(key, value);
      assert.deepEqual(
        issues,
        [],
        `unexpected issues for ${value}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("passes fixed not-found calques and intact ${kind}", () => {
    const good = [
      ["给${escapeHTML(kind)}起个名字", "Đặt tên cho ${escapeHTML(kind)}"],
      ["${escapeHTML(kind)}内容", "Nội dung ${escapeHTML(kind)}"],
      ["资产不存在或已删除", "Không tìm thấy tài nguyên hoặc đã bị xóa"],
      ["未找到该需求", "Không tìm thấy yêu cầu"],
      ["未找到最新 SOP", "Không tìm thấy SOP mới nhất"],
      ["其他项目还没有共享", "Các dự án khác chưa chia sẻ "],
      ["运行不存在", "Không tìm thấy lần chạy"],
      ["没有找到指定的 MCP server", "Không tìm thấy máy chủ MCP đã chỉ định"],
    ];
    for (const [key, value] of good) {
      const issues = checkValue(key, value);
      assert.deepEqual(
        issues,
        [],
        `unexpected issues for ${value}: ${JSON.stringify(issues)}`,
      );
    }
  });

  it("passes fixed 实例 translations (not thể hiện)", () => {
    const good = [
      [
        "这是这个实例的第一个账号，拥有全部管理权限，默认项目也归它。之后队友可以自己注册，开关在「平台管理」里。",
        "Đây là tài khoản đầu tiên trên máy chủ LoopForge này. Tài khoản có toàn quyền quản trị và sở hữu dự án mặc định. Sau này đồng đội có thể tự đăng ký; bật/tắt trong “Quản trị nền tảng”.",
      ],
      ["个实例执行中", " lần chạy đang thực thi"],
      ["个实例等待门禁", " lần chạy đang chờ cổng duyệt"],
      ["当前实例", "Phiên bản hiện tại"],
      ["本实例未启用 scheduler", "Phiên bản này chưa bật bộ lập lịch"],
      // "thể hiện" OK when key means display/表现, not 实例
      ["显示方式", "Cách thể hiện"],
    ];
    for (const [key, value] of good) {
      const issues = checkValue(key, value);
      assert.deepEqual(
        issues,
        [],
        `unexpected issues for ${value}: ${JSON.stringify(issues)}`,
      );
    }
  });
});
