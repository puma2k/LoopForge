# Inbound Webhook theo dự án

LoopForge cho phép mỗi dự án tạo nhiều **kênh webhook inbound**. Hệ thống bên ngoài `POST` JSON vào URL riêng của kênh để tạo yêu cầu (requirement) trong đúng project, và (tuỳ chọn) tự chạy SOP mặc định.

> **EN (short):** Per-project inbound webhooks with unguessable URL + HMAC/token auth, replay protection, rate limits, idempotency, and optional SOP auto-dispatch. Secrets are shown once on create/rotate.

## 1. Tạo kênh trên UI

1. Mở **Quản trị dự án** → tab **Webhook**.
2. Chỉ **Owner**, **maintainer** hoặc platform admin mới tạo/sửa/xoá/xoay secret.
3. **＋ Tạo kênh**, điền tên, cách xác thực (mặc định HMAC-SHA256), SOP mặc định, người phụ trách, template title/description, bật/tắt **Tự chạy SOP**.
4. Sau khi tạo, **sao chép ngay** Inbound URL và Secret (chỉ hiện một lần). Delivery token nằm trong path URL.

Migration thủ công: `migrations/043_project_inbound_webhooks.sql`.

## 2. URL và Secret

- **URL:** `POST {PUBLIC_URL}/api/webhooks/inbound/{deliveryToken}`
  - `deliveryToken` không đoán được, gắn đúng 1 project + 1 kênh.
  - Không dùng cookie/session đăng nhập.
- **Secret:** lưu hashed + sealed (credcrypt). Sau lần hiện duy nhất lúc tạo/xoay chỉ còn prefix mask.
- **Xoay secret:** secret mới có hiệu lực ngay; secret cũ còn cửa sổ ân hạn ~15 phút.

### Cách xác thực (`authMode`)

| Mode | Header |
| --- | --- |
| `hmac` (mặc định) | `X-LoopForge-Signature: sha256=<hex(HMAC-SHA256(secret, rawBody))>` |
| `token` | `X-LoopForge-Token: <secret>` hoặc `Authorization: Bearer <secret>` |
| `hmac_or_token` | HMAC hoặc token |

Bắt buộc thêm (chống replay):

- `X-LoopForge-Timestamp`: Unix giây, cửa sổ ±5 phút
- `X-LoopForge-Nonce`: chuỗi duy nhất theo kênh (nonce đã dùng → 401)

Tuỳ chọn:

- `Idempotency-Key`: trùng key trong 24h → trả cùng issue, không tạo trùng

## 3. Ví dụ `curl` (HMAC)

```bash
BODY='{"title":"Fix login flake","body":"CI failed on suite A"}'
TS=$(date +%s)
NONCE=$(openssl rand -hex 16)
# SECRET và DELIVERY_TOKEN chỉ là placeholder — dùng giá trị từ UI
SIG=$(printf '%s' "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')

curl -sS -X POST "$PUBLIC_URL/api/webhooks/inbound/$DELIVERY_TOKEN" \
  -H 'Content-Type: application/json' \
  -H "X-LoopForge-Timestamp: $TS" \
  -H "X-LoopForge-Nonce: $NONCE" \
  -H "X-LoopForge-Signature: sha256=$SIG" \
  -H "Idempotency-Key: ci-build-12345" \
  --data-binary "$BODY"
```

Phản hồi thành công (rút gọn):

```json
{ "issueId": "issue-…", "number": 42, "eventId": "whe-…", "channelId": "whc-…", "taskId": "…", "runId": "…" }
```

Không bao giờ trả secret.

## 4. Template title / description

- Token dạng `{{title}}`, `{{.issue.title}}`, `{{payload.body}}` (dot-path trong JSON).
- Nếu template trống hoặc field thiếu:
  - **Title:** `title` → `summary` → `subject` → mặc định `[tên kênh] inbound <RFC3339>`
  - **Description:** `description`/`body`/… hoặc JSON đã tóm tắt (giới hạn kích thước)

Issue `source=webhook`, `externalRef` gắn `channelId/eventId`.

## 5. Tự chạy SOP

Khi kênh bật **Auto-run SOP**:

1. Tạo issue với `workflowId` = SOP mặc định của kênh.
2. Gọi cùng cơ chế `dispatch` như `POST /api/issues/{id}/dispatch` (`triggerOrigin=webhook`).
3. Nếu thiếu SOP hoặc dispatch lỗi: **vẫn giữ issue**, ghi comment/log sự kiện, trả **HTTP 502** (không “im lặng thành công”).

Kênh tắt → **403**. Payload > hạn (mặc định 256KB, trần 1MB) → **413**. Rate limit theo kênh/IP → **429** + `Retry-After`.

## 6. Trigger / node trong SOP

SOP hiện có (`code-review-merge-auto`, …) chạy bình thường khi kênh trỏ tới chúng — payload webhook là input bổ sung, **không bắt buộc** node webhook mới.

Tuỳ chọn khai báo node trigger trong JSON workflow:

```json
{
  "id": "from-webhook",
  "name": "Webhook inbound",
  "type": "webhook"
}
```

(`webhook` hoặc `webhook_inbound` — no-op thành công ngay.)

Biến ổn định trong run input / prompt:

| Biến | Ý nghĩa |
| --- | --- |
| `webhook.channelId` | ID kênh |
| `webhook.eventId` | ID sự kiện |
| `webhook.payload` | JSON body (đã parse) |
| `webhook.headers` | Header đã lọc (không có Authorization/signature/token) |

Placeholder prompt: `{{webhook.payload}}`, `{{webhook.headers}}`, `{{webhook.channelId}}`, `{{webhook.eventId}}`.

## 7. Bảo mật

- Endpoint public tách biệt; không nới ACL/CSRF của API đã đăng nhập.
- So khớp chữ ký / token timing-safe; reject 401 khi sai/thiếu.
- Nonce + timestamp chống replay.
- Rate limit theo kênh và IP.
- Log sự kiện chỉ metadata (channel, issue, status, lý do) — **không** log secret/chữ ký/Authorization.
- Secret hashed + sealed; rotate không làm gián đoạn request hợp lệ dùng secret cũ trong cửa sổ ân hạn.

## 8. Xử lý sự cố

| HTTP | Nguyên nhân thường gặp |
| --- | --- |
| 401 | Sai/thiếu chữ ký hoặc token; timestamp ngoài cửa sổ; nonce trùng; URL token sai |
| 403 | Kênh đã tắt |
| 409 | (idempotency/conflict khác — webhook idempotent trả 200 cùng issue) |
| 413 | Body vượt hạn |
| 415 | `Content-Type` không phải `application/json` |
| 429 | Rate limit — xem `Retry-After` |
| 502 | Issue đã tạo nhưng auto-dispatch SOP thất bại |

Xem log gần đây trên UI kênh (tab Webhook → Sự kiện).

## 9. API quản trị (tóm tắt)

| Method | Path |
| --- | --- |
| GET/POST | `/api/projects/{id}/webhooks` |
| GET/PUT/DELETE | `/api/projects/{id}/webhooks/{channelId}` |
| POST | `/api/projects/{id}/webhooks/{channelId}/rotate-secret` |
| GET | `/api/projects/{id}/webhooks/{channelId}/events` |
| POST | `/api/webhooks/inbound/{token}` (public) |

Chi tiết schema: `docs/api/openapi.yaml`.
