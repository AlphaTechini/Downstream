package scan

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/app/auth.go b/app/auth.go
index 1111111..2222222 100644
--- a/app/auth.go
+++ b/app/auth.go
@@ -10 +10 @@ func hashSecret() {
-	const rounds = 3
+	const rounds = 4
@@ -20,2 +20,3 @@ func other() {
-a
-b
+c
+d
+e
@@ -40,2 +41,0 @@ func gone() {
-x
-y
diff --git a/app/new.go b/app/new.go
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/app/new.go
@@ -0,0 +1,2 @@
+package app
+
diff --git a/app/old.go b/app/old.go
deleted file mode 100644
index 4444444..0000000
--- a/app/old.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package app
-
diff --git a/app/before.go b/app/after.go
similarity index 90%
rename from app/before.go
rename to app/after.go
index 5555555..6666666 100644
--- a/app/before.go
+++ b/app/after.go
@@ -3 +3 @@
-old
+new
\ No newline at end of file
`

func TestParseDiff(t *testing.T) {
	files, err := parseDiff(sampleDiff)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("got %d files, want 4", len(files))
	}

	a := files[0]
	if a.Path != "app/auth.go" || a.Status != statusModified || len(a.Hunks) != 3 {
		t.Fatalf("file 0: %+v", a)
	}
	h := a.Hunks[0]
	if h.OldStart != 10 || h.OldLines != 1 || h.NewStart != 10 || h.NewLines != 1 {
		t.Errorf("omitted counts must mean 1: %+v", h)
	}
	if h.Text != "-\tconst rounds = 3\n+\tconst rounds = 4" {
		t.Errorf("hunk text: %q", h.Text)
	}
	h = a.Hunks[1]
	if h.OldStart != 20 || h.OldLines != 2 || h.NewStart != 20 || h.NewLines != 3 {
		t.Errorf("hunk 1: %+v", h)
	}
	h = a.Hunks[2]
	if h.OldStart != 40 || h.OldLines != 2 || h.NewStart != 41 || h.NewLines != 0 {
		t.Errorf("deletion-only hunk: %+v", h)
	}

	n := files[1]
	if n.Path != "app/new.go" || n.Status != statusAdded || n.OldPath != "" {
		t.Errorf("added file: %+v", n)
	}
	if n.Hunks[0].OldStart != 0 || n.Hunks[0].OldLines != 0 || n.Hunks[0].NewLines != 2 {
		t.Errorf("added hunk: %+v", n.Hunks[0])
	}

	d := files[2]
	if d.Path != "app/old.go" || d.Status != statusDeleted {
		t.Errorf("deleted file: %+v", d)
	}
	if d.Hunks[0].NewStart != 0 || d.Hunks[0].NewLines != 0 {
		t.Errorf("deleted hunk: %+v", d.Hunks[0])
	}

	r := files[3]
	if r.Path != "app/after.go" || r.OldPath != "app/before.go" || r.Status != statusRenamed {
		t.Errorf("renamed file: %+v", r)
	}
	if strings.Contains(r.Hunks[0].Text, "No newline") {
		t.Errorf("no-newline marker should not be in the hunk text: %q", r.Hunks[0].Text)
	}
}

func TestParseDiffRemovedLineLooksLikeHeader(t *testing.T) {
	in := "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,0 @@\n--- not a header\n-- another\n"
	files, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "f.txt" || len(files[0].Hunks) != 1 {
		t.Fatalf("%+v", files)
	}
}

func TestParseDiffQuotedAndSpacedPaths(t *testing.T) {
	in := "diff --git a/my dir/x y.go b/my dir/x y.go\n--- a/my dir/x y.go\t\n+++ b/my dir/x y.go\t\n@@ -1 +1 @@\n-a\n+b\n" +
		"diff --git \"a/caf\\303\\251.go\" \"b/caf\\303\\251.go\"\n--- \"a/caf\\303\\251.go\"\n+++ \"b/caf\\303\\251.go\"\n@@ -2 +2 @@\n-a\n+b\n" +
		"diff --git a/empty.go b/empty.go\nnew file mode 100644\nindex 0000000..e69de29\n"
	files, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files", len(files))
	}
	if files[0].Path != "my dir/x y.go" {
		t.Errorf("spaced path: %q", files[0].Path)
	}
	if files[1].Path != "café.go" {
		t.Errorf("quoted path: %q", files[1].Path)
	}
	if files[2].Path != "empty.go" || files[2].Status != statusAdded || len(files[2].Hunks) != 0 {
		t.Errorf("empty added file: %+v", files[2])
	}
}

func TestParseDiffHeaderOnlyFile(t *testing.T) {
	// A pure rename has no hunks and no ---/+++ lines.
	in := "diff --git a/a.go b/b.go\nsimilarity index 100%\nrename from a.go\nrename to b.go\n"
	files, err := parseDiff(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "b.go" || files[0].OldPath != "a.go" || files[0].Status != statusRenamed {
		t.Fatalf("%+v", files)
	}
}

func TestParseDiffCapsHunkText(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -0,0 +1,300 @@\n")
	for i := 0; i < 300; i++ {
		b.WriteString("+0123456789abcdef0123456789abcdef\n")
	}
	files, err := parseDiff(b.String())
	if err != nil {
		t.Fatal(err)
	}
	text := files[0].Hunks[0].Text
	if len(text) > maxHunkText || !strings.HasSuffix(text, truncMark) {
		t.Errorf("text length %d, tail %q", len(text), text[len(text)-20:])
	}
	if files[0].Hunks[0].NewLines != 300 {
		t.Errorf("counts must survive the cap: %+v", files[0].Hunks[0])
	}
}

func TestParseDiffErrors(t *testing.T) {
	for name, in := range map[string]string{
		"bad header": "diff --git a/f b/f\n@@ nonsense @@\n",
		"truncated":  "diff --git a/f b/f\n@@ -1,3 +1,3 @@\n-a\n",
		"bad line":   "diff --git a/f b/f\n@@ -1 +1 @@\n?x\n",
	} {
		if _, err := parseDiff(in); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseDiffEmpty(t *testing.T) {
	files, err := parseDiff("")
	if err != nil || len(files) != 0 {
		t.Fatalf("%v %v", files, err)
	}
}
