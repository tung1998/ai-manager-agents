package peer

import (
	"context"
	"errors"
	"net/http"
)

// Asking an expert of another office (ADR-177): on its own endpoints, never
// /api/peer/relay, so an office that does not know them refuses the question
// (404) rather than taking it for work handed over.

// ConsultFrom is where a question comes from, as the asked office shows it.
type ConsultFrom struct {
	Chat    string `json:"chat"`
	Project string `json:"project,omitempty"` // its project's name
	Agent   string `json:"agent,omitempty"`
}

// ConsultRequest asks Agent of Project there; ID is the asking office's.
type ConsultRequest struct {
	ID       string      `json:"id"`
	Project  string      `json:"project"`
	Agent    string      `json:"agent"`
	Question string      `json:"question"`
	From     ConsultFrom `json:"from"`
}

// ConsultReply names the expert that took the question.
type ConsultReply struct {
	Agent   string `json:"agent"`
	Project string `json:"project"`
}

// ConsultAnswer is the expert's answer to question ID (or why there is none).
type ConsultAnswer struct {
	ID     string `json:"id"`
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

// MaxQuestion caps a question's and an answer's length.
const MaxQuestion = 8000

// Consult asks an expert of the paired office id.
func (s *Service) Consult(ctx context.Context, id string, q ConsultRequest) (ConsultReply, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return ConsultReply{}, errors.New("office liên kết không còn (đã gỡ ghép nối?)")
	}
	var out ConsultReply
	err = s.call(ctx, p, http.MethodPost, "/api/peer/consult", q, &out)
	return out, err
}

// AnswerConsult gives the paired office id the answer to its question.
func (s *Service) AnswerConsult(ctx context.Context, id string, a ConsultAnswer) error {
	p, err := s.Get(ctx, id)
	if err != nil {
		return errors.New("office liên kết không còn (đã gỡ ghép nối?)")
	}
	return s.call(ctx, p, http.MethodPost, "/api/peer/consult/answer", a, nil)
}
