package integration

import (
	"fmt"
	"os"
	"strings"

	"blockchain.hanz.dev/manager/types"
)

type ctfPlatform string

const (
	platformCTFd ctfPlatform = "ctfd"
	platformRCTF ctfPlatform = "rctf"
)

type ctfConfig struct {
	platform ctfPlatform
	baseURL  string
}

var ctfCfg, ctfCfgErr = loadCTFConfig()

// loadCTFConfig resolves the platform integration from the environment once at
// startup. CTF_BASE is the canonical base URL; CTFD_BASE and RCTF_BASE remain
// supported as platform-specific fallbacks for existing deployments.
func loadCTFConfig() (ctfConfig, error) {
	platform := ctfPlatform(strings.ToLower(strings.TrimSpace(os.Getenv("CTF_PLATFORM"))))
	if platform == "" {
		platform = platformCTFd
	}

	var baseURL string
	switch platform {
	case platformCTFd:
		baseURL = firstNonEmpty(os.Getenv("CTF_BASE"), os.Getenv("CTFD_BASE"))
	case platformRCTF:
		baseURL = firstNonEmpty(os.Getenv("CTF_BASE"), os.Getenv("RCTF_BASE"))
	default:
		return ctfConfig{}, fmt.Errorf("unsupported CTF_PLATFORM %q (want %q or %q)", platform, platformCTFd, platformRCTF)
	}

	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return ctfConfig{}, fmt.Errorf("missing CTF_BASE for CTF_PLATFORM %q", platform)
	}

	return ctfConfig{platform: platform, baseURL: strings.TrimSuffix(baseURL, "/")}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// ValidateConfig reports whether the configured CTF platform integration is
// usable. It lets main fail fast instead of surfacing errors per request.
func ValidateConfig() error {
	return ctfCfgErr
}

// GetMe resolves the authenticated player for an access token using the
// configured CTF platform.
func GetMe(accessToken string) types.CTFUser {
	if ctfCfgErr != nil {
		return types.CTFUser{Name: "invalid"}
	}

	switch ctfCfg.platform {
	case platformRCTF:
		return rctfGetMe(ctfCfg.baseURL, accessToken)
	default:
		return ctfdGetMe(ctfCfg.baseURL, accessToken)
	}
}
