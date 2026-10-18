# Thiết kế: gói tính năng ngoài Lập trình (`develop`)

Bản nháp để hội đồng xét, chưa phải ADR. Nền: ADR-090 (văn phòng AI cá nhân, code đi trước), ADR-161 (gói tính năng theo project, chỉ ẩn giao diện), `internal/features/features.go:31` (hiện chỉ có `develop` = `git`, `ops`).

## 1. Nhu cầu dùng AI theo ngành nghề

Dữ liệu tham khảo:
- Anthropic Economic Index: dùng AI tập trung ở phát triển phần mềm và viết lách/tài liệu; ~36% nghề dùng AI cho ≥ 1/4 công việc; 57% là "tăng cường" (người làm cùng AI), 43% là tự động.
- Khảo sát doanh nghiệp 2025–2026: agent được triển khai nhiều nhất ở CSKH (~49%), vận hành (~47%), quản lý dữ liệu (~47%); doanh thu tăng nhờ AI nhiều nhất ở marketing & bán hàng.

| Ngành / người dùng | Việc lặp lại AI làm được | Cần ra ngoài (duyệt) | Gói hợp |
|---|---|---|---|
| E-commerce / POD (vd shop Etsy) | trả lời ticket, mô tả sản phẩm, theo dõi đơn lỗi | gửi tin cho khách, đăng bài | Chăm sóc khách hàng, Sáng tạo nội dung |
| Agency / marketer / creator | bài FB, blog, lịch đăng, tái chế nội dung | đăng bài | Sáng tạo nội dung |
| Freelancer / tư vấn / sales | email follow-up, báo giá, ghi chú khách | gửi email | Chăm sóc khách hàng (sau: Bán hàng) |
| Kế toán / tài chính | báo cáo định kỳ, đối soát | không (chỉ đọc) | Không làm gói: báo cáo là việc chung, lõi (tự động hóa + MCP) đã làm được |
| Giáo dục / đào tạo | giáo án, bài tập, tóm tắt tài liệu | đăng lên LMS | Sáng tạo nội dung (sau: Giảng dạy) |
| Luật / y tế | tra cứu, tóm tắt hồ sơ | — | **Không làm**: rủi ro tuân thủ cao, ít hợp office cá nhân |

**Kết luận:** hai lĩnh vực có nhu cầu rõ nhất ngoài code là **chăm sóc khách hàng** và **sáng tạo nội dung**. Cả hai đều khớp với MCP đang dùng thật trên máy (Crisp, Discord, Telegram, Metabase, wiki), nên làm được mà không dựng hệ thống riêng (ADR-090).

## 2. Nguyên tắc

- **Gói = không gian làm việc riêng của một nghề**: có mục menu riêng trong sidebar của project (cạnh Chat, Tự động hóa, Vận hành; `dashboard/app/layouts/default.vue:88`) và trang riêng với dữ liệu riêng của nghề. Đây là thứ lõi và tự động hóa không có.
- Tự động hóa, quy trình, agent chỉ là **phần đi kèm**: chúng làm việc ngầm và ghi kết quả vào trang của gói. Ví dụ: tự động hóa nhận tin khách thì ghi vào Hộp thư; agent viết bài thì bài nằm trong Lịch đăng.
- Thứ chỉ cần chạy định kỳ rồi báo (báo cáo, review PR, cảnh báo) **không làm trang**, vì mẫu tự động hóa đã có.
- Hành động ra ngoài (gửi khách, đăng bài) **luôn qua thẻ duyệt** (ADR-090), ngay trong trang của gói.
- Mỗi gói 2–4 trang, chỉ thứ dùng hằng ngày.

## 3. Các gói

### 3.1 Chăm sóc khách hàng (`support`): menu **Khách hàng**
| Trang | Dạng | Làm gì |
|---|---|---|
| **Hộp thư** | 3 cột: danh sách hội thoại · hội thoại · thông tin khách | Tin từ mọi kênh (bot Discord/Telegram, Crisp, email qua MCP) gộp một chỗ. Trạng thái Mới / Đang xử lý / Chờ khách / Xong, gán người, lọc kênh. Mỗi tin khách có **nháp trả lời của AI** ngay ô nhập: sửa rồi Gửi (là duyệt). Badge quá hạn |
| **Khách hàng** | bảng + trang chi tiết | Hồ sơ khách: kênh, toàn bộ lịch sử trao đổi, ghi chú, nhãn (VIP, hay khiếu nại), đơn hàng đọc qua MCP |
| **Kiến thức** | danh sách mục có trình sửa | FAQ, chính sách, câu trả lời mẫu (macro). AI dùng khi soạn nháp; nút "Thêm vào kiến thức" từ một câu trả lời hay trong Hộp thư |

Đi kèm: mẫu `customer-routing`, `case-summary`, `agent-assist-quality`, `knowledge-refresh`; agent mẫu "CSKH".

### 3.2 Sáng tạo nội dung (`creator`): menu **Nội dung**
| Trang | Dạng | Làm gì |
|---|---|---|
| **Lịch đăng** | lịch tuần/tháng, kéo thả | Bài theo ngày giờ và kênh; kéo đổi ngày; đến giờ thì office đăng (bài đã duyệt) |
| **Bài viết** | bảng kanban Ý tưởng → Nháp → Chờ duyệt → Đã lên lịch → Đã đăng | Mỗi bài: một nội dung gốc, các **bản theo nền tảng** (FB, blog, TikTok, thread) với xem trước đúng khung từng nền tảng; nhờ AI viết lại từng bản; ảnh đính kèm; số liệu sau khi đăng |
| **Thương hiệu** | form | Giọng văn, đối tượng, từ nên/không dùng, các kênh đã kết nối. AI viết theo đây |
| **Thư viện** | lưới ảnh/video | Media của project, tạo ảnh AI, dùng lại cho bài |

Đi kèm: mẫu `research-digest` (đổ ý tưởng vào cột Ý tưởng), `content-calendar`, `campaign-report`; quy trình `/write-content`.

### 3.3 Lập trình (`develop`): menu **Lập trình**
Hiện gói chỉ ẩn/hiện thứ có sẵn (`git`, `ops`). Thêm một trang đặc thù:

| Trang | Dạng | Làm gì |
|---|---|---|
| **Bảng việc** | kanban Cần làm → Đang làm → Review → Đã gộp | Mỗi thẻ nối **issue** (Jira/GitHub, nếu có) ↔ **chat/Việc** của agent ↔ **worktree/nhánh** ↔ **diff/PR** ↔ **CI**. Kéo thẻ từ Cần làm sang Đang làm là giao cho agent. Nhìn một chỗ thấy agent nào đang làm gì, kẹt ở đâu |
| Vận hành | có sẵn | tiến trình, container, giám sát |

Đi kèm: mẫu `pr-review`, `release-notes`, `incident-triage`, `dependency-watch`.

### 3.4 Ứng viên sau
Bán hàng (pipeline khách tiềm năng, báo giá), Tuyển dụng (ứng viên, lịch phỏng vấn), Giảng dạy (lớp, bài tập, chấm bài). Chỉ làm khi có người dùng thật.

## 4. Thay đổi kỹ thuật

1. `internal/features`: thêm `support`, `creator`; trường **`DefaultOn bool`** vì hiện "thiếu key = bật" (`features.go:67`), gói mới phải mặc định **tắt**. `Pack` thêm `Templates []string` (key mẫu đi kèm).
2. Mỗi gói một package Go (`internal/support`, `internal/creator`) + migration riêng + API `/api/projects/{id}/support/...`, `/creator/...`. API trả 404 khi gói tắt (khác ADR-161 "chỉ ẩn giao diện", vì đây là dữ liệu riêng của gói).
3. Dashboard: mục menu và route `/projects/:id/support/*`, `/projects/:id/creator/*`, chỉ hiện khi gói bật (cách làm như `hasOps`, `layouts/default.vue:77`).
4. Agent ghi vào trang qua công cụ office mới theo gói (vd `support_reply_draft`, `creator_post_upsert`), chỉ có khi gói bật; prompt chỉ nhắc khi gói bật (ADR-163).
5. Hộp thư dùng lại chat của bot (mỗi hội thoại khách = một chat, thêm trạng thái/người phụ trách); gửi đi qua thẻ duyệt sẵn có.
6. Gói khởi tạo (`templates/packs/*.json`) chọn gói tính năng. ADR mới thay "Không làm: gói Nội dung/Khách hàng" và "chỉ ẩn giao diện" của ADR-161.

## 5. Lộ trình

| Bước | Việc | Kiểm chứng |
|---|---|---|
| 1 | `DefaultOn`, khung gói có menu/route/API riêng | `go test ./...`, typecheck; project cũ không đổi |
| 2 | CSKH: **Hộp thư** trên bot sẵn có | dùng thật với bot của 1 project ≥ 2 tuần |
| 3 | CSKH: Khách hàng, Kiến thức | nháp AI dùng được kiến thức |
| 4 | Nội dung: Bài viết + Lịch đăng | đăng thật 1 bài qua thẻ duyệt |
| 5 | Nội dung: Thương hiệu, Thư viện; Lập trình: Bảng việc | sau khi 2–4 dùng thật |

## 6. Không làm (lúc này)

Trang cho thứ chỉ chạy định kỳ rồi báo (đã có mẫu tự động hóa); luật, y tế, tài chính; CRM đầy đủ (deal, doanh số); dựng video; cài đặt gói chung cho cả office.

## Nguồn
- https://www.anthropic.com/research/economic-index-primitives
- https://chatmaxima.com/blog/ai-agent-statistics/
- https://conversionsystem.com/blog/rise-of-agentic-ai-2026
