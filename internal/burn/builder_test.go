package burn

import (
	"os"
	"path/filepath"
	"testing"
)

// Features, read for the project page's progress block (ADR-139): none
// before features.json exists, either JSON shape once it does.
func TestFeaturesForDisplay(t *testing.T) {
	if got := Features(""); got != nil {
		t.Fatalf("no dir: %v", got)
	}
	dir := t.TempDir()
	if got := Features(dir); got != nil {
		t.Fatalf("no features.json yet: %v", got)
	}
	os.WriteFile(filepath.Join(dir, featuresFile), []byte(`[{"id":"f1","title":"Đăng nhập","passes":true},{"id":"f2","title":"Thanh toán","passes":false}]`), 0o644)
	got := Features(dir)
	if len(got) != 2 || got[0] != (Feature{"f1", "Đăng nhập", true}) || got[1] != (Feature{"f2", "Thanh toán", false}) {
		t.Fatalf("array form: %+v", got)
	}
	os.WriteFile(filepath.Join(dir, featuresFile), []byte(`{"features":[{"id":"f1","title":"Đăng nhập","passes":true}]}`), 0o644)
	if got := Features(dir); len(got) != 1 || got[0] != (Feature{"f1", "Đăng nhập", true}) {
		t.Fatalf("wrapped form: %+v", got)
	}
}
