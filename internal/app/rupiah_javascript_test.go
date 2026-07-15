package app_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRupiahBrowserJavascript(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate Rupiah JavaScript test")
	}
	testFile := filepath.Join(filepath.Dir(filename), "static", "vendor", "rupiah-inputs.test.mjs")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", "--test", testFile)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Rupiah browser JavaScript test failed: %v\n%s", err, output)
	}
}
