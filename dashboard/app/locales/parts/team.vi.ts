// Vietnamese strings for team (source of truth for keys).
// A project's agents and starter packs (ADR-099).
export default {
  'team.agentCount': '{n} agent',
  'team.noAgents': 'Chưa có agent',
  'team.starterPack': 'Gói khởi đầu',
  'team.default': 'Mặc định',
  'team.defaultHint': 'Agent này trả lời khi chat, tin nhắn bot (Discord/Telegram) hoặc một tự động hóa không chỉ rõ tên agent nào. Dùng khi project có nhiều agent nhưng muốn có một agent luôn trả lời trong các trường hợp chung chung. Đặt agent khác làm mặc định thì agent cũ thôi giữ vai trò này, chat cũ của nó vẫn giữ nguyên.',
  'team.defaultAgent': 'Agent mặc định',
  'team.defaultAgentLower': 'agent mặc định',
  'team.defaultSet': '{name} giờ là agent mặc định',
  'team.makeDefault': 'Đặt làm mặc định',
  'team.open': 'Mở trang agent',
  'team.export': 'Tải JSON các agent',
  'team.applyPack': 'Dùng gói này',
  'team.packApplied': 'Đã áp gói',
  'team.replacePack': 'Thay bằng gói khác',
  'team.replaceWarn': 'Áp gói khác thay toàn bộ agent hiện tại của project bằng agent trong gói mới. Dùng khi muốn đổi hẳn sang một bộ agent khác thay vì sửa từng agent. Agent trùng tên định danh với gói mới được cập nhật tại chỗ và giữ nguyên chat cũ; agent không trùng tên bị xóa, nhưng trạng thái trước khi thay vẫn xem và khôi phục được ở Lịch sử.',
  'team.replaceBtn': 'Thay agent',
  'team.addAndConfigure': 'Thêm rồi cấu hình',
  'team.empty.title': 'Project chưa có agent',
  'team.empty.desc': 'Chọn một gói khởi đầu (agent và quy trình), hoặc để AI đề xuất theo project.',

  // AgentLibrary (ADR-153)
  'team.lib.tab': 'Từ thư viện',
  'team.lib.blankTab': 'Tự tạo',
  'team.lib.addOne': 'Thêm một agent từ thư viện',
  'team.lib.all': 'Tất cả',
  'team.lib.pageTitle': 'Thư viện agent',
  'team.lib.pageDesc': 'Mẫu agent để thêm vào project hoặc tham khảo khi tự viết hướng dẫn. Bấm một agent để xem đầy đủ.',
  'team.lib.pickProject': 'Chọn project',
  'team.lib.toProject': 'Thêm vào project',
  'team.lib.featured': 'Đề xuất',
  'team.lib.cat.design': 'Thiết kế',
  'team.lib.count': '{n} agent',
  'team.lib.words': '{n} từ',
  'team.lib.agencyNote': 'Bản gốc tiếng Anh, khá dài: nên rút gọn sau khi thêm để đỡ tốn token',
  'team.lib.cat.code': 'Code',
  'team.lib.cat.plan': 'Kế hoạch',
  'team.lib.cat.customer': 'Khách hàng',
  'team.lib.cat.content': 'Nội dung',
  'team.lib.search': 'Tìm agent…',
  'team.lib.none': 'Không có agent nào khớp',
  'team.lib.source': 'Bản gốc',
  'team.lib.use': 'Dùng mẫu này',
  'team.lib.from': 'Theo mẫu {name}: kèm mô tả, hướng dẫn và quyền của mẫu',
  'team.lib.clear': 'Bỏ mẫu',

  // PackPicker
  'team.pack.aiSuggest': 'AI đề xuất',
  'team.pack.aiRecommended': '· khuyên dùng',
  'team.pack.aiDesc': 'Dùng AI để tự chọn gói khởi đầu phù hợp cho project, thay vì bạn tự chọn thủ công. Agent sẽ quét cấu trúc project, đọc các file hướng dẫn có sẵn như CLAUDE.md, AGENTS.md, .claude/agents…, rồi chọn một gói phù hợp và chỉnh tên, hướng dẫn, quyền của từng agent trong gói cho khớp với project. Cần có một kết nối AI đã kiểm tra hoạt động trước.',
  'team.pack.workflowCount': '{n} quy trình',

  // RevisionHistory
  'team.rev.intro': 'Mỗi lần sửa agent, office lưu lại trạng thái ngay trước đó. Khôi phục một bản cũng được lưu, nên luôn hoàn tác được.',
  'team.rev.restoreConfirm': 'Đưa các agent của project về trạng thái này? Trạng thái hiện tại vẫn được lưu lại trong lịch sử.',
  'team.rev.pack': 'áp gói {arg}',

  // setup with AI
  'team.setup.pack': 'Gói khởi đầu',
  'team.setup.hasAgentsWarn': 'Project đang có {n} agent. Áp dụng chỉ thêm agent còn thiếu, agent đang có giữ nguyên.',

  // home: getting started
  'team.home.step3Title': 'Cho project các agent',
  'team.home.step3TextDone': '{n}/{total} project đã có agent',
  'team.home.step3TextTodo': 'Một agent, nhóm phát triển hoặc hội đồng'
} as const
