package tools

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A shebang is one line. Reading a fixed block and splitting the whole thing
// used to append the first line of the script body as interpreter arguments, so
// a bare script path was launched as `sh while ...` and failed with
// "while: No such file or directory".
func TestResolveInterpreterUsesOnlyTheShebangLine(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "shell shebang",
			content: "#!/bin/sh\nwhile true; do echo hi; done\n",
			want:    []string{"/bin/sh"},
		},
		{
			name:    "env python",
			content: "#!/usr/bin/env python3\nprint('hi')\n",
			want:    []string{"python3"},
		},
		{
			name:    "interpreter with args",
			content: "#!/usr/bin/env -S node --experimental\nconsole.log(1)\n",
			want:    []string{"-S", "node", "--experimental"},
		},
		{
			name:    "no shebang falls back to extension",
			content: "print('hi')\n",
			want:    nil, // .txt has no mapping
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext := ".txt"
			if tc.want != nil {
				ext = ".sh"
			}
			got := resolveInterpreter(write("script"+ext, tc.content))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolveInterpreter = %v, want %v", got, tc.want)
			}
		})
	}

	// A .py file without a shebang still resolves through the extension table.
	if got := resolveInterpreter(write("tool.py", "print(1)\n")); !reflect.DeepEqual(got, []string{"python3"}) {
		t.Fatalf("bare .py = %v, want [python3]", got)
	}
}
