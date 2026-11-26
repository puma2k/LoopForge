# Discord Bot（Gateway）

Hướng dẫn cấu hình kênh Discord cho LoopForge. Platform dùng **Gateway WebSocket** (thư viện `gorilla/websocket` sẵn trong `go.mod`), **không** dùng `discordgo`.

## English summary

1. Create an application in the [Discord Developer Portal](https://discord.com/developers/applications), enable a Bot, copy **Application ID** and **Bot Token**.
2. OAuth2 scopes: `bot`, `applications.commands`. Privileged **Message Content Intent** is **not** required — LoopForge uses slash commands over Gateway (`Guilds` intent only).
3. Invite the bot to your guild; copy the **Guild ID**.
4. In LoopForge → Channels → Discord: paste Application ID, Bot Token, Guild ID; optional Public Key, allowed channel IDs, default SOP. Set write policy to **网页确认** or **自动执行** if you want `/task` to create work (default is read-only).
5. Bind with `/link` (optional `code`), create work with `/task …`, ask with `/ask`.

Token is never returned in API JSON; responses may show `secretHint` like `••••abcd`.

## 1. Developer Portal

1. Tạo **Application** → tab **Bot** → Reset Token, lưu Bot Token (chỉ hiện một lần).
2. Intent tối thiểu: **Guilds** (Gateway Identify). Không cần Message Content Intent — lệnh là **slash command**, không parse tin nhắn thường.
3. OAuth2 → URL Generator: scope `bot` + `applications.commands`; chọn quyền gửi tin nhắn / tạo public thread theo nhu cầu.
4. Mở URL invite, thêm bot vào server (guild). Bật Developer Mode trong Discord client, chuột phải server → Copy Server ID (= Guild ID).

## 2. Cấu hình trên LoopForge

| Trường | Bắt buộc | Ghi chú |
| --- | --- | --- |
| Application ID | Có | ID ứng dụng trên Developer Portal |
| Bot Token | Có | Lưu mã hóa; API chỉ trả `secretHint` |
| Guild ID | Có | Khóa tenant như CorpId của DingTalk; sự kiện guild khớp sẽ tự khóa |
| Public Key | Không | Hex, dùng xác minh Interactions HTTP nếu bật |
| Allowed channel IDs | Không | Danh sách ID kênh (tối đa ~50); trống = mọi kênh trong guild |
| Default SOP | Không | Workflow cho `/task` khi không có map theo kênh |
| Write approval | Không | Mặc định **只读**; `/task` cần **网页确认** (draft trên web) hoặc **自动执行** |

Luồng bước: ① Điền credentials → ② Xác nhận server (auto-lock khi Guild khớp) → ③ Người nhận thông báo **tuỳ chọn** (Discord `/task` không phụ thuộc).

Điều kiện môi trường chung (`AGENTFLOW_PUBLIC_URL`, `AGENTFLOW_CREDENTIAL_KEYS`, …) xem [im-channels.md](im-channels.md).

## 3. Lệnh trong Discord

| Lệnh | Ý nghĩa |
| --- | --- |
| `/link` | Xin liên kết tài khoản nền tảng (ephemeral) |
| `/link code:` | Xác nhận mã một lần từ trang bind (tuỳ chọn `code` trên slash command) |
| `/task mô tả` | Tạo requirement local (khi write policy cho phép), gán user đã bind, dispatch SOP (channel map → default SOP → SOP dự án); sau khi bind thành công mới mở **thread** công khai để đăng tiến độ |
| `/ask` | Hội thoại qua model (cần đã bind; ephemeral) |
| `/unbind` | Gỡ liên kết (nếu gửi qua kênh hỗ trợ lệnh văn bản tương đương) |

User chưa bind nhận hướng dẫn + URL **ephemeral** (không Markdown, không thread công khai).

Runtime hiện tại chỉ xử lý **slash command** qua Gateway; không đăng ký Message Content / `MESSAGE_CREATE`.

## 4. SOP và `/task`

`/task` dùng cùng semantics dispatch SOP như các kênh IM khác khi write policy là **自动执行**: tạo issue `kind=requirement` `source=local`, rồi `dispatchIssue` với `TriggerUserID` của user đã bind và `ChatOrigin` (ChatID = thread tiến độ nếu tạo được, còn map SOP vẫn theo **channel gốc**). Ở chế độ **网页确认**, `/task` chỉ tạo draft `issues_save` để xác nhận trên web. Ở **只读**, `/task` bị từ chối. Chi tiết điều khiển SOP xem [bot-sop-controls.md](bot-sop-controls.md).

## 5. Troubleshooting

| Hiện tượng | Kiểm tra |
| --- | --- |
| Lưu Bot báo `credential_invalid` | Token sai / Application ID không khớp bot |
| `tenant_required` | Thiếu Guild ID |
| Server conflict | Event đến từ guild khác với Guild ID đã điền |
| `/task` báo chỉ đọc | Write approval đang `readonly` — đổi sang confirm/auto |
| `/task` không tạo issue | User chưa `/link`; hoặc Bot tắt interaction |
| Không gửi được mã bind | Dùng `/link` với option `code`, không gõ tin nhắn thường |
| Link bind hỏng | `AGENTFLOW_PUBLIC_URL` / địa chỉ công khai phải mở được từ Discord |
| Token lộ trong log | Adapter Discord đã redact; không log raw token |

## 6. Vì sao Gateway, không discordgo

Gateway Discord là WebSocket Identify/Heartbeat/Dispatch đơn giản. Repo đã phụ thuộc `gorilla/websocket` cho các kênh khác; adapter Discord tái sử dụng stack đó để tránh thêm dependency lớn (`discordgo`) và giữ kiểm soát redact / allowlist / rate-limit trong code nội bộ.
