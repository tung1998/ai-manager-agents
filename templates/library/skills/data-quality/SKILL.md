---
name: data-quality
description: Kiểm tra freshness, schema, null, duplicate và outlier trước khi dùng dữ liệu cho báo cáo hoặc agent.
---

# Data quality

Trước khi kết luận từ dữ liệu:

1. Ghi nguồn, kỳ dữ liệu, timezone, owner và expected freshness.
2. Kiểm tra schema drift, null, duplicate, range, outlier và liên kết giữa bảng.
3. Đếm số dòng bị ảnh hưởng; đưa ví dụ đại diện nhưng không làm lộ dữ liệu nhạy cảm.
4. Phân biệt lỗi nguồn, lỗi pipeline và lỗi diễn giải; đề xuất kiểm tra tái hiện được.
5. Nêu rõ dữ liệu đủ dùng đến đâu và điều kiện cần kiểm tra lại sau khi sửa.
