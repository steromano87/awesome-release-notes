package gitlog

import (
	"slices"
	"testing"
)

func TestSplitTrailer(t *testing.T) {
	tests := []struct {
		name, body, wantBody, wantTrailer string
		wantCoAuthors                     []User
	}{
		{"empty", "", "", "", []User{}},
		{"no trailer", "Just text.\n\nMore: text here\nand more.", "Just text.\n\nMore: text here\nand more.", "", []User{}},
		{"trailer only", "Co-authored-by: Ada <ada@example.com>", "", "Co-authored-by: Ada <ada@example.com>",
			[]User{{"Ada", "ada@example.com"}}},
		{"body and trailers", "Details.\n\nSigned-off-by: Me <me@example.com>\nco-authored-by: Ada L <ada@example.com>\nCo-Authored-By: Bob <bob@example.com>",
			"Details.", "Signed-off-by: Me <me@example.com>\nco-authored-by: Ada L <ada@example.com>\nCo-Authored-By: Bob <bob@example.com>",
			[]User{{"Ada L", "ada@example.com"}, {"Bob", "bob@example.com"}}},
		{"github separator", "* fix thing\n\n* other\n\n---------\n\nCo-authored-by: Ada <ada@example.com>", "* fix thing\n\n* other",
			"Co-authored-by: Ada <ada@example.com>", []User{{"Ada", "ada@example.com"}}},
		{"separator only before trailer", "---------\n\nCo-authored-by: Ada <ada@example.com>", "",
			"Co-authored-by: Ada <ada@example.com>", []User{{"Ada", "ada@example.com"}}},
		{"separator without trailer is kept", "Text.\n\n---------", "Text.\n\n---------", "", []User{}},
		{"continuation", "Text.\n\nKey: value\n  continued", "Text.", "Key: value\n  continued", []User{}},
		{"mixed last paragraph", "Text.\n\nKey: value\nnot a trailer", "Text.\n\nKey: value\nnot a trailer", "", []User{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, trailer := splitTrailer(tt.body)
			got := coAuthors(trailer)
			if body != tt.wantBody || trailer != tt.wantTrailer || got == nil || !slices.Equal(got, tt.wantCoAuthors) {
				t.Fatalf("got %q, %q, %v", body, trailer, got)
			}
		})
	}
}
