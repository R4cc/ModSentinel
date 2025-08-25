package handlers

import "testing"

func TestParseJarFilename(t *testing.T) {
	tests := []struct {
		name string
		file string
		want jarMeta
	}{
		{
			name: "sodium",
			file: "sodium-fabric-mc1.20.1-0.4.10.jar",
			want: jarMeta{Slug: "sodium", ID: "sodium", Version: "0.4.10", MCVersion: "1.20.1", Loader: "fabric"},
		},
		{
			name: "jei",
			file: "jei-1.20.1-forge-15.2.0.27.jar",
			want: jarMeta{Slug: "jei", ID: "jei", Version: "15.2.0.27", MCVersion: "1.20.1", Loader: "forge"},
		},
		{
			name: "fabric-api",
			file: "fabric-api-0.86.1+1.20.1.jar",
			want: jarMeta{Slug: "fabric-api", ID: "fabric", Version: "0.86.1", MCVersion: "1.20.1", Loader: "fabric"},
		},
		{
			name: "beta channel",
			file: "awesome-mod-1.2.3-beta.jar",
			want: jarMeta{Slug: "awesome-mod", ID: "awesome", Version: "1.2.3", Channel: "beta"},
		},
		{
			name: "rc channel",
			file: "example-rc-v2.0.0.jar",
			want: jarMeta{Slug: "example", ID: "example", Version: "2.0.0", Channel: "rc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseJarFilename(tt.file)
			if got.Slug != tt.want.Slug || got.ID != tt.want.ID || got.Version != tt.want.Version || got.MCVersion != tt.want.MCVersion || got.Loader != tt.want.Loader || got.Channel != tt.want.Channel {
				t.Errorf("parseJarFilename(%q) = %+v, want %+v", tt.file, got, tt.want)
			}
		})
	}
}
