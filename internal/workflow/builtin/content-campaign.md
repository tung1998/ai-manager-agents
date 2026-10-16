---
key: content-campaign
name: Lập chiến dịch nội dung
description: Từ mục tiêu và insight, lập campaign đa kênh có thông điệp, lịch, đo lường và bước duyệt rõ ràng.
input: Mục tiêu marketing, đối tượng và thời gian chiến dịch
inputs:
  - { key: outcome, description: "Mục tiêu, audience, offer và chỉ số thành công", required: true }
  - { key: context, description: "Brand voice, kênh, tài sản sẵn có và giới hạn pháp lý" }
outputs:
  - { key: campaign, description: "Thông điệp, asset, kênh, lịch và owner", required: true, type: list }
  - { key: measurement, description: "Funnel, event tracking và tiêu chí đánh giá", required: true, type: list }
roles:
  - key: strategy
    name: Chiến lược
    hint: audience, positioning, offer, funnel
  - key: creative
    name: Sáng tạo nội dung
    hint: copy, visual brief, channel adaptation
    differ_from: [strategy]
  - key: measurement
    name: Đo lường
    hint: events, attribution, experiment, dashboard
parallel: [[strategy, creative], [measurement]]
gates:
  - key: publish
    name: Duyệt nội dung trước khi phát hành
    kind: approve
    required: true
limits: { rounds: 3, turns: 9, timeout: 3h, idle: 45m }
brief: [outcome, context]
---
1. Giao `strategy` xác định audience, problem, proposition, funnel và giả định cần test.
2. Giao `creative` lập message house, asset matrix và bản nháp theo từng kênh; tuân thủ brand voice và không bịa claim.
3. Giao `measurement` định nghĩa event, attribution, baseline, experiment và dashboard cần có trước launch.
4. Hợp nhất campaign plan với owner, lịch, dependency, ngân sách giả định và rủi ro.
5. Mở gate `publish` trước khi phát hành; sau khi được duyệt, gọi `workflow_done` với campaign và measurement.
