package callexpr

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		server  string
		op      string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:   "json form",
			expr:   `fs.read_file({"path": "/tmp/a.txt", "lines": 10})`,
			server: "fs", op: "read_file",
			args: map[string]interface{}{"path": "/tmp/a.txt", "lines": int64(10)},
		},
		{
			name:   "collapsed form single quotes",
			expr:   `fs.read_file(path: '/tmp/a.txt', lines: 10)`,
			server: "fs", op: "read_file",
			args: map[string]interface{}{"path": "/tmp/a.txt", "lines": int64(10)},
		},
		{
			name:   "no args",
			expr:   `time.now()`,
			server: "time", op: "now",
			args: map[string]interface{}{},
		},
		{
			name:   "booleans null floats",
			expr:   `s.op(a: true, b: false, c: null, d: 1.5)`,
			server: "s", op: "op",
			args: map[string]interface{}{"a": true, "b": false, "c": nil, "d": 1.5},
		},
		{
			name:   "nested object and array",
			expr:   `s.op(filter: {kind: 'file', tags: ['x', 'y']}, n: 3)`,
			server: "s", op: "op",
			args: map[string]interface{}{
				"filter": map[string]interface{}{
					"kind": "file",
					"tags": []interface{}{"x", "y"},
				},
				"n": int64(3),
			},
		},
		{
			name:   "trailing comma collapsed",
			expr:   `s.op(a: 1,)`,
			server: "s", op: "op",
			args: map[string]interface{}{"a": int64(1)},
		},
		{
			name:    "missing dot",
			expr:    `noop(a: 1)`,
			wantErr: true,
		},
		{
			name:    "missing parens",
			expr:    `s.op`,
			wantErr: true,
		},
		{
			name:    "bare unquoted value",
			expr:    `s.op(a: hello)`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.expr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Server != tt.server || got.Operation != tt.op {
				t.Fatalf("server/op = %q/%q, want %q/%q", got.Server, got.Operation, tt.server, tt.op)
			}
			if !reflect.DeepEqual(got.Arguments, tt.args) {
				t.Fatalf("args = %#v, want %#v", got.Arguments, tt.args)
			}
		})
	}
}
