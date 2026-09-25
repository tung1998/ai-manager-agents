package tasks

import (
	"bitbucket.org/senprints/agent-office/internal/perm"
	"encoding/json"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

const (
	maxAssignments  = 8
	maxFilesPerTask = 3 // a worker gets one small, well-bounded job
)

// Plan is what a lead / planner returns.
type Plan struct {
	Analysis string `json:"analysis"`
	// Conventions every worker must follow (library, naming, paths…), decided
	// once by the planner so parallel work stays consistent.
	Conventions string       `json:"conventions,omitempty"`
	Answer      string       `json:"answer,omitempty"` // when no delegation is needed
	Assignments []Assignment `json:"assignments"`
}

// Assignment hands one small job to one agent.
type Assignment struct {
	Agent     string   `json:"agent"`
	Task      string   `json:"task"`
	Files     []string `json:"files,omitempty"`      // the only files it may change
	DependsOn []int    `json:"depends_on,omitempty"` // 1-based numbers of earlier assignments
	DoneWhen  string   `json:"done_when,omitempty"`
}

// validatePlan checks the rules that keep workers from colliding: each code
// job names at most maxFilesPerTask files, no file belongs to two jobs, and
// dependencies point to earlier jobs. canEdit reports agents that may propose diffs.
func validatePlan(p Plan, canEdit func(agent string) bool) []string {
	var problems []string
	if len(p.Assignments) > maxAssignments {
		problems = append(problems, fmt.Sprintf("tối đa %d việc (đang có %d)", maxAssignments, len(p.Assignments)))
	}
	owner := map[string]int{}
	for i, a := range p.Assignments {
		n := i + 1
		if canEdit(a.Agent) {
			if len(a.Files) == 0 {
				problems = append(problems, fmt.Sprintf("việc %d (%s) sửa code nhưng không liệt kê files", n, a.Agent))
			}
			if len(a.Files) > maxFilesPerTask {
				problems = append(problems, fmt.Sprintf("việc %d có %d file, tối đa %d: tách nhỏ hơn", n, len(a.Files), maxFilesPerTask))
			}
		}
		for _, f := range a.Files {
			f = cleanPath(f)
			if prev, ok := owner[f]; ok {
				problems = append(problems, fmt.Sprintf("file %s nằm trong cả việc %d và việc %d: mỗi file chỉ thuộc một việc", f, prev, n))
				continue
			}
			owner[f] = n
		}
		for _, d := range a.DependsOn {
			if d < 1 || d >= n {
				problems = append(problems, fmt.Sprintf("việc %d phụ thuộc việc %d không hợp lệ (chỉ được phụ thuộc việc trước nó)", n, d))
			}
		}
	}
	if len(p.Assignments) > 1 && strings.TrimSpace(p.Conventions) == "" {
		problems = append(problems, "thiếu conventions: chốt quy ước chung (thư viện, cách đặt tên, đường dẫn…) để các việc khớp nhau")
	}
	return problems
}

func cleanPath(p string) string {
	p = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(p, "./"), "/"))
	return strings.TrimPrefix(strings.TrimPrefix(p, "a/"), "b/")
}

// Vote is a council member's decision on a plan.
type Vote struct {
	Vote   string `json:"vote"` // approve | reject
	Reason string `json:"reason"`
}

// Verdict is the auditor's review of the work.
type Verdict struct {
	Verdict      string   `json:"verdict"`            // pass | fix | fail (cannot be fixed) | ask (needs the person)
	Question     string   `json:"question,omitempty"` // for "ask"
	Summary      string   `json:"summary"`
	Issues       []string `json:"issues"`
	BlockChanges bool     `json:"block_changes"`
	Fixes        []Fix    `json:"fixes,omitempty"` // what each job must correct before another review
}

// Fix asks the worker of one job to correct its work.
type Fix struct {
	Job   int    `json:"job"` // 1-based job number
	Issue string `json:"issue"`
}

// Repairs go on until the review passes; these only stop runaway loops (the
// task and daily budgets stop spending anyway).
const (
	maxRepairRounds = 8
	stuckRounds     = 2 // the same jobs failing with the same issues this many times in a row
)

// labeledPatch is a patch with the job that produced it.
type labeledPatch struct {
	storage.Patch
	Job   int
	Agent string
}

func roster(agents []storage.Agent) string {
	var b strings.Builder
	for _, a := range agents {
		access := "gói " + perm.Label(perm.Agent(a))
		if perm.AtLeast(perm.Agent(a), perm.Propose) {
			access += ", được giao sửa code"
		} else {
			access += ", chỉ giao việc đọc/phân tích"
		}
		fmt.Fprintf(&b, "- %s (key: %s, %s): %s", a.Name, a.Key, a.Tier, firstNonEmpty(a.Role, a.Description))
		fmt.Fprintf(&b, " [%s]\n", access)
	}
	return b.String()
}

func planPrompt(goal string, team []storage.Agent, feedback string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mục tiêu người dùng giao cho đội:\n%s\n\n", goal)
	fmt.Fprintf(&b, "Đội của bạn (bạn giao việc bằng key):\n%s\n", roster(team))
	if feedback != "" {
		fmt.Fprintf(&b, "Kế hoạch trước bị bác vì:\n%s\n\nHãy sửa kế hoạch theo góp ý.\n\n", feedback)
	}
	fmt.Fprintf(&b, `Lập kế hoạch: đọc project trước (cấu trúc, file liên quan, cách code hiện có), rồi chia mục tiêu thành tối đa %[1]d việc NHỎ, mỗi việc cho đúng một agent phù hợp nhất.

Quy tắc bắt buộc (kế hoạch vi phạm sẽ bị trả lại):
1. "conventions": chốt trước mọi quyết định dùng chung để các việc khớp nhau: thư viện/cách làm, tên hàm/key/biến, đường dẫn file mới, định dạng. Worker chỉ làm theo, không tự chọn.
2. Mỗi việc sửa code chỉ làm MỘT việc đơn giản và liệt kê "files": tối đa %[2]d file nó được sửa hoặc tạo (đường dẫn tương đối từ gốc project). Worker không được đụng file khác.
3. Một file chỉ thuộc MỘT việc. Nếu hai phần cần sửa cùng file, gộp thành một việc.
4. Việc cần kết quả của việc khác thì ghi "depends_on" (số thứ tự các việc trước nó); việc nền (cài thư viện, tạo khung) đặt trước.
5. "task" phải đủ chi tiết để làm mà không cần đoán: sửa ở đâu, thành gì, theo quy ước nào; "done_when" là tiêu chí xong.
6. Không giao việc ngoài mục tiêu (không sửa cấu hình, build, file không liên quan).
Việc chỉ phân tích/đọc (agent chỉ đọc) thì bỏ trống "files".
Nếu mục tiêu đơn giản đến mức tự trả lời được ngay, để assignments rỗng và viết câu trả lời vào "answer".

Cuối câu trả lời, trả về đúng một khối `+"```json"+`:
{"analysis": "vài câu phân tích", "conventions": "các quy ước chung", "assignments": [{"agent": "key", "task": "việc chi tiết", "files": ["đường/dẫn"], "depends_on": [], "done_when": "tiêu chí"}], "answer": ""}
`, maxAssignments, maxFilesPerTask)
	return b.String()
}

func workPrompt(goal, assigner string, a Assignment, conventions, before string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s giao cho bạn một việc nhỏ trong khuôn khổ mục tiêu chung.\n\nMục tiêu chung: %s\n\nViệc của bạn: %s\n", assigner, goal, a.Task)
	if a.DoneWhen != "" {
		fmt.Fprintf(&b, "Xong khi: %s\n", a.DoneWhen)
	}
	if strings.TrimSpace(conventions) != "" {
		fmt.Fprintf(&b, "\nQuy ước chung của cả đội (bắt buộc làm theo, không tự chọn cách khác):\n%s\n", conventions)
	}
	if len(a.Files) > 0 {
		fmt.Fprintf(&b, "\nBạn CHỈ được sửa/tạo các file: %s. Diff đụng file khác sẽ bị loại. Nếu thấy cần sửa file khác, ghi vào báo cáo thay vì sửa.\n", strings.Join(a.Files, ", "))
	}
	if before != "" {
		fmt.Fprintf(&b, "\nKết quả các việc bạn phụ thuộc (các diff này sẽ được áp cùng lúc với diff của bạn; viết code khớp với chúng):\n%s\n", before)
	}
	b.WriteString("\nChỉ làm đúng việc được giao, đọc code liên quan trước khi sửa. Báo cáo ngắn gọn: đã làm gì, dẫn chứng (file:dòng), điều chưa chắc chắn.")
	return b.String()
}

func outputsBlock(steps []storage.TaskStep) string {
	var b strings.Builder
	for _, s := range steps {
		status := ""
		if s.Status == "failed" {
			status = " (thất bại: " + s.Error + ")"
		}
		fmt.Fprintf(&b, "### %s (%s)%s\nViệc: %s\nKết quả:\n%s\n\n", s.AgentName, s.AgentKey, status, s.Instruction, truncate(s.Output, 8000))
	}
	return b.String()
}

func synthesizePrompt(goal string, work []storage.TaskStep, patches []storage.Patch, extra string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Mục tiêu người dùng: %s\n\nKết quả từ đội:\n\n%s", goal, outputsBlock(work))
	if len(patches) > 0 {
		b.WriteString("Đội đã đề xuất thay đổi code (đang chờ người dùng duyệt):\n")
		for _, p := range patches {
			fmt.Fprintf(&b, "- %s [%s]\n", strings.Join(p.Files, ", "), p.Status)
		}
		b.WriteString("\n")
	}
	if extra != "" {
		b.WriteString(extra + "\n\n")
	}
	b.WriteString(`Viết câu trả lời cuối cho người dùng bằng Markdown: kết luận, dẫn chứng chính, việc nên làm tiếp. Không lặp lại diff. Nêu rõ nếu kết quả của đội mâu thuẫn hoặc chưa đủ để kết luận.`)
	return b.String()
}

func votePrompt(goal string, planner storage.Agent, plan Plan, planText string) string {
	raw, _ := json.MarshalIndent(plan.Assignments, "", "  ")
	return fmt.Sprintf(`Hội đồng đang xét kế hoạch do %s đề xuất.

Mục tiêu người dùng: %s

Phân tích của người lập kế hoạch: %s

Các việc sẽ giao:
%s

Với vai trò của bạn, xét kế hoạch có đúng mục tiêu, an toàn, đủ và không thừa không. Bạn có thể đọc project để kiểm tra.
Cuối câu trả lời, trả về đúng một khối `+"```json"+`: {"vote": "approve" hoặc "reject", "reason": "lý do ngắn"}`,
		planner.Name, goal, firstNonEmpty(plan.Analysis, truncate(planText, 2000)), raw)
}

func reviewPrompt(goal string, work []storage.TaskStep, patches []labeledPatch, round int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bạn là giám sát. Mục tiêu người dùng: %s\n\nKết quả worker báo cáo:\n\n%s", goal, outputsBlock(work))
	if round > 0 {
		fmt.Fprintf(&b, "Đây là lần kiểm tra sau vòng sửa %d: các việc bị nêu lỗi lần trước đã được làm lại.\n\n", round)
	}
	if len(patches) > 0 {
		b.WriteString("Thay đổi code worker đề xuất (cùng được áp một lúc):\n")
		for i, p := range patches {
			fmt.Fprintf(&b, "\n#%d việc %d (%s): %s", i+1, p.Job, p.Agent, strings.Join(p.Files, ", "))
			if p.Status == "failed" {
				fmt.Fprintf(&b, " [KHÔNG ÁP ĐƯỢC: %s]", truncate(p.Detail, 300))
			}
			fmt.Fprintf(&b, "\n```diff\n%s```\n", truncate(p.Diff, 6000))
		}
	}
	b.WriteString(`
Kiểm tra độc lập: dẫn chứng có thật không (hãy đọc lại file), kết luận có vượt quá dữ liệu không, thay đổi code có đúng phạm vi, gọi đúng hàm/API có thật, các diff có khớp nhau và biên dịch được không.
Chọn một kết luận:
- "pass": đạt, có thể trình người dùng.
- "fix": còn lỗi SỬA ĐƯỢC. Ghi "fixes" cho đúng những việc cần làm lại (số việc + lỗi cụ thể + cách sửa); việc đúng giữ nguyên. Đội sẽ sửa rồi bạn kiểm tra lại, lặp đến khi đạt.
- "ask": cần người dùng quyết định hoặc cung cấp thông tin mới làm tiếp (vd chọn giữa hai hướng, thiếu yêu cầu). Ghi câu hỏi ngắn, rõ vào "question".
- "fail": KHÔNG sửa được trong phạm vi này (hướng làm sai từ gốc, nguy hiểm, vượt khả năng). Mọi thay đổi bị chặn.
Cuối câu trả lời, trả về đúng một khối ` + "```json" + `:
{"verdict": "pass | fix | ask | fail", "summary": "đánh giá ngắn", "issues": ["vấn đề"], "fixes": [{"job": 1, "issue": "lỗi và cách sửa"}], "question": ""}`)
	return b.String()
}

// repairPrompt asks a worker to redo one job after review or a failed apply.
func repairPrompt(goal, assigner string, a Assignment, conventions, before, previous, issue string) string {
	var b strings.Builder
	b.WriteString(workPrompt(goal, assigner, a, conventions, before))
	fmt.Fprintf(&b, "\n\nLần làm trước của bạn cho việc này bị trả lại:\n%s\n", issue)
	if previous != "" {
		fmt.Fprintf(&b, "\nBản làm trước (để tham khảo, KHÔNG dùng lại nguyên xi):\n%s\n", truncate(previous, 8000))
	}
	b.WriteString("\nĐọc lại file hiện tại trong project (diff cũ CHƯA được áp), sửa đúng lỗi trên và đưa lại diff đầy đủ cho các file của việc này, với dòng ngữ cảnh khớp chính xác nội dung file hiện tại.")
	return b.String()
}

// parseJSON reads the last ```json block (or outermost braces) into v.
func parseJSON(text string, v any) error {
	s := text
	if i := strings.LastIndex(s, "```json"); i >= 0 {
		rest := s[i+7:]
		if j := strings.Index(rest, "```"); j >= 0 {
			s = rest[:j]
		}
	} else if i, j := strings.Index(s, "{"), strings.LastIndex(s, "}"); i >= 0 && j > i {
		s = s[i : j+1]
	}
	return json.Unmarshal([]byte(strings.TrimSpace(s)), v)
}

// stripJSON removes the trailing machine-readable block from what people see.
func stripJSON(text string) string {
	if i := strings.LastIndex(text, "```json"); i >= 0 {
		if j := strings.Index(text[i+7:], "```"); j >= 0 {
			return strings.TrimSpace(text[:i] + text[i+7+j+3:])
		}
	}
	return strings.TrimSpace(text)
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
