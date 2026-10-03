package sudoku

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	hexPublicKeyRE  = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
	hexPrivateKeyRE = regexp.MustCompile(`^[0-9a-fA-F]{64}(?:[0-9a-fA-F]{64})?$`)
)

func ValidPublicKey(key string) bool {
	return hexPublicKeyRE.MatchString(strings.TrimSpace(key))
}

func ValidPrivateKey(key string) bool {
	return hexPrivateKeyRE.MatchString(strings.TrimSpace(key))
}

func GenerateMasterKey(ctx context.Context, binary string) (publicKey, privateKey string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-keygen")
	cmd.Env = append(os.Environ(), "SUDOKU_LOG_LEVEL=info", "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("Sudoku keygen failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	publicKey = findLabeledValue(string(out), "Master Public Key:")
	privateKey = findLabeledValue(string(out), "Master Private Key:")
	if !ValidPrivateKey(privateKey) || !ValidPublicKey(publicKey) {
		return "", "", fmt.Errorf("Sudoku keygen returned invalid master keys")
	}
	return publicKey, privateKey, nil
}

func GenerateSplitKey(ctx context.Context, binary, masterPrivate string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-keygen", "-more", masterPrivate)
	cmd.Env = append(os.Environ(), "SUDOKU_LOG_LEVEL=info", "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Sudoku split-key generation failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	key := findLabeledValue(string(out), "Split Private Key:")
	if !ValidPrivateKey(key) {
		return "", fmt.Errorf("Sudoku keygen returned invalid split key")
	}
	return key, nil
}

func findLabeledValue(output, label string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// Upstream emits "HH:MM:SS info [CLI] <label> <key>". Older
		// versions print the label directly without a logging prefix.
		if index := strings.Index(line, label); index >= 0 {
			return strings.TrimSpace(line[index+len(label):])
		}
	}
	return ""
}
