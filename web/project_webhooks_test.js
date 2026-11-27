/**
 * Smoke: project admin exposes a Webhooks tab and panel helpers.
 * Run: node --test web/project_webhooks_test.js
 */
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)));

test("projects admin lists Webhook tab", () => {
  const src = readFileSync(join(root, "app/projects.js"), "utf8");
  assert.match(src, /id:'webhooks'/);
  assert.match(src, /projectWebhooksPanelHTML/);
  assert.match(src, /loadProjectWebhooks/);
});

test("project-webhooks.js exports panel helpers", () => {
  const src = readFileSync(join(root, "app/project-webhooks.js"), "utf8");
  assert.match(src, /window\.projectWebhooksPanelHTML/);
  assert.match(src, /rotate-secret/);
  assert.match(src, /Idempotency|HMAC|authMode/);
});

test("index.html loads project-webhooks.js", () => {
  const html = readFileSync(join(root, "index.html"), "utf8");
  assert.match(html, /project-webhooks\.js/);
});
