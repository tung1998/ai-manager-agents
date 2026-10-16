---
key: hiring-loop
name: Đánh giá ứng viên
description: Tổng hợp phỏng vấn theo rubric thống nhất, giảm thiên kiến và ra quyết định tuyển dụng có bằng chứng.
input: Ứng viên, vị trí và bộ tiêu chí tuyển dụng
inputs:
  - { key: context, description: "Role, rubric, CV, ghi chú phỏng vấn và bối cảnh team", required: true }
  - { key: constraints, description: "Mức độ seniority, thời hạn và yêu cầu bắt buộc" }
outputs:
  - { key: scorecard, description: "Điểm theo rubric và bằng chứng", required: true, type: list }
  - { key: recommendation, description: "hire | no_hire | more_signal và lý do", required: true }
roles:
  - key: craft
    name: Đánh giá chuyên môn
    hint: role-specific skills, work sample, evidence
  - key: collaboration
    name: Phối hợp và giao tiếp
    hint: collaboration, ownership, clarity
    differ_from: [craft]
  - key: hiring-manager
    name: Hiring manager
    hint: scope, level, team fit, decision
parallel: [[craft, collaboration], [hiring-manager]]
gates:
  - key: decision
    name: Người tuyển dụng duyệt quyết định
    kind: approve
    required: true
limits: { rounds: 2, turns: 8, timeout: 2h, idle: 30m }
brief: [constraints, context]
---
1. Chuẩn hóa rubric và tách bằng chứng quan sát được khỏi ấn tượng cá nhân; không suy luận từ thông tin nhạy cảm không liên quan.
2. Giao các vai đánh giá độc lập phần của mình, ghi điểm, evidence và câu hỏi còn bỏ ngỏ.
3. Hợp nhất scorecard, cân nhắc mức độ tin cậy của từng tín hiệu và đề xuất vòng bổ sung nếu dữ liệu chưa đủ.
4. Mở gate `decision` với recommendation và rủi ro; chờ người tuyển dụng duyệt.
5. Gọi `workflow_done` với scorecard và recommendation.
