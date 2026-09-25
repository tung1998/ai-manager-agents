package tasks

import (
	"encoding/json"
	"fmt"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

const maxAssignments = 6

// Plan is what a lead / planner returns.
type Plan struct {
	Analysis    string       `json:"analysis"`
	Answer      string       `json:"answer,omitempty"` // when no delegation is needed
	Assignments []Assignment `json:"assignments"`
}

// Assignment hands work to one agent.
type Assignment struct {
	Agent string `json:"agent"`
	Task  string `json:"task"`
}

// Vote is a council member's decision on a plan.
type Vote struct {
	Vote   string `json:"vote"` // approve | reject
	Reason string `json:"reason"`
}

// Verdict is the auditor's review of the work.
type Verdict struct {
	Verdict      string   `json:"verdict"` // pass | fail
	Summary      string   `json:"summary"`
	Issues       []string `json:"issues"`
	BlockChanges bool     `json:"block_changes"`
}

func roster(agents []storage.Agent) string {
	var b strings.Builder
	for _, a := range agents {
		access := "chỉ đọc"
		if !a.Permissions.ReadOnly {
			access = "được đề xuất sửa code (diff, người duyệt)"
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
	fmt.Fprintf(&b, `Lập kế hoạch: đọc nhanh project nếu cần, rồi giao tối đa %d việc cụ thể, mỗi việc cho đúng một agent phù hợp nhất (một agent có thể nhận nhiều việc). Mỗi việc phải tự đủ nghĩa: nói rõ cần tìm/đọc/làm gì và trả về gì.
Nếu mục tiêu đơn giản đến mức tự trả lời được ngay, để assignments rỗng và viết câu trả lời vào "answer".

Cuối câu trả lời, trả về đúng một khối `+"```json"+`:
{"analysis": "vài câu phân tích", "assignments": [{"agent": "key", "task": "mô tả việc"}], "answer": ""}
`, maxAssignments)
	return b.String()
}

func workPrompt(goal, assigner, task string) string {
	return fmt.Sprintf(`%s giao cho bạn một việc trong khuôn khổ mục tiêu chung.

Mục tiêu chung: %s

Việc của bạn: %s

Làm đúng việc được giao, dùng công cụ đọc để lấy dẫn chứng. Báo cáo ngắn gọn: kết quả, dẫn chứng (file:dòng), điều chưa chắc chắn.`, assigner, goal, task)
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

func reviewPrompt(goal string, work []storage.TaskStep, patches []storage.Patch) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bạn là giám sát. Mục tiêu người dùng: %s\n\nKết quả worker báo cáo:\n\n%s", goal, outputsBlock(work))
	if len(patches) > 0 {
		b.WriteString("Thay đổi code worker đề xuất:\n")
		for i, p := range patches {
			fmt.Fprintf(&b, "\n#%d %s\n```diff\n%s```\n", i+1, strings.Join(p.Files, ", "), truncate(p.Diff, 6000))
		}
	}
	b.WriteString(`
Kiểm tra độc lập: dẫn chứng có thật không (hãy đọc lại file), kết luận có vượt quá dữ liệu không, thay đổi code có an toàn và đúng phạm vi không.
Cuối câu trả lời, trả về đúng một khối ` + "```json" + `:
{"verdict": "pass" hoặc "fail", "summary": "đánh giá ngắn", "issues": ["vấn đề"], "block_changes": true nếu phải chặn mọi thay đổi code}`)
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
