package cli

import (
	"reflect"
	"testing"
)

func TestPrepareArgs(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"ws", "X", "--repos"}, []string{"ws", "X", "--repos="}},
		{[]string{"ws", "X", "--repos", "--bundles"}, []string{"ws", "X", "--repos=", "--bundles="}},
		{[]string{"ws", "X", "--repos", "a,b@main"}, []string{"ws", "X", "--repos", "a,b@main"}},
		{[]string{"ws", "X", "--repos=a"}, []string{"ws", "X", "--repos=a"}},
		{[]string{"__complete", "ws", "X", "--repos"}, []string{"__complete", "ws", "X", "--repos"}},
	}
	for _, c := range cases {
		if got := PrepareArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("PrepareArgs(%v) = %v; want %v", c.in, got, c.want)
		}
	}
}
