---
key: contract-renewal
name: Rà soát gia hạn hợp đồng
category: Finance & Legal
description: Tóm tắt thay đổi, nghĩa vụ, mốc gia hạn và điểm cần luật sư hoặc người phụ trách quyết định.
input: Hợp đồng sắp gia hạn hoặc amendment
inputs:
  - { key: context, description: "Hợp đồng hiện tại, bản mới, playbook và lịch quan trọng", required: true }
outputs:
  - { key: changes, description: "Thay đổi và nghĩa vụ đáng chú ý", required: true, type: list }
  - { key: issues, description: "Điểm cần legal/business review", required: true, type: list }
roles:
  - { key: legal, name: Pháp lý, hint: obligations, liability, termination, data }
  - { key: business, name: Nghiệp vụ, hint: commercial outcome, service, renewal, differ_from: [legal] }
  - { key: security, name: Bảo mật, hint: data processing, access, incident }
parallel: [[legal, business], [security]]
gates:
  - { key: sign, name: Duyệt ký hợp đồng, kind: approve, required: true }
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [context]
---
1. So sánh bản cũ và mới, trích điều khoản theo evidence; không đưa kết luận pháp lý thay cho luật sư.
2. Nêu issue, owner, deadline và phương án thương lượng.
3. Mở gate `sign`; sau khi duyệt gọi `workflow_done` với changes và issues.
