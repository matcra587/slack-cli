package channel

import (
	"slices"
	"testing"
)

func TestParseConversationTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "blank input defaults to public and private channels",
			want: []string{"public_channel", "private_channel"},
		},
		{
			name: "empty CSV segments default to public and private channels",
			in:   ",,  ,",
			want: []string{"public_channel", "private_channel"},
		},
		{
			name: "all short-circuits to every supported conversation type",
			in:   "im,all,public_channel",
			want: []string{"public_channel", "private_channel", "im", "mpim"},
		},
		{
			name: "literal repeats keep first occurrence order",
			in:   "im,public_channel,im,private_channel,public_channel",
			want: []string{"im", "public_channel", "private_channel"},
		},
		{
			name: "aliases dedupe after normalization",
			in:   "channels, public, public_channel, groups, private, dm, dms, group-dm, mpim",
			want: []string{"public_channel", "private_channel", "im", "mpim"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseConversationTypes(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("parseConversationTypes(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}
