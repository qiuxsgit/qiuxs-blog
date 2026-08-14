package buildtarget

import "testing"

func TestSnapshotValidationMatchesCanonicalBuilderTargetRules(t *testing.T) {
	valid := Snapshot{Name: "Production", BaseURL: "https://jenkins.example.com:8443", Username: "builder", JobName: "blog/deploy"}
	if !Valid(valid) {
		t.Fatal("expected canonical builder target to be valid")
	}
	for name, mutate := range map[string]func(*Snapshot){
		"trimmed name":   func(value *Snapshot) { value.Name = " Production" },
		"canonical URL":  func(value *Snapshot) { value.BaseURL = "https://jenkins.example.com:443" },
		"username colon": func(value *Snapshot) { value.Username = "build:er" },
		"job traversal":  func(value *Snapshot) { value.JobName = "blog/../deploy" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if Valid(candidate) {
				t.Fatal("expected non-canonical builder target to be rejected")
			}
		})
	}
}
