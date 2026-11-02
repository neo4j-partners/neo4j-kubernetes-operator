package platform

import "testing"

func TestResolve(t *testing.T) {
	cases := []struct {
		explicit string
		detected bool
		want     Mode
	}{
		{"openshift", false, OpenShift},   // explicit wins over detection
		{"OpenShift", false, OpenShift},   // case-insensitive
		{"kubernetes", true, Kubernetes},  // explicit wins over detection
		{"", true, OpenShift},             // unset → autodetect
		{"", false, Kubernetes},           // unset → autodetect
		{"auto", true, OpenShift},         // auto → autodetect
		{"auto", false, Kubernetes},       // auto → autodetect
		{"garbage", true, OpenShift},      // unrecognized falls back to autodetect
		{"  openshift  ", false, OpenShift}, // trimmed
	}
	for _, c := range cases {
		if got := Resolve(c.explicit, c.detected); got != c.want {
			t.Errorf("Resolve(%q, %v) = %q, want %q", c.explicit, c.detected, got, c.want)
		}
	}
}

func TestSetFallsBackToKubernetes(t *testing.T) {
	t.Cleanup(func() { Set(Kubernetes) })

	Set(OpenShift)
	if !IsOpenShift() {
		t.Fatal("Set(OpenShift) should make IsOpenShift true")
	}
	// Auto is not a resolved mode; Set must not leave it stored as OpenShift.
	Set(Auto)
	if IsOpenShift() {
		t.Fatal("Set(Auto) must fall back to Kubernetes, not stay OpenShift")
	}
}
