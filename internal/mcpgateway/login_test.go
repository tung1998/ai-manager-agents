package mcpgateway

import (
	"testing"
	"time"
)

func TestLoginsTakeKeepsEntryForWrongUser(t *testing.T) {
	var l logins
	l.put("s1", pendingLogin{user: "A", exp: time.Now().Add(time.Minute)})

	if _, ok := l.take("s1", "B"); ok {
		t.Fatal("take with wrong user should fail")
	}
	if _, ok := l.take("s1", "A"); !ok {
		t.Fatal("take with right user should still succeed after a wrong-user attempt")
	}
	if _, ok := l.take("s1", "A"); ok {
		t.Fatal("take should only succeed once")
	}
}
