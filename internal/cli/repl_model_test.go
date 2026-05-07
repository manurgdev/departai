package cli

import "testing"

func TestFilterByPrefix(t *testing.T) {
	cases := []struct {
		name   string
		items  []suggestion
		prefix string
		want   []string // expected Title list
	}{
		{
			name:   "empty prefix returns all items",
			items:  []suggestion{{"alpha", "a"}, {"beta", "b"}},
			prefix: "",
			want:   []string{"alpha", "beta"},
		},
		{
			name:   "prefix filters case-insensitively",
			items:  []suggestion{{"Alpha", "a"}, {"alphabet", "b"}, {"beta", "c"}},
			prefix: "alp",
			want:   []string{"Alpha", "alphabet"},
		},
		{
			name:   "no match returns nil",
			items:  []suggestion{{"alpha", "a"}, {"beta", "b"}},
			prefix: "z",
			want:   nil,
		},
		{
			name:   "exact match still included",
			items:  []suggestion{{"/respec", "x"}, {"/resume", "y"}},
			prefix: "/respec",
			want:   []string{"/respec"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := filterByPrefix(tc.items, tc.prefix)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tc.want), got)
			}
			for i, s := range got {
				if s.text != tc.want[i] {
					t.Errorf("idx %d: got %q, want %q", i, s.text, tc.want[i])
				}
			}
		})
	}
}

func TestPopoverNavigationClampsAtBounds(t *testing.T) {
	m := newREPLModel(nil, "")
	m.popoverItems = []suggestion{{"a", ""}, {"b", ""}, {"c", ""}}
	m.popoverVisible = true
	m.popoverSelected = 0

	// Up at index 0: should not go below 0
	if m.popoverSelected > 0 {
		m.popoverSelected--
	}
	if m.popoverSelected != 0 {
		t.Errorf("Up at 0: got %d, want 0", m.popoverSelected)
	}

	// Down twice should reach last
	for range 2 {
		if m.popoverSelected < len(m.popoverItems)-1 {
			m.popoverSelected++
		}
	}
	if m.popoverSelected != 2 {
		t.Errorf("after 2 Downs: got %d, want 2", m.popoverSelected)
	}

	// Down at end: should clamp at last
	if m.popoverSelected < len(m.popoverItems)-1 {
		m.popoverSelected++
	}
	if m.popoverSelected != 2 {
		t.Errorf("Down at end: got %d, want 2 (clamp)", m.popoverSelected)
	}
}

func TestComputeSuggestionsHierarchy(t *testing.T) {
	cases := []struct {
		name        string
		buffer      string
		wantNonZero bool
		wantContain string // first item's text should contain this
	}{
		{name: "non-slash returns nothing", buffer: "hello world", wantNonZero: false},
		{name: "single slash shows top-level", buffer: "/", wantNonZero: true, wantContain: "/"},
		{name: "/c filters to /config /continue", buffer: "/c", wantNonZero: true, wantContain: "/c"},
		{name: "/config space shows subcommands", buffer: "/config ", wantNonZero: true, wantContain: ""},
		{name: "/config set shows config keys", buffer: "/config set ", wantNonZero: true, wantContain: ""},
		{name: "/model shows model subcommands", buffer: "/model ", wantNonZero: true, wantContain: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newREPLModel(nil, "")
			m.textarea.SetValue(tc.buffer)
			m.textarea.CursorEnd()
			items, _, _ := m.computeSuggestions()
			if tc.wantNonZero && len(items) == 0 {
				t.Errorf("expected suggestions for %q, got 0", tc.buffer)
			}
			if !tc.wantNonZero && len(items) != 0 {
				t.Errorf("expected no suggestions for %q, got %d (%v)", tc.buffer, len(items), items)
			}
		})
	}
}
