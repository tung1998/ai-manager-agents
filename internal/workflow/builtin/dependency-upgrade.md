---
key: dependency-upgrade
name: Nâng cấp dependency
category: Engineering
description: Đánh giá một đợt nâng cấp thư viện, tìm breaking change và lập kế hoạch rollout an toàn.
input: Dependency hoặc nhóm package cần nâng cấp
inputs:
  - { key: context, description: "Version hiện tại/mục tiêu, changelog, lockfile và phạm vi ảnh hưởng", required: true }
outputs:
  - { key: assessment, description: "Rủi ro, breaking changes và compatibility", required: true, type: list }
  - { key: plan, description: "Các bước nâng cấp, test và rollback", required: true, type: list }
roles:
  - { key: maintainer, name: Maintainer, hint: "changelog, API, code impact" }
  - { key: security, name: Bảo mật, hint: "advisories, provenance, licenses", differ_from: [maintainer] }
  - { key: verifier, name: Kiểm chứng, hint: "build, test, runtime behavior" }
parallel: [[maintainer, security], [verifier]]
gates:
  - { key: upgrade, name: Duyệt nâng cấp, kind: approve, required: true }
limits: { rounds: 2, turns: 8, timeout: 3h }
brief: [context]
---
1. Đọc release notes và advisory chính thức, không suy đoán từ version number.
2. Lập impact map, test plan, canary và rollback.
3. Mở gate `upgrade`; sau khi duyệt gọi `workflow_done` với assessment và plan.
