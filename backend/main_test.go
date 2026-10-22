package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMainExitsNamingMissingVariables(t *testing.T) {
	if os.Getenv("BACKEND_MAIN_SUBPROCESS") == "1" {
		os.Clearenv()
		os.Setenv("JWT_SECRET", "secret-value-never-logged")
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestMainExitsNamingMissingVariables$")
	command.Env = []string{"BACKEND_MAIN_SUBPROCESS=1"}
	output, err := command.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("exit: %v, output %q", err, output)
	}
	if !strings.Contains(string(output), "missing environment variables: STEAM_API_KEY, PUBLIC_URL, PUBLIC_API_URL, DOWNLOADS_DIR") {
		t.Fatalf("output %q", output)
	}
	if strings.Contains(string(output), "secret-value-never-logged") {
		t.Fatal("a value was logged")
	}
}
