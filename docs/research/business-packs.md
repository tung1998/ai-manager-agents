# Thiết kế: việc AI làm được cho doanh nghiệp nhỏ

Bản nháp để hội đồng xét, chưa phải ADR. Thay bản trước (7 gói kiểu ERP: POS, kho, sổ sách, chấm công… tự dựng dữ liệu riêng): **bỏ**. Office không làm phần mềm quản lý; chỉ làm việc **agent AI tối ưu, tự động hóa được** trên công cụ người dùng đang có.

## 1. Nguyên tắc

- **Không dữ liệu riêng, không màn hình nghiệp vụ.** Dữ liệu ở chỗ người dùng đang dùng (KiotViet, Sapo, MISA, Google Sheets, Zalo, email, sao kê ngân hàng); agent đọc/ghi qua MCP, file, ảnh, tin nhắn.
- Mỗi việc = **tự động hóa** (lịch, webhook, tin nhắn bot) hoặc **quy trình**, chạy bằng agent có sẵn của office. Ưu tiên `action=script`, chỉ gọi AI khi cần đọc hiểu/viết.
- Ghi ra ngoài (gửi khách, nhập vào phần mềm, chi tiền) **luôn qua thẻ duyệt** (ADR-090).
- Chỉ chọn việc AI làm tốt hơn người: **đọc giấy tờ/ảnh, đối soát, tóm tắt, soạn tin, phát hiện bất thường**. Màn hình nhập liệu, POS, sơ đồ bàn, Gantt: không làm.

## 2. Việc theo ngành

| Ngành | Việc agent làm | Kích hoạt | Đầu ra (duyệt nếu ra ngoài) |
|---|---|---|---|
| **Bán hàng** | Đọc tin nhắn đặt hàng → đơn nháp | tin nhắn bot | đơn trong Sheet/phần mềm bán hàng (duyệt) |
| | Nhắc khách nợ quá hạn | lịch hằng ngày | tin nhắn soạn sẵn (duyệt) |
| | Báo cáo doanh thu cuối ngày, so tuần trước, mặt hàng bất thường | lịch | tin Telegram/Discord |
| **Kho** | Ảnh/PDF hóa đơn nhà cung cấp → phiếu nhập | gửi ảnh cho bot | dòng hàng nhập (duyệt) |
| | Hàng sắp hết, hàng chậm bán → đơn đặt hàng nháp | lịch tuần | đơn đặt nhà cung cấp (duyệt) |
| | Kiểm kê: đối chiếu số đếm với tồn sổ, giải thích chênh lệch | gửi file | báo cáo lệch |
| **Kế toán** | Sao kê ngân hàng → ghép với đơn/hóa đơn, liệt kê khoản chưa khớp | lịch / gửi file | bảng đối soát |
| | Phân loại khoản chi theo khoản mục | khi có giao dịch mới | Sheet đã phân loại |
| | Lãi lỗ, dòng tiền tháng; hỏi đáp trên số liệu | lịch tháng / chat | báo cáo |
| **Nhân sự** | Bảng công (máy chấm công, Sheet) → bảng lương theo công thức mô tả bằng lời | lịch tháng | bảng lương (duyệt) |
| | Nhắc hết hạn hợp đồng, sinh nhật, ngày phép | lịch | tin nhắn |
| | Lọc CV, soạn tin mời phỏng vấn | email/upload | danh sách + tin (duyệt) |
| **Nhà hàng – Cà phê** | Báo cáo cuối ca: món bán chạy, hao hụt nguyên liệu so định mức | lịch | tin nhắn |
| | Đọc và trả lời đánh giá Google/Foody | lịch | trả lời nháp (duyệt) |
| | Gợi ý nhập nguyên liệu theo bán hàng tuần trước | lịch tuần | đơn nháp (duyệt) |
| **Sản xuất** (in, CNC, gỗ…) | **Báo giá** từ mô tả/bản vẽ của khách theo bảng giá vật tư, công | tin nhắn/email | báo giá (duyệt) |
| | Theo dõi đơn trễ hạn từ Sheet tiến độ | lịch | cảnh báo |
| **Công trình** | Ảnh + ghi chú hiện trường → nhật ký, báo cáo tuần cho chủ đầu tư | gửi ảnh cho bot / lịch tuần | báo cáo (duyệt) |
| | Hóa đơn vật tư → so với dự toán, cảnh báo vượt | gửi ảnh | cảnh báo |

## 3. Cần thêm gì vào office

Gần như dùng lại lõi (bot, tự động hóa, quy trình, thẻ duyệt, MCP, thư viện ADR-166). Thiếu:

1. **Mẫu trong thư viện** theo ngành: tự động hóa + quy trình + skill (đọc hóa đơn, đối soát sao kê, báo giá). Thiết lập bằng AI (ADR-166) chọn theo mô tả project.
2. **Kết nối Google Sheets** làm "sổ" mặc định cho người chưa có phần mềm (MCP có sẵn).
3. **Đọc ảnh/PDF gửi qua bot** thành dữ liệu có cấu trúc (kiểm lại hiện bot nhận file đính kèm tới đâu).
4. MCP cho phần mềm VN phổ biến (KiotViet, Sapo, MISA): chỉ làm khi có người dùng thật.

## 4. Lộ trình

| Bước | Việc | Kiểm chứng |
|---|---|---|
| 1 | Skill đọc hóa đơn ảnh/PDF → dòng hàng; mẫu "nhập hóa đơn vào Sheet" | 20 hóa đơn thật, sai ≤ 1 dòng |
| 2 | Mẫu đối soát sao kê; báo cáo doanh thu/lãi lỗ định kỳ từ Sheet | một tháng thật |
| 3 | Mẫu báo giá (sản xuất), nhật ký công trình, trả lời đánh giá | mỗi mẫu một người dùng thật |

## 5. Không làm

POS, sơ đồ bàn, màn hình bếp, chấm công, sổ kế toán, quản lý kho, Gantt; tài khoản nhân viên; hóa đơn điện tử, thuế; kết nối phần cứng.
