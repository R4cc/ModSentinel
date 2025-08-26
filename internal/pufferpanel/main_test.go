package pufferpanel

import (
	"os"
	"testing"
)

const nodeKey = "0123456789abcdef"

func TestMain(m *testing.M) {
	os.Setenv("MODSENTINEL_NODE_KEY", nodeKey)
	code := m.Run()
	os.Unsetenv("MODSENTINEL_NODE_KEY")
	os.Exit(code)
}
