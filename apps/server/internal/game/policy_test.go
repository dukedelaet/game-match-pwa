package game

import "testing"

func TestIntentsOK(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"dating+dating", []string{"dating", "gaming"}, []string{"dating"}, true},
		{"dating vs friendship", []string{"dating"}, []string{"friendship"}, false},
		{"dating vs gaming", []string{"dating"}, []string{"gaming"}, false},
		{"friendship vs gaming", []string{"friendship"}, []string{"gaming"}, true},
		{"empty a", nil, []string{"gaming"}, false},
		{"empty b", []string{"gaming"}, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IntentsOK(tc.a, tc.b); got != tc.want {
				t.Fatalf("IntentsOK(%v,%v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestLevelForXP(t *testing.T) {
	cases := []struct {
		xp   int
		want int
	}{
		{0, 1}, {99, 1}, {100, 2}, {299, 2}, {300, 3}, {600, 4},
	}
	for _, tc := range cases {
		if got := LevelForXP(tc.xp); got != tc.want {
			t.Fatalf("LevelForXP(%d) = %d, want %d", tc.xp, got, tc.want)
		}
	}
}

func TestOrderedPair(t *testing.T) {
	a, b := OrderedPair("bbb", "aaa")
	if a != "aaa" || b != "bbb" {
		t.Fatalf("OrderedPair = (%s,%s), want (aaa,bbb)", a, b)
	}
}

func TestJsonEqualIsOrderInsensitive(t *testing.T) {
	if !jsonEqual(`{"choice":"left"}`, `{"choice":"left"}`) {
		t.Fatal("identical payloads should compare equal")
	}
	if !jsonEqual(`{"a":1,"b":2}`, `{"b":2,"a":1}`) {
		t.Fatal("key order should not matter")
	}
	if jsonEqual(`{"optionId":"a"}`, `{"optionId":"b"}`) {
		t.Fatal("different optionIds should not compare equal")
	}
}
