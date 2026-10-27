package jobs

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestSelectMatchesSort(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 7))
	for trial := 0; trial < 200; trial++ {
		n := 1 + r.IntN(300)
		xs := make([]float64, n)
		for i := range xs {
			xs[i] = float64(r.IntN(50)) // duplicates
		}
		sorted := slices.Clone(xs)
		slices.Sort(sorted)
		k := r.IntN(n)
		if got := Select(xs, k); got != sorted[k] {
			t.Fatalf("n=%d k=%d got %v want %v", n, k, got, sorted[k])
		}
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		typ, payload string
		ok           bool
	}{
		{"echo", `{"message":"hi"}`, true},
		{"sleep", `{"ms":-1}`, false},
		{"flaky", `{"fail_rate":1.5}`, false},
		{"synthgen", `{"rows":0}`, false},
		{"synthgen", `{"rows":100,"seed":1}`, true},
		{"nope", `{}`, false},
		{"echo", `not json`, false},
	}
	for _, c := range cases {
		if err := Validate(c.typ, json.RawMessage(c.payload)); (err == nil) != c.ok {
			t.Errorf("%s %s: err=%v want ok=%v", c.typ, c.payload, err, c.ok)
		}
	}
}

func TestSynthgenDeterministic(t *testing.T) {
	h := Handlers["synthgen"]
	a, err := h(context.Background(), json.RawMessage(`{"rows":5000,"seed":42}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := h(context.Background(), json.RawMessage(`{"rows":5000,"seed":42}`))
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatalf("same seed gave %s and %s", ja, jb)
	}
	m := a.(map[string]any)
	if m["p95"].(float64) <= m["mean"].(float64) {
		t.Fatalf("p95 should exceed mean for a right-skewed distribution: %v", m)
	}
}
