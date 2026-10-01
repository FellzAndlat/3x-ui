//go:build linux

package mtproto

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// MEKO's iOS SYN signature. The rule recognizes the TCP-option layout used by
// Telegram/iOS and bypasses the non-iOS SYN rate limiter, matching the v3 fix.
const mekoIOSU32 = "32 & 0x000FFFFF = 0x0002FFFF && 40 & 0xFF000000 = 0x02000000 && 44 & 0xFFFF0000 = 0x01030000 && 48 & 0xFFFFFF00 = 0x01010800 && 60 & 0xFFFFFFFF = 0x04020000"

func applyMekoFix(id, port int, cfg MekoFixConfig) error {
	if !cfg.Enabled {
		removeMekoFix(id)
		return nil
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid inbound port %d", port)
	}
	if os.Geteuid() != 0 {
		return errors.New("MEKO SYN fix requires root privileges")
	}
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" || backend == "auto" {
		if _, err := exec.LookPath("nft"); err == nil {
			if err := applyMekoNft(id, port, cfg); err == nil {
				removeMekoIptables(id)
				return nil
			}
		}
		if _, err := exec.LookPath("iptables"); err == nil {
			if err := applyMekoIptables(id, port, cfg); err != nil {
				return err
			}
			removeMekoNft(id)
			return nil
		}
		return errors.New("neither nft nor iptables is available")
	}
	switch backend {
	case "nftables":
		if _, err := exec.LookPath("nft"); err != nil {
			return errors.New("nft backend selected but nft is not installed")
		}
		if err := applyMekoNft(id, port, cfg); err != nil {
			return err
		}
		removeMekoIptables(id)
		return nil
	case "iptables":
		if _, err := exec.LookPath("iptables"); err != nil {
			return errors.New("iptables backend selected but iptables is not installed")
		}
		if err := applyMekoIptables(id, port, cfg); err != nil {
			return err
		}
		removeMekoNft(id)
		return nil
	default:
		return fmt.Errorf("unsupported MEKO fix backend %q", cfg.Backend)
	}
}

func removeMekoFix(id int) {
	removeMekoNft(id)
	removeMekoIptables(id)
}

func mekoNftTable(id int) string { return fmt.Sprintf("xui_mtproto_%d", id) }

func applyMekoNft(id, port int, cfg MekoFixConfig) error {
	removeMekoNft(id)
	table := mekoNftTable(id)
	rate := max(cfg.SynRatePerMinute, 1)
	burst := max(cfg.Burst, 1)
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", table)
	fmt.Fprintf(&b, "add chain inet %s input { type filter hook input priority filter; policy accept; }\n", table)
	if cfg.IOSBypass {
		fmt.Fprintf(&b, "add rule inet %s input tcp dport %d tcp flags & (syn | ack) == syn @th,108,20 0x2ffff @th,160,16 0x204 @th,192,16 0x103 @th,224,24 0x10108 @th,320,32 0x4020000 counter accept comment \"ios_accept\"\n", table, port)
	}
	fmt.Fprintf(&b, "add rule inet %s input tcp dport %d tcp flags & (syn | ack) == syn meter other4 { ip saddr timeout 60s limit rate %d/minute burst %d packets } counter accept comment \"other_accept\"\n", table, port, rate, burst)
	fmt.Fprintf(&b, "add rule inet %s input tcp dport %d tcp flags & (syn | ack) == syn meter other6 { ip6 saddr timeout 60s limit rate %d/minute burst %d packets } counter accept comment \"other_accept6\"\n", table, port, rate, burst)
	fmt.Fprintf(&b, "add rule inet %s input tcp dport %d tcp flags & (syn | ack) == syn counter reject with tcp reset comment \"syn_reject\"\n", table, port)
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader(b.String())
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		removeMekoNft(id)
		return fmt.Errorf("apply nftables rules: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func removeMekoNft(id int) {
	if _, err := exec.LookPath("nft"); err != nil {
		return
	}
	_ = exec.Command("nft", "delete", "table", "inet", mekoNftTable(id)).Run()
}

func mekoFilterChain(id int) string  { return fmt.Sprintf("XUIMTF%d", id) }
func mekoMangleChain(id int) string  { return fmt.Sprintf("XUIMTM%d", id) }
func mekoFilter6Chain(id int) string { return fmt.Sprintf("XUIMT6%d", id) }

func applyMekoIptables(id, port int, cfg MekoFixConfig) error {
	removeMekoIptables(id)
	filterChain, mangleChain := mekoFilterChain(id), mekoMangleChain(id)
	rate := max(cfg.SynRatePerMinute, 1)
	burst := max(cfg.Burst, 1)

	if err := ipt("iptables", "-t", "filter", "-N", filterChain); err != nil {
		return fmt.Errorf("create filter chain: %w", err)
	}
	if err := ipt("iptables", "-t", "mangle", "-N", mangleChain); err != nil {
		removeMekoIptables(id)
		return fmt.Errorf("create mangle chain: %w", err)
	}
	if err := ipt("iptables", "-t", "filter", "-I", "INPUT", "1", "-j", filterChain); err != nil {
		removeMekoIptables(id)
		return err
	}
	if err := ipt("iptables", "-t", "mangle", "-I", "PREROUTING", "1", "-j", mangleChain); err != nil {
		removeMekoIptables(id)
		return err
	}
	portStr := strconv.Itoa(port)
	if cfg.IOSBypass {
		if err := ipt("iptables", "-t", "mangle", "-A", mangleChain, "-p", "tcp", "--dport", portStr, "--syn", "-m", "u32", "--u32", mekoIOSU32, "-j", "MARK", "--set-mark", "0x400"); err != nil {
			removeMekoIptables(id)
			return fmt.Errorf("install iOS classifier: %w", err)
		}
		if err := ipt("iptables", "-t", "filter", "-A", filterChain, "-p", "tcp", "--dport", portStr, "--syn", "-m", "mark", "--mark", "0x400", "-j", "ACCEPT"); err != nil {
			removeMekoIptables(id)
			return err
		}
	}
	hashName := fmt.Sprintf("xui_mt_%d", id)
	if err := ipt("iptables", "-t", "filter", "-A", filterChain, "-p", "tcp", "--dport", portStr, "--syn", "-m", "hashlimit", "--hashlimit-mode", "srcip", "--hashlimit-name", hashName, "--hashlimit-upto", fmt.Sprintf("%d/minute", rate), "--hashlimit-burst", strconv.Itoa(burst), "--hashlimit-htable-expire", "60000", "-j", "ACCEPT"); err != nil {
		removeMekoIptables(id)
		return fmt.Errorf("install SYN rate rule: %w", err)
	}
	if err := ipt("iptables", "-t", "filter", "-A", filterChain, "-p", "tcp", "--dport", portStr, "--syn", "-j", "REJECT", "--reject-with", "tcp-reset"); err != nil {
		removeMekoIptables(id)
		return err
	}

	// Mirror the rate/reject behavior for IPv6 when ip6tables is available.
	if _, err := exec.LookPath("ip6tables"); err == nil {
		chain6 := mekoFilter6Chain(id)
		_ = ipt("ip6tables", "-t", "filter", "-N", chain6)
		_ = ipt("ip6tables", "-t", "filter", "-I", "INPUT", "1", "-j", chain6)
		_ = ipt("ip6tables", "-t", "filter", "-A", chain6, "-p", "tcp", "--dport", portStr, "--syn", "-m", "hashlimit", "--hashlimit-mode", "srcip", "--hashlimit-name", hashName+"6", "--hashlimit-upto", fmt.Sprintf("%d/minute", rate), "--hashlimit-burst", strconv.Itoa(burst), "--hashlimit-htable-expire", "60000", "-j", "ACCEPT")
		_ = ipt("ip6tables", "-t", "filter", "-A", chain6, "-p", "tcp", "--dport", portStr, "--syn", "-j", "REJECT", "--reject-with", "tcp-reset")
	}
	return nil
}

func removeMekoIptables(id int) {
	filterChain, mangleChain := mekoFilterChain(id), mekoMangleChain(id)
	if _, err := exec.LookPath("iptables"); err == nil {
		for commandExists("iptables", "-t", "filter", "-C", "INPUT", "-j", filterChain) {
			_ = ipt("iptables", "-t", "filter", "-D", "INPUT", "-j", filterChain)
		}
		for commandExists("iptables", "-t", "mangle", "-C", "PREROUTING", "-j", mangleChain) {
			_ = ipt("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", mangleChain)
		}
		_ = ipt("iptables", "-t", "filter", "-F", filterChain)
		_ = ipt("iptables", "-t", "filter", "-X", filterChain)
		_ = ipt("iptables", "-t", "mangle", "-F", mangleChain)
		_ = ipt("iptables", "-t", "mangle", "-X", mangleChain)
	}
	if _, err := exec.LookPath("ip6tables"); err == nil {
		chain6 := mekoFilter6Chain(id)
		for commandExists("ip6tables", "-t", "filter", "-C", "INPUT", "-j", chain6) {
			_ = ipt("ip6tables", "-t", "filter", "-D", "INPUT", "-j", chain6)
		}
		_ = ipt("ip6tables", "-t", "filter", "-F", chain6)
		_ = ipt("ip6tables", "-t", "filter", "-X", chain6)
	}
}

func ipt(binary string, args ...string) error {
	cmd := exec.Command(binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func commandExists(binary string, args ...string) bool {
	return exec.Command(binary, args...).Run() == nil
}
