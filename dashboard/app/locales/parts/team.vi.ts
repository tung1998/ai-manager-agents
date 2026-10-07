// Vietnamese strings for team (source of truth for keys).
// A project's agents and starter packs (ADR-099).
export default {
  'team.agentCount': '{n} agent',
  'team.noAgents': 'Chưa có agent',
  'team.starterPack': 'Gói khởi đầu',
  'team.default': 'Mặc định',
  'team.defaultHint': 'Trả lời chat, bot và tự động hóa không gọi tên agent nào',
  'team.defaultAgent': 'Agent mặc định',
  'team.defaultAgentLower': 'agent mặc định',
  'team.defaultSet': '{name} giờ là agent mặc định',
  'team.makeDefault': 'Đặt làm mặc định',
  'team.open': 'Mở trang agent',
  'team.export': 'Tải JSON các agent',
  'team.applyPack': 'Dùng gói này',
  'team.packApplied': 'Đã áp gói',
  'team.replacePack': 'Thay bằng gói khác',
  'team.replaceWarn': 'Agent của gói thay cho agent hiện tại (agent trùng key giữ chat cũ). Trạng thái hiện tại vẫn khôi phục được ở Lịch sử.',
  'team.replaceBtn': 'Thay agent',
  'team.addAndConfigure': 'Thêm rồi cấu hình',
  'team.empty.title': 'Project chưa có agent',
  'team.empty.desc': 'Chọn một gói khởi đầu (agent và quy trình), hoặc để AI đề xuất theo project.',

  // PackPicker
  'team.pack.aiSuggest': 'AI đề xuất',
  'team.pack.aiRecommended': '· khuyên dùng',
  'team.pack.aiDesc': 'Quét project, đọc CLAUDE.md, AGENTS.md, .claude/agents… rồi chọn gói và tinh chỉnh agent. Cần kết nối AI.',
  'team.pack.workflowCount': '{n} quy trình',

  // RevisionHistory
  'team.rev.intro': 'Mỗi lần sửa agent, office lưu lại trạng thái ngay trước đó. Khôi phục một bản cũng được lưu, nên luôn hoàn tác được.',
  'team.rev.restoreConfirm': 'Đưa các agent của project về trạng thái này? Trạng thái hiện tại vẫn được lưu lại trong lịch sử.',
  'team.rev.pack': 'áp gói {arg}',

  // setup with AI
  'team.setup.pack': 'Gói khởi đầu',
  'team.setup.hasAgentsWarn': 'Project đang có {n} agent. Áp dụng thiết lập mới sẽ thay các agent này (khôi phục được ở Lịch sử).',

  // home: getting started
  'team.home.step3Title': 'Cho project các agent',
  'team.home.step3TextDone': '{n}/{total} project đã có agent',
  'team.home.step3TextTodo': 'Một agent, nhóm phát triển hoặc hội đồng'
} as const
