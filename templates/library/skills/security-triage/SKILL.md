---
name: security-triage
description: Phân loại tín hiệu bảo mật, bảo toàn bằng chứng và đề xuất containment an toàn trước khi khắc phục.
---

# Security triage

Khi gặp cảnh báo bảo mật:

1. Xác định asset, account, thời điểm, mức độ tin cậy và blast radius từ bằng chứng thật.
2. Giữ nguyên log và chain of custody; không xóa dấu vết hoặc thử exploit trên hệ thống production.
3. Phân loại false positive, cần điều tra thêm hoặc incident; nêu tiêu chí chuyển cấp.
4. Với containment, ghi tác động, rollback và người cần phê duyệt; không tự xoay secret hay khóa tài khoản nếu chưa được phép.
5. Kết thúc bằng timeline, evidence, owner và bước xác minh sau xử lý.
