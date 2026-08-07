package handler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	managedRuleCompilerPathEnv = "ZERO_RULE_COMPILER_PATH"
	managedRuleCompilerDefault = "zrs-compiler"
	managedRuleCompilerTimeout = 60 * time.Second
)

var managedRuleZRSCompiler = compileManagedRuleZRSWithCommand

func compileManagedRuleZRSWithCommand(source []byte) ([]byte, error) {
	compilerPath := strings.TrimSpace(os.Getenv(managedRuleCompilerPathEnv))
	if compilerPath == "" {
		compilerPath = managedRuleCompilerDefault
	}

	workDir, err := os.MkdirTemp("", "zboard-zrs-compile-*")
	if err != nil {
		return nil, fmt.Errorf("create ZRS compiler workspace: %w", err)
	}
	defer os.RemoveAll(workDir)

	inputPath := filepath.Join(workDir, managedRuleSourceFileName)
	outputPath := filepath.Join(workDir, "rules.zrs")
	if err := os.WriteFile(inputPath, source, 0o600); err != nil {
		return nil, fmt.Errorf("write ZRS compiler input: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), managedRuleCompilerTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, compilerPath, "compile", "--input", inputPath, "--output", outputPath).CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("compile ZRS: timeout after %s", managedRuleCompilerTimeout)
		}
		message := strings.TrimSpace(string(output))
		if len(message) > 4096 {
			message = message[:4096]
		}
		if message == "" {
			return nil, fmt.Errorf("compile ZRS with %q: %w", compilerPath, err)
		}
		return nil, fmt.Errorf("compile ZRS with %q: %w: %s", compilerPath, err, message)
	}

	artifact, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("read compiled ZRS: %w", err)
	}
	if len(artifact) == 0 {
		return nil, errors.New("compile ZRS: compiler returned an empty artifact")
	}
	return artifact, nil
}
