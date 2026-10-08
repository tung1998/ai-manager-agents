# Burn: agent chạy liên tục để tự tìm và làm việc trong một project

## Mục tiêu

Trong một project, admin bật một **phiên Burn**: một agent chính chạy liên tục với quyền admin, tự tìm và làm việc, cho tới khi tắt hoặc tới giờ hẹn. Agent tìm 3 loại việc:

- việc dang dở,
- chỗ có thể nâng cấp,
- lỗi chi tiết.

Mỗi việc làm trên một worktree riêng. Tắt thì việc đang làm tạm dừng; bật lại thì làm tiếp.

## Phạm vi

- **Trang Burn riêng** trong project (menu project, cạnh Tự động, Agents…): bật/tắt, hẹn giờ, cài đặt, bảng việc. Mỗi project có tối đa một phiên Burn đang chạy.
- **Trang Chat** chỉ hiện hội thoại của phiên Burn như một loại chat (biểu tượng ngọn lửa, có trong bộ lọc nguồn). Người dùng xem agent làm gì và nhắn để chỉnh hướng ("ưu tiên phần thanh toán"). Không có nút điều khiển Burn ở trang Chat.
- **Chỉ admin** được bật. Không có giới hạn chi phí hay lượt chạy, theo ADR-080; điểm dừng duy nhất là hẹn giờ hoặc tắt tay.

## Bật phiên

Trang Burn → nút **Bật** → hộp xác nhận ghi rõ:

- Agent chạy liên tục với **toàn quyền máy** (Bash, sửa file mọi nơi), không hỏi duyệt từng bước.
- Tiêu tốn nhiều lượt dùng của kết nối AI.
- Kết quả đi đâu (theo chế độ đã chọn).
- Giờ tắt.

Bấm xác nhận thì phiên mới chạy.

## Cài đặt của phiên

Lưu theo project, sửa được khi đang tắt; một số mục sửa được cả khi đang chạy:

| Cài đặt | Giá trị | Mặc định |
|---|---|---|
| Agent chính | một agent của project | Trưởng nhóm |
| Mức tiêu (model) | strong / balanced / fast | balanced |
| Số việc chạy cùng lúc (ADR-117) | 1–5 | 1 |
| Kết quả mỗi việc | **nhánh riêng** (commit local) / **diff chờ duyệt** | nhánh riêng |
| Tắt lúc | sau X giờ / tới giờ cụ thể / không hẹn | lần reset giới hạn tuần tiếp theo của kết nối AI mà agent chính dùng (`seven_day.resets_at`); không có dữ liệu thì sau 8 giờ |
| Trọng tâm (tùy chọn) | chữ tự do tới 2000 ký tự, có gợi ý Bảo mật / UI/UX / Hiệu năng / Test; chi phối quét, làm, review (ADR-120) | trống |
| Gửi tổng kết khi dừng | một bot của project + chat ID (Discord/Telegram) | chỉ trong chat Burn |

## Cách chạy

Một tiến trình nền cho mỗi phiên đang chạy, gọi là **burn runner**. Nó lặp lại hai loại lượt trong cùng hội thoại Burn.

### 1. Lượt điều phối (agent chính)

Agent chính nhận:

- trạng thái phiên,
- danh sách việc hiện có (tìm thấy, đang làm, tạm dừng, xong, bỏ qua),
- trọng tâm của phiên,
- tin nhắn mới của người dùng.

Nhiệm vụ của lượt này:

- **Quét** khi danh sách còn ít việc "tìm thấy":
  - TODO/FIXME, nhánh làm dở, test và build đang fail;
  - đề xuất còn treo trong các chat của project;
  - chỗ có thể nâng cấp;
  - lỗi chi tiết (đọc code, chạy test, soi log).
- **Ghi việc** bằng công cụ `burn_add(title, kind, detail)`, với `kind` là `unfinished` | `upgrade` | `bug`. Có chống trùng theo tiêu đề và nội dung gần giống.
- **Chọn việc tiếp theo** bằng `burn_pick(item_id)`, hoặc `burn_skip(item_id, lý do)`.

Chỉ agent chính quyết việc nào chạy, để các việc không giẫm lên nhau.

### 2. Lượt làm việc (theo từng việc)

- Runner tạo **worktree riêng** cho việc vừa chọn, trên nhánh `burn/<mã việc>-<tên ngắn>` tách từ nhánh hiện tại của project.
- Runner chạy agent chính trong worktree đó với **toàn quyền** và mức model đã chọn.
- Agent được dùng subagent (công cụ Agent/Task của Claude Code), office không đặt số. Office đếm số lần gọi công cụ đó và hiện trên bảng việc.
- Lượt kết thúc bằng `burn_done(item_id, tóm tắt)` hoặc `burn_fail(item_id, lý do)`.
- Theo chế độ kết quả:
  - **Nhánh riêng:** runner commit mọi thay đổi còn lại trong worktree lên nhánh của việc, với thông điệp là tóm tắt. Không push.
  - **Diff chờ duyệt:** runner tạo diff (patch) như chat thường, hiện trong Cần xử lý.
- Xong một việc thì quay về lượt điều phối.

Tối đa "số việc chạy cùng lúc" việc được làm song song, mỗi việc trong worktree và chat riêng (ADR-117); lượt quét chạy khi còn chỗ trống và chọn tối đa số chỗ đó.

## Tắt, tạm dừng, làm tiếp

- **Tắt** (bằng tay, ADR-118): phiên chuyển sang **đang hoàn thành nốt**: không quét, không nhận việc mới, làm xong các việc đang làm, tạm dừng hoặc chờ review kết quả rồi tự sang **đã tắt**. Trong lúc đó có hai nút: **Tiếp tục** (chạy lại như trước) và **Dừng hẳn** (như dưới).
- **Dừng hẳn** (bằng tay hoặc tới giờ hẹn): hủy lượt đang chạy. Việc đang làm chuyển sang **tạm dừng**; giữ nguyên worktree, nhánh và những gì đã sửa. Phiên chuyển sang **đã tắt**.
- **Bật lại:** runner ưu tiên việc tạm dừng. Lượt làm việc chạy lại trong **đúng worktree cũ**, với lời dặn "đây là việc đang dở; xem `git status`/`git diff` và làm tiếp".
- **Office khởi động lại khi đang chạy:** phiên vẫn ở trạng thái "đang chạy", runner tự chạy tiếp (việc dở thành tạm dừng rồi được làm tiếp ngay).
- **Chạm giới hạn của kết nối AI** (hết hạn mức 5 giờ hay hạn mức tuần): phiên **chờ** tới lúc reset rồi tự chạy tiếp, trừ khi đã quá giờ tắt.

## Dữ liệu

- Hội thoại Burn là một conversation với `purpose = "burn"`. Trang Chat hiển thị nó với nguồn "Burn".
- Bảng `burn_sessions`:
  - `id`, `project_id`, `conversation_id`, `agent_id`,
  - `model_tier`, `max_parallel`, `result_mode` (`branch` | `patch`), `focus`,
  - `ends_at` (null = không hẹn), `state` (`running` | `stopped` | `waiting_limit`),
  - `started_by`, `created_at`, `updated_at`.
- Bảng `burn_items`:
  - `id`, `session_id`, `title`, `kind`, `detail`,
  - `status` (`found` | `doing` | `paused` | `done` | `failed` | `skipped`),
  - `branch`, `worktree`, `summary`, `subagents`, `cost_usd`,
  - `created_at`, `updated_at`.
- Thay đổi được đẩy qua SSE (`burn_sessions`, `burn_items` thuộc danh sách bảng theo dõi), nên trang Burn cập nhật trực tiếp.

## Trang Burn

- **Đầu trang:** trạng thái (đang chạy / đã tắt / chờ reset giới hạn), đồng hồ đếm ngược tới giờ tắt, nút Bật/Tắt, cài đặt (mở trong ngăn bên). Link "Mở hội thoại" sang trang Chat.
- **Bảng việc** theo cột: Tìm thấy · Đang làm · Tạm dừng · Xong · Thất bại / Bỏ qua. Mỗi việc hiện tiêu đề, loại, nhánh, tóm tắt, chi phí, số subagent. Thao tác trên từng việc:
  - Bỏ qua.
  - Ưu tiên làm tiếp theo.
  - Mở nhánh: copy tên nhánh và lệnh `git switch`.
  - Xóa worktree: với việc đã xong và đã merge.
- **Lịch sử phiên:** các lần bật, tắt, thời gian chạy, số việc xong.

## Review (ADR-112, ADR-113)

- Burn chọn một **hồ sơ review** (trang riêng `/projects/:id/burn/reviews`): review **vấn đề**, **cách làm** (trước khi làm) và **kết quả** (sau khi xong), mỗi bước có agent review và quy trình review riêng. Không chọn hồ sơ thì Burn chạy như trên. Chi tiết ở ADR-112, ADR-113.

## An toàn

- Chỉ admin bật được, và phải qua hộp xác nhận. Người bật được ghi Nhật ký (`burn.start`, `burn.stop`).
- Toàn quyền của phiên chỉ áp cho các lượt của chính phiên đó. Tin người khác nhắn vào hội thoại Burn chạy theo quyền của họ.
- Chế độ "nhánh riêng" không bao giờ push hay merge. Merge là việc của người dùng.
- Các rào chắn của ADR-074 vẫn giữ nguyên: guard, chặn thư mục nguy hiểm.

## Ngoài phạm vi (lần này)

- Nhiều phiên Burn cùng lúc trong một project.
- Tự tạo PR.
- Tự merge.
