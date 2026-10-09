# Nghiên cứu: cách các bên lớn làm agent dựng dự án

So với Burn (ADR-130..136) và `office init` (`internal/initscan`) — office đã có gì, còn thiếu gì.

## 1. Anthropic — "Effective harnesses for long-running agents"
https://anthropic.com/engineering/effective-harnesses-for-long-running-agents

- Chia hai vai: **initializer agent** chạy một lần đầu, viết `feature_list.json` (danh sách feature + cờ `passes`) và `init.sh` (script khởi động cho các phiên sau).
- **Coding agent** được đánh thức lặp lại nhiều phiên; mỗi phiên chỉ làm **một feature**, chạy test, rồi ghi `claude-progress.txt` (nhật ký phiên trước đã làm gì) và commit.
- Harness là **tập file**, không phải code điều phối: progress log + feature list máy đọc được + script khởi động + lịch sử git nhỏ gọn là "bộ nhớ" duy nhất sống sót qua các cửa sổ ngữ cảnh.
- Mỗi phiên bắt đầu gần như "mất trí nhớ": phải đọc lại file để biết trạng thái, không dựa vào hội thoại cũ.

**Office đã có:** phần "mỗi lượt một feature + file tiến độ" khớp với Burn — ADR-130 (quét/làm tách luồng, quét theo quy trình cố định, có bản đồ code, `internal/burn/scan.go`), ADR-131 (kiểm chứng tất định + sổ bài học, `internal/burn/verify.go`, `internal/burn/report.go`), ADR-135 (dừng sau N việc, chờ review, `internal/burn/caps.go`, `internal/burn/review.go`). Phần "initializer agent" khớp với `office init` / `internal/initscan` (quét tĩnh repo: manifest, SDK, infra, migrations, sinh tóm tắt cho LLM — `internal/initscan/README.md:3`).

**Thiếu:** Burn chưa có `feature_list.json` dạng cờ `passes` tường minh cho người đọc nhanh tiến độ toàn cục (Burn dùng quest/piece rời rạc qua `burn_add`/`burn_pick`, không phải một danh sách feature cố định từ đầu). `office init`/`internal/initscan` chưa sinh `init.sh` để các phiên sau tự chạy lại môi trường.

## 2. OpenAI — "Harness engineering" (Codex)
https://openai.com/index/harness-engineering/

- Nội bộ OpenAI dựng một sản phẩm phần mềm mà 100% dòng code (logic, test, CI, docs, observability, tooling) do Codex viết; họ gọi cách làm này là "harness engineering".
- Thử cách "một `AGENTS.md` khổng lồ" trước — **thất bại**: file hướng dẫn to lấn chỗ của task/code/docs liên quan, agent bỏ sót ràng buộc quan trọng hoặc tối ưu sai hướng; manual to cũng "mục rữa" rất nhanh, thành đống rule cũ.
- Bài học: coi **tri thức trong repo là nguồn sự thật** (system of record) — `AGENTS.md` nên là bản đồ điều hướng ngắn (lệnh test, quy ước), không phải kho quy tắc; docs sống trong repo, không tách rời.

**Office đã có:** tương đương "repo là nguồn sự thật" là `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`, `docs/DATA_MODEL.md`, `docs/INTERFACES.md` (tài liệu sống trong repo, Burn/initscan đọc trực tiếp). ADR-133 (kiểm tra nhanh file ngay sau khi agent sửa — lint cơ học, tương đương "lint cơ học" OpenAI nói tới) và ADR-134 (bộ nhớ hai tầng: cốt lõi luôn nạp, theo chủ đề nạp khi cần — tránh đúng lỗi "một file to lấn chỗ" mà OpenAI gặp).

**Thiếu:** office chưa có file "bản đồ điều hướng ngắn" kiểu `AGENTS.md` ở gốc mỗi project được quét/dựng mới; `office init`/`internal/initscan` hiện sinh tóm tắt cho LLM nhưng chưa rõ có tối ưu để "ngắn, không mục rữa" như khuyến nghị này.

## 3. Ralph loop (Geoffrey Huntley)
https://devinterrupted.substack.com/p/inventing-the-ralph-wiggum-loop-creator

- Vòng lặp shell đơn giản: lặp lại **cùng một prompt file**, mỗi lần một **cửa sổ ngữ cảnh mới hoàn toàn** (agent không nhớ gì), chỉ đọc lại file hệ thống (code, plan, progress log) để biết tiếp tục từ đâu.
- Ý tưởng: một lượt chạy riêng lẻ có thể "ngu" (Ralph Wiggum), nhưng cả vòng lặp đạt kết quả nhờ lặp đi lặp lại có kỷ luật ghi file + git.
- Dẫn chứng thực tế: một hợp đồng $50.000 hoàn thành với $297 chi phí API bằng kỹ thuật này.

**Office đã có:** đúng mô hình "mỗi lượt context mới, chỉ còn file+git là trí nhớ" — đây chính là cách `internal/burn/run.go` vận hành piece theo piece. ADR-136 (dừng theo ngưỡng usage 5 giờ/tuần, `docs/DECISIONS.md:2184`) còn thêm lớp kiểm soát chi phí mà Ralph loop gốc không có (Ralph loop dựa vào người chạy tự dừng).

**Thiếu:** không có khoảng trống rõ rệt so với Burn — đây là nguồn ít chính thống nhất (blog cá nhân), dùng để xác nhận hướng đã chọn hơn là tìm thiếu sót mới.

## 4. Lovable / Bolt / v0 — preview sau mỗi vòng
https://www.pootlepress.com/?p=54055

- Kiến trúc: planner tách prompt thành tasks → agent sinh code → **sandbox chạy app** (mini máy ảo trên cloud) → log/lỗi được đưa ngược lại vòng lặp → deploy.
- Mỗi vòng cho ra **preview sống** ngay lập tức, không chỉ code; vòng lặp tiếp theo dựa trên phản hồi từ preview đó (lỗi runtime, output thực tế) chứ không chỉ dựa trên test tĩnh.
- Bolt dùng môi trường dev đầy đủ (StackBlitz) cho người kỹ thuật; Lovable thiên về hội thoại + default hợp lý cho người không chuyên.

**Office đã có:** `ops_overview`/`process_logs`/`propose_action` cho phép Burn (qua người vận hành có quyền "Vận hành") chạy lại tiến trình/container để xem kết quả thật, gần giống vòng "chạy → xem log → sửa tiếp". ADR-133 (kiểm tra nhanh ngay sau khi sửa) đóng vai trò gần với "feed lỗi runtime ngược vào vòng lặp".

**Thiếu:** Burn hiện kiểm chứng chủ yếu bằng build/test tĩnh (go build, go vet, go test, i18n check, typecheck) — chưa có bước "preview sống" tự động (chạy app thật, chụp lại trạng thái UI) sau mỗi quest như Lovable/Bolt/v0 làm. Đây là khoảng trống rõ nhất so với nhóm preview-driven.

## Bảng tóm tắt

| Bên | Ý chính | Office đã có (ADR + file) | Thiếu |
|---|---|---|---|
| Anthropic | initializer 1 lần + feature_list.json + progress file + 1 feature/lượt | ADR-130, ADR-131, ADR-135; `internal/burn/scan.go`, `verify.go`, `report.go`, `caps.go`, `review.go`; `office init`/`internal/initscan` | Chưa có `feature_list.json` dạng cờ `passes` toàn cục; initscan chưa sinh `init.sh` |
| OpenAI Codex | repo là nguồn sự thật, AGENTS.md ngắn thay vì manual to, lint cơ học | `docs/ARCHITECTURE.md` etc.; ADR-133 (lint cơ học); ADR-134 (bộ nhớ hai tầng) | Chưa có file "bản đồ điều hướng ngắn" kiểu AGENTS.md sinh tự động khi `office init` |
| Ralph loop | mỗi lượt context mới, chỉ file+git là trí nhớ | `internal/burn/run.go`; ADR-136 (ngưỡng usage, hơn cả Ralph gốc) | Không có khoảng trống rõ rệt (nguồn ít chính thống nhất) |
| Lovable/Bolt/v0 | preview sống sau mỗi vòng, lỗi runtime feed ngược | `ops_overview`, `process_logs`, `propose_action`; ADR-133 | Chưa có bước "preview sống" tự động sau mỗi quest Burn (chỉ build/test tĩnh) |

## Đề xuất áp dụng

1. **Quest tiếp theo (không ADR mới):** thêm bước tuỳ chọn "chạy thử + chụp trạng thái" vào quy trình kiểm chứng của Burn cho các quest chạm UI/dashboard, dùng `propose_action`/`run_command` đã có sẵn — không cần hạ tầng mới, chỉ là bước verify thêm. Ưu tiên cao nhất vì đây là khoảng trống rõ nhất (mục 4).
2. **ADR tiếp theo (đề xuất, chưa viết):** cân nhắc cho `office init`/`internal/initscan` sinh thêm một file "bản đồ ngắn" kiểu AGENTS.md (vài chục dòng: lệnh build/test, quy ước, nơi tìm docs) cho mỗi project mới, tách khỏi các ADR/tài liệu dài — theo đúng bài học "manual to thì mục rữa" của OpenAI (mục 2).
3. **ADR tiếp theo (đề xuất, chưa viết):** cân nhắc thêm một "feature list" máy đọc được (dạng JSON, cờ hoàn thành) song song với cơ chế quest hiện tại của Burn, để người xem nhanh được tiến độ toàn cục của một dự án đang dựng — bổ sung cho ADR-130/131, không thay thế (mục 1).
