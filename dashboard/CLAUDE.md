# Dashboard (Nuxt 4 + Nuxt UI)

## i18n: bắt buộc
- Không viết chữ hiển thị thẳng trong `.vue`/`.ts`. Dùng `const { t, dateLocale } = useLang()` và `t('khu.vuc.key', { n })`.
- Key nằm trong `app/locales/parts/<khu>.vi.ts` (tiếng Việt, nguồn của key) và `<khu>.en.ts` (tiếng Anh; TypeScript báo lỗi nếu thiếu key). Chuỗi dùng chung: `common.*`, tên menu/tab: `nav.*` trong `app/locales/vi.ts`/`en.ts`.
- Map/hằng chứa chữ phải tính lúc render (computed, hàm, getter), không gọi `t()` một lần lúc khởi tạo.
- Ngày giờ: `toLocale*String(dateLocale.value, …)`, không cứng `'vi-VN'`.
- Chữ từ backend (lỗi, output AI) giữ nguyên; enum từ backend (status, kind, level) thì dịch theo key.
- Kiểm tra: `node scripts/check-i18n.mjs` (chạy trong `make test`, `make ui-build` và "Cập nhật office" khi bật test). Dòng buộc phải giữ tiếng Việt (prompt gửi AI, regex) thêm `// i18n-ignore`.

## Loading: bắt buộc
- Fetch có `await` (trang): thanh `NuxtLoadingIndicator` lo. Tab trong trang project nằm trong `<Suspense :key="tab">` có khung chờ, nên panel `await` trong setup cũng có loading.
- Fetch không `await` (`lazy`, hoặc không chờ): trong lúc `!data && !error` hiện `<LoadingRows>` (danh sách/bảng) hoặc `<USkeleton>` (số liệu, ô nhập); không bao giờ hiện trạng thái rỗng ("chưa có…", "0 B", "—") trước khi có dữ liệu. Có `error` thì `UAlert` + nút Làm mới.
- Nút hành động phụ thuộc dữ liệu (Bật, Lưu, Cài đặt) bị `disabled` khi chưa tải xong.

## Header trang
- Header (`PageShell`) chỉ có tiêu đề, dòng phụ (`#subtitle`) và chỉ số office; không đặt nút ở đó. Nút của trang đặt qua slot `#actions`: `PageShell` hiện chúng ở đầu phần nội dung, canh phải.
- Trang chiếm trọn chiều cao (chat, theo dõi) không dùng `#actions`: thao tác nằm trong chính nội dung (tab Thông tin, menu của box…).
